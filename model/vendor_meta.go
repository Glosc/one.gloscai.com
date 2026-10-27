package model

import (
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

var autoAddVendorsMu sync.Mutex

// Vendor 用于存储供应商信息，供模型引用
// Name 唯一，用于在模型中关联
// Alias 供应商别名，用于名称匹配（例如上游同步时的名称归一化）
// Icon 采用 @lobehub/icons 的图标名，前端可直接渲染
// Status 预留字段，1 表示启用
// 本表同样遵循 3NF 设计范式

type Vendor struct {
	ModelCount  int64          `json:"model_count" gorm:"-"`
	Version     string         `json:"version,omitempty" gorm:"-"`
	Id          int            `json:"id"`
	Name        string         `json:"name" gorm:"size:128;not null;uniqueIndex:uk_vendor_name_delete_at,priority:1"`
	Alias       string         `json:"alias,omitempty" gorm:"type:varchar(128);index"`
	Description string         `json:"description,omitempty" gorm:"type:text"`
	Icon        string         `json:"icon,omitempty" gorm:"type:varchar(128)"`
	Status      int            `json:"status" gorm:"default:1"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_vendor_name_delete_at,priority:2"`
}

type VendorAutoAddCatalogItem struct {
	Name      string   `json:"name"`
	Alias     string   `json:"alias,omitempty"`
	Icon      string   `json:"icon"`
	MatchKeys []string `json:"match_keys"`
}

type AutoAddVendorsResult struct {
	ScannedModelCount         int64    `json:"scanned_model_count"`
	ScannedNamespaceCount     int      `json:"scanned_namespace_count"`
	CreatedCount              int      `json:"created_count"`
	CreatedNamespaceCount     int      `json:"created_namespace_count"`
	ExistingCount             int      `json:"existing_count"`
	UnmatchedCount            int      `json:"unmatched_count"`
	AmbiguousCount            int      `json:"ambiguous_count"`
	DeletedConflictCount      int      `json:"deleted_conflict_count"`
	CreatedVendors            []Vendor `json:"created_vendors"`
	CreatedNamespaces         []string `json:"created_namespaces"`
	ExistingNamespaces        []string `json:"existing_namespaces"`
	UnmatchedNamespaces       []string `json:"unmatched_namespaces"`
	AmbiguousNamespaces       []string `json:"ambiguous_namespaces"`
	DeletedConflictNamespaces []string `json:"deleted_conflict_namespaces"`
}

// Insert 创建新的供应商记录
func (v *Vendor) Insert() error {
	v.Id = 0
	err := metadataTransaction(func(tx *gorm.DB) error {
		if err := validateVendorMetadata(tx, v); err != nil {
			return err
		}
		now := common.GetTimestamp()
		v.CreatedTime, v.UpdatedTime, v.Status = now, now, 1
		return tx.Create(v).Error
	})
	if err == nil {
		v.Version = VendorRecordVersion(v)
		RefreshPricing()
	}
	return err
}

// IsVendorNameDuplicated 检查供应商名称是否与其他名称或别名冲突（排除自身 ID）
func IsVendorNameDuplicated(id int, name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Vendor{}).
		Where("(name = ? OR alias = ?) AND id <> ?", name, name, id).
		Count(&cnt).Error
	return cnt > 0, err
}

// IsVendorAliasDuplicated 检查供应商别名是否与其他名称或别名冲突（排除自身 ID）
// 别名用于名称匹配，重复会导致匹配歧义，因此需要唯一。
func IsVendorAliasDuplicated(id int, alias string) (bool, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Vendor{}).
		Where("(alias = ? OR name = ?) AND id <> ?", alias, alias, id).
		Count(&cnt).Error
	return cnt > 0, err
}

// GetVendorByNameOrAlias 根据名称或别名查找供应商，优先匹配名称。
// 用于上游同步等场景下的名称归一化。
func GetVendorByNameOrAlias(name string) (*Vendor, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var v Vendor
	if err := DB.Where("name = ?", name).First(&v).Error; err == nil {
		return &v, nil
	}
	if err := DB.Where("alias = ?", name).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// IsVendorInUse reports whether active model metadata still references a vendor.
func IsVendorInUse(id int) (bool, error) {
	var count int64
	err := DB.Model(&Model{}).Where("vendor_id = ?", id).Count(&count).Error
	return count > 0, err
}

// Update 更新供应商记录
func (v *Vendor) Update() error {
	err := metadataTransaction(func(tx *gorm.DB) error {
		var saved Vendor
		if err := tx.First(&saved, v.Id).Error; err != nil {
			return err
		}
		if v.Version != "" && v.Version != VendorRecordVersion(&saved) {
			return ErrVendorConflict
		}
		if err := validateVendorMetadata(tx, v); err != nil {
			return err
		}
		v.CreatedTime, v.UpdatedTime = saved.CreatedTime, common.GetTimestamp()
		return tx.Model(&Vendor{}).Where("id = ?", v.Id).Updates(map[string]any{"name": v.Name, "alias": v.Alias, "description": v.Description, "icon": v.Icon, "status": v.Status, "updated_time": v.UpdatedTime}).Error
	})
	if err == nil {
		v.Version = VendorRecordVersion(v)
		RefreshPricing()
	}
	return err
}

// Delete rejects referenced vendors rather than leaving orphaned model records.
func (v *Vendor) Delete() error { return DeleteVendors([]int{v.Id}) }

// GetVendorByID 根据 ID 获取供应商
func GetVendorByID(id int) (*Vendor, error) {
	var v Vendor
	err := DB.First(&v, id).Error
	if err != nil {
		return nil, err
	}
	if err := DB.Model(&Model{}).Where("vendor_id = ?", id).Count(&v.ModelCount).Error; err != nil {
		return nil, err
	}
	v.Version = VendorRecordVersion(&v)
	return &v, nil
}

type VendorListFilter struct {
	Keyword string
	Status  *int
	HasIcon *bool
}

// ListVendors 按关键字、状态和图标配置筛选供应商。
func ListVendors(filter VendorListFilter, offset int, limit int, association ...string) ([]*Vendor, int64, error) {
	db := DB.Model(&Vendor{})
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("name LIKE ? OR alias LIKE ? OR description LIKE ?", like, like, like)
	}
	if filter.Status != nil {
		db = db.Where("status = ?", *filter.Status)
	}
	if filter.HasIcon != nil {
		if *filter.HasIcon {
			db = db.Where("icon IS NOT NULL AND TRIM(icon) <> ''")
		} else {
			db = db.Where("icon IS NULL OR TRIM(icon) = ''")
		}
	}
	if len(association) > 0 {
		references := DB.Model(&Model{}).Select("1").Where("models.vendor_id = vendors.id")
		switch association[0] {
		case "linked":
			db = db.Where("EXISTS (?)", references)
		case "unlinked":
			db = db.Where("NOT EXISTS (?)", references)
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var vendors []*Vendor
	if err := db.Offset(offset).Limit(limit).Order("id DESC").Find(&vendors).Error; err != nil {
		return nil, 0, err
	}
	counts, err := GetVendorModelCounts()
	if err != nil {
		return nil, 0, err
	}
	for _, vendor := range vendors {
		vendor.ModelCount = counts[int64(vendor.Id)]
		vendor.Version = VendorRecordVersion(vendor)
	}
	return vendors, total, nil
}

func BatchUpdateVendorStatus(ids []int, status int) (int64, error) {
	result := DB.Model(&Vendor{}).
		Where("id IN ? AND status <> ?", ids, status).
		Updates(map[string]interface{}{
			"status":       status,
			"updated_time": common.GetTimestamp(),
		})
	return result.RowsAffected, result.Error
}

// BatchDeleteVendors atomically deletes vendors only when none are referenced
// by model metadata. The returned names identify the vendors blocking deletion.
func BatchDeleteVendors(ids []int) (int64, []string, error) {
	var deletedCount int64
	var blockedNames []string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var vendors []Vendor
		if err := tx.Where("id IN ?", ids).Find(&vendors).Error; err != nil {
			return err
		}
		if len(vendors) != len(ids) {
			return gorm.ErrRecordNotFound
		}

		var referencedVendorIDs []int
		if err := tx.Model(&Model{}).
			Where("vendor_id IN ?", ids).
			Distinct("vendor_id").
			Pluck("vendor_id", &referencedVendorIDs).Error; err != nil {
			return err
		}
		if len(referencedVendorIDs) > 0 {
			referenced := make(map[int]struct{}, len(referencedVendorIDs))
			for _, vendorID := range referencedVendorIDs {
				referenced[vendorID] = struct{}{}
			}
			for _, vendor := range vendors {
				if _, exists := referenced[vendor.Id]; exists {
					blockedNames = append(blockedNames, vendor.Name)
				}
			}
			sort.Strings(blockedNames)
			return nil
		}

		result := tx.Where("id IN ?", ids).Delete(&Vendor{})
		deletedCount = result.RowsAffected
		return result.Error
	})
	return deletedCount, blockedNames, err
}

// RemoveUnusedVendors soft-deletes every vendor that is not referenced by
// active model metadata.
func RemoveUnusedVendors() (int64, error) {
	var deletedCount int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var referencedVendorIDs []int
		if err := tx.Model(&Model{}).
			Where("vendor_id > ?", 0).
			Distinct("vendor_id").
			Pluck("vendor_id", &referencedVendorIDs).Error; err != nil {
			return err
		}

		unusedVendors := tx.Model(&Vendor{})
		if len(referencedVendorIDs) > 0 {
			unusedVendors = unusedVendors.Where("id NOT IN ?", referencedVendorIDs)
		}
		var unusedVendorIDs []int
		if err := unusedVendors.Pluck("id", &unusedVendorIDs).Error; err != nil {
			return err
		}
		if len(unusedVendorIDs) == 0 {
			return nil
		}

		result := tx.Where("id IN ?", unusedVendorIDs).Delete(&Vendor{})
		deletedCount = result.RowsAffected
		return result.Error
	})
	return deletedCount, err
}

func buildVendorCatalogMatchIndex(catalog []VendorAutoAddCatalogItem) map[string]int {
	matchIndex := make(map[string]int)
	for catalogIndex, candidate := range catalog {
		for _, matchKey := range candidate.MatchKeys {
			normalizedKey := normalizeVendorMatchKey(matchKey)
			if normalizedKey == "" {
				continue
			}
			existingIndex, exists := matchIndex[normalizedKey]
			if !exists || existingIndex == catalogIndex {
				matchIndex[normalizedKey] = catalogIndex
				continue
			}
			if existingIndex != ambiguousVendorMatch {
				existingCandidate := catalog[existingIndex]
				existingIdentity := normalizeVendorMatchKey(existingCandidate.Name) + "\x00" + normalizeVendorMatchKey(existingCandidate.Alias)
				candidateIdentity := normalizeVendorMatchKey(candidate.Name) + "\x00" + normalizeVendorMatchKey(candidate.Alias)
				if existingIdentity == candidateIdentity {
					continue
				}
			}
			matchIndex[normalizedKey] = ambiguousVendorMatch
		}
	}
	return matchIndex
}

func matchVendorCatalogNamespace(namespace string, matchIndex map[string]int) (int, bool, bool) {
	if catalogIndex, exists := matchIndex[namespace]; exists {
		return catalogIndex, true, catalogIndex == ambiguousVendorMatch
	}

	canonicalKey, exists := vendorNamespaceAliases[namespace]
	if !exists {
		return 0, false, false
	}
	catalogIndex, exists := matchIndex[canonicalKey]
	if !exists {
		return 0, false, false
	}
	return catalogIndex, true, catalogIndex == ambiguousVendorMatch
}

func matchingCatalogCandidateVendorIDs(candidate VendorAutoAddCatalogItem, vendors []Vendor) []int {
	candidateKeys := make(map[string]struct{}, 2)
	for _, value := range []string{candidate.Name, candidate.Alias} {
		key := normalizeVendorMatchKey(value)
		if key != "" {
			candidateKeys[key] = struct{}{}
		}
	}

	matchedIDs := make([]int, 0, 1)
	for _, vendor := range vendors {
		matched := false
		for _, value := range []string{vendor.Name, vendor.Alias} {
			if _, exists := candidateKeys[normalizeVendorMatchKey(value)]; exists {
				matched = true
				break
			}
		}
		if matched {
			matchedIDs = append(matchedIDs, vendor.Id)
		}
	}
	return matchedIDs
}

// AutoAddVendorsFromCatalog creates catalog vendors referenced by current model
// namespaces. It never changes existing vendors or model metadata.
func AutoAddVendorsFromCatalog(catalog []VendorAutoAddCatalogItem) (AutoAddVendorsResult, error) {
	autoAddVendorsMu.Lock()
	defer autoAddVendorsMu.Unlock()

	result := AutoAddVendorsResult{
		CreatedVendors:            make([]Vendor, 0),
		CreatedNamespaces:         make([]string, 0),
		ExistingNamespaces:        make([]string, 0),
		UnmatchedNamespaces:       make([]string, 0),
		AmbiguousNamespaces:       make([]string, 0),
		DeletedConflictNamespaces: make([]string, 0),
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var modelNames []string
		if err := tx.Model(&Model{}).Order("id ASC").Pluck("model_name", &modelNames).Error; err != nil {
			return err
		}
		result.ScannedModelCount = int64(len(modelNames))

		namespaceSet := make(map[string]struct{})
		for _, modelName := range modelNames {
			prefix, _, found := strings.Cut(strings.TrimSpace(modelName), "/")
			if !found {
				continue
			}
			namespace := normalizeVendorMatchKey(prefix)
			if namespace != "" {
				namespaceSet[namespace] = struct{}{}
			}
		}
		namespaces := make([]string, 0, len(namespaceSet))
		for namespace := range namespaceSet {
			namespaces = append(namespaces, namespace)
		}
		sort.Strings(namespaces)
		result.ScannedNamespaceCount = len(namespaces)

		var activeVendors []Vendor
		if err := tx.Order("id ASC").Find(&activeVendors).Error; err != nil {
			return err
		}
		nameIndex, aliasIndex := buildVendorMatchIndexes(activeVendors)
		catalogMatchIndex := buildVendorCatalogMatchIndex(catalog)

		candidateNamespaces := make(map[int][]string)
		for _, namespace := range namespaces {
			vendorID, ambiguous := matchVendorNamespace(namespace, nameIndex, aliasIndex)
			if ambiguous {
				result.AmbiguousNamespaces = append(result.AmbiguousNamespaces, namespace)
				continue
			}
			if vendorID != 0 {
				result.ExistingNamespaces = append(result.ExistingNamespaces, namespace)
				continue
			}

			catalogIndex, matched, ambiguous := matchVendorCatalogNamespace(namespace, catalogMatchIndex)
			if ambiguous {
				result.AmbiguousNamespaces = append(result.AmbiguousNamespaces, namespace)
				continue
			}
			if !matched {
				result.UnmatchedNamespaces = append(result.UnmatchedNamespaces, namespace)
				continue
			}
			candidateNamespaces[catalogIndex] = append(candidateNamespaces[catalogIndex], namespace)
		}

		var deletedVendors []Vendor
		if err := tx.Unscoped().Where("deleted_at IS NOT NULL").Order("id ASC").Find(&deletedVendors).Error; err != nil {
			return err
		}
		deletedIdentityKeys := make(map[string]struct{}, len(deletedVendors)*2)
		for _, vendor := range deletedVendors {
			for _, value := range []string{vendor.Name, vendor.Alias} {
				key := normalizeVendorMatchKey(value)
				if key != "" {
					deletedIdentityKeys[key] = struct{}{}
				}
			}
		}

		candidateIndexes := make([]int, 0, len(candidateNamespaces))
		for catalogIndex := range candidateNamespaces {
			candidateIndexes = append(candidateIndexes, catalogIndex)
		}
		sort.Slice(candidateIndexes, func(i int, j int) bool {
			left := catalog[candidateIndexes[i]]
			right := catalog[candidateIndexes[j]]
			leftKey := normalizeVendorMatchKey(left.Name) + "\x00" + normalizeVendorMatchKey(left.Alias) + "\x00" + normalizeVendorMatchKey(left.Icon)
			rightKey := normalizeVendorMatchKey(right.Name) + "\x00" + normalizeVendorMatchKey(right.Alias) + "\x00" + normalizeVendorMatchKey(right.Icon)
			if leftKey == rightKey {
				return candidateIndexes[i] < candidateIndexes[j]
			}
			return leftKey < rightKey
		})

		createdVendorIDs := make(map[int]struct{})
		for _, catalogIndex := range candidateIndexes {
			candidate := catalog[catalogIndex]
			matchedVendorIDs := matchingCatalogCandidateVendorIDs(candidate, activeVendors)
			if len(matchedVendorIDs) > 1 {
				result.AmbiguousNamespaces = append(result.AmbiguousNamespaces, candidateNamespaces[catalogIndex]...)
				continue
			}
			if len(matchedVendorIDs) == 1 {
				if _, created := createdVendorIDs[matchedVendorIDs[0]]; created {
					result.CreatedNamespaces = append(result.CreatedNamespaces, candidateNamespaces[catalogIndex]...)
				} else {
					result.ExistingNamespaces = append(result.ExistingNamespaces, candidateNamespaces[catalogIndex]...)
				}
				continue
			}

			deletedConflict := false
			for _, value := range []string{candidate.Name, candidate.Alias} {
				if _, exists := deletedIdentityKeys[normalizeVendorMatchKey(value)]; exists {
					deletedConflict = true
					break
				}
			}
			if deletedConflict {
				result.DeletedConflictNamespaces = append(result.DeletedConflictNamespaces, candidateNamespaces[catalogIndex]...)
				continue
			}

			now := common.GetTimestamp()
			vendor := Vendor{
				Name:        strings.TrimSpace(candidate.Name),
				Alias:       strings.TrimSpace(candidate.Alias),
				Icon:        strings.TrimSpace(candidate.Icon),
				Status:      1,
				CreatedTime: now,
				UpdatedTime: now,
			}
			if err := tx.Create(&vendor).Error; err != nil {
				return err
			}
			activeVendors = append(activeVendors, vendor)
			createdVendorIDs[vendor.Id] = struct{}{}
			result.CreatedVendors = append(result.CreatedVendors, vendor)
			result.CreatedNamespaces = append(result.CreatedNamespaces, candidateNamespaces[catalogIndex]...)
		}

		sort.Slice(result.CreatedVendors, func(i int, j int) bool {
			leftKey := normalizeVendorMatchKey(result.CreatedVendors[i].Name)
			rightKey := normalizeVendorMatchKey(result.CreatedVendors[j].Name)
			if leftKey == rightKey {
				return result.CreatedVendors[i].Id < result.CreatedVendors[j].Id
			}
			return leftKey < rightKey
		})
		sort.Strings(result.CreatedNamespaces)
		sort.Strings(result.ExistingNamespaces)
		sort.Strings(result.UnmatchedNamespaces)
		sort.Strings(result.AmbiguousNamespaces)
		sort.Strings(result.DeletedConflictNamespaces)
		result.CreatedCount = len(result.CreatedVendors)
		result.CreatedNamespaceCount = len(result.CreatedNamespaces)
		result.ExistingCount = len(result.ExistingNamespaces)
		result.UnmatchedCount = len(result.UnmatchedNamespaces)
		result.AmbiguousCount = len(result.AmbiguousNamespaces)
		result.DeletedConflictCount = len(result.DeletedConflictNamespaces)
		return nil
	})
	return result, err
}

func GetAllVendors(offset, limit int) ([]*Vendor, error) {
	items, _, err := ListVendors(VendorListFilter{}, offset, limit)
	return items, err
}

func SearchVendors(keyword string, offset, limit int, association ...string) ([]*Vendor, int64, error) {
	return ListVendors(VendorListFilter{Keyword: keyword}, offset, limit, association...)
}
