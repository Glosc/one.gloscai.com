package model

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	NameRuleExact = iota
	NameRulePrefix
	NameRuleContains
	NameRuleSuffix
)

// Billing types for a model. Default is per-call (the configured fixed price is
// charged once per request). Per-second multiplies the fixed price by the
// requested duration in seconds (used for video generation models).
const (
	BillingTypePerCall   = iota // 0: charge the fixed price once per call
	BillingTypePerSecond        // 1: charge fixed price × duration(seconds)
)

type ModelSquareState string

const (
	ModelSquareVisible     ModelSquareState = "visible"
	ModelSquareUnavailable ModelSquareState = "unavailable"
	ModelSquareHidden      ModelSquareState = "hidden"
	ModelSquarePartial     ModelSquareState = "partial"
)

type BoundChannel struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type Model struct {
	Categories         string         `json:"categories,omitempty" gorm:"type:text"`
	BillingType        int            `json:"billing_type" gorm:"default:0"`
	Id                 int            `json:"id"`
	ModelName          string         `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_name_delete_at,priority:1"`
	Description        string         `json:"description,omitempty" gorm:"type:text"`
	Icon               string         `json:"icon,omitempty" gorm:"type:varchar(128)"`
	Tags               string         `json:"tags,omitempty" gorm:"type:varchar(255)"`
	VendorID           int            `json:"vendor_id,omitempty" gorm:"index"`
	Endpoints          string         `json:"endpoints,omitempty" gorm:"type:text"`
	SupportedEndpoints []string       `json:"supported_endpoints,omitempty" gorm:"-"`
	Status             int            `json:"status" gorm:"default:1"`
	SyncOfficial       int            `json:"sync_official" gorm:"default:1"`
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime        int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_model_name_delete_at,priority:2"`

	BoundChannels []BoundChannel `json:"bound_channels,omitempty" gorm:"-"`
	EnableGroups  []string       `json:"enable_groups,omitempty" gorm:"-"`
	QuotaTypes    []int          `json:"quota_types,omitempty" gorm:"-"`
	HasPrice      bool           `json:"has_price" gorm:"-"`
	NameRule      int            `json:"name_rule" gorm:"default:0"`

	MatchedModels []string `json:"matched_models,omitempty" gorm:"-"`
	MatchedCount  int      `json:"matched_count,omitempty" gorm:"-"`

	HasMetadata            bool             `json:"has_metadata" gorm:"-"`
	ConfiguredChannelCount int              `json:"configured_channel_count" gorm:"-"`
	SquareState            ModelSquareState `json:"square_state" gorm:"-"`
}

// MatchesName applies a metadata rule to a concrete channel model name.
func (mi *Model) MatchesName(name string) bool {
	switch mi.NameRule {
	case NameRulePrefix:
		return strings.HasPrefix(name, mi.ModelName)
	case NameRuleSuffix:
		return strings.HasSuffix(name, mi.ModelName)
	case NameRuleContains:
		return strings.Contains(name, mi.ModelName)
	default:
		return name == mi.ModelName
	}
}

// resolveModelMetadata preserves catalog precedence: exact, prefix, suffix,
// then contains. The first matching record within a rule type wins.
func resolveModelMetadata(records []Model, names []string) map[string]*Model {
	resolved := make(map[string]*Model)
	for i := range records {
		if records[i].NameRule == NameRuleExact {
			resolved[records[i].ModelName] = &records[i]
		}
	}
	for _, rule := range []int{NameRulePrefix, NameRuleSuffix, NameRuleContains} {
		for i := range records {
			metadata := &records[i]
			if metadata.NameRule != rule {
				continue
			}
			for _, name := range names {
				if _, exists := resolved[name]; !exists && metadata.MatchesName(name) {
					resolved[name] = metadata
				}
			}
		}
	}
	return resolved
}

// FillModelSquareStates applies the same metadata policy as the public catalog
// to live routes, then aggregates concrete models for metadata rule rows.
func FillModelSquareStates(rows []*Model, configured map[string][]int, connections []ModelConnection) error {
	var metadata []Model
	if err := DB.Find(&metadata).Error; err != nil {
		return err
	}
	nameSet := make(map[string]struct{}, len(configured))
	for name := range configured {
		nameSet[name] = struct{}{}
	}
	for _, row := range rows {
		if row != nil && row.NameRule == NameRuleExact {
			nameSet[row.ModelName] = struct{}{}
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	resolved := resolveModelMetadata(metadata, names)
	available := make(map[string]bool)
	for _, connection := range connections {
		available[connection.Model] = true
	}
	states := make(map[string]ModelSquareState, len(names))
	for _, name := range names {
		state := ModelSquareUnavailable
		if policy := resolved[name]; policy != nil && policy.Status != 1 {
			state = ModelSquareHidden
		} else if available[name] {
			state = ModelSquareVisible
		}
		states[name] = state
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.NameRule == NameRuleExact {
			row.SquareState = states[row.ModelName]
			continue
		}
		total, visible, hidden := 0, 0, 0
		for name := range configured {
			if !row.MatchesName(name) {
				continue
			}
			total++
			switch states[name] {
			case ModelSquareVisible:
				visible++
			case ModelSquareHidden:
				hidden++
			}
		}
		row.SquareState = ModelSquareUnavailable
		switch {
		case total == 0:
			if row.Status != 1 {
				row.SquareState = ModelSquareHidden
			}
		case hidden == total:
			row.SquareState = ModelSquareHidden
		case visible == total:
			row.SquareState = ModelSquareVisible
		case visible > 0:
			row.SquareState = ModelSquarePartial
		}
	}
	return nil
}

// GetConfiguredModelChannels includes disabled channels and reads no credentials.
func GetConfiguredModelChannels() (map[string][]int, error) {
	var channels []Channel
	if err := DB.Select("id", "models").Find(&channels).Error; err != nil {
		return nil, err
	}
	configured := make(map[string][]int)
	for _, channel := range channels {
		for _, name := range normalizeLookupValues(channel.GetModels()) {
			configured[name] = append(configured[name], channel.Id)
		}
	}
	return configured, nil
}

// SearchModelsWithChannels augments metadata with concrete configured names.
// Synthetic rows never persist and never affect the public pricing catalog.
func SearchModelsWithChannels(keyword, vendor, status, syncOfficial string, offset, limit int) ([]*Model, int64, error) {
	records, _, err := SearchModels(keyword, vendor, status, syncOfficial, 0, -1)
	if err != nil {
		return nil, 0, err
	}
	_, filterStatus := parseModelStatusFilter(status)
	_, filterSync := parseModelSyncFilter(syncOfficial)
	if !filterStatus && !filterSync && (vendor == "" || vendor == "0") {
		configured, err := GetConfiguredModelChannels()
		if err != nil {
			return nil, 0, err
		}
		var exactNames []string
		if err := DB.Model(&Model{}).Where("name_rule = ?", NameRuleExact).Pluck("model_name", &exactNames).Error; err != nil {
			return nil, 0, err
		}
		for _, name := range exactNames {
			delete(configured, name)
		}
		names := make([]string, 0, len(configured))
		for name := range configured {
			if keyword == "" || strings.Contains(strings.ToLower(name), strings.ToLower(keyword)) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			records = append(records, &Model{ModelName: name, NameRule: NameRuleExact})
		}
	}
	total := len(records)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*Model{}, int64(total), nil
	}
	end := total
	if limit >= 0 && limit < total-offset {
		end = offset + limit
	}
	return records[offset:end], int64(total), nil
}

type ModelsMetaFilter struct {
	Keyword      string
	Vendor       string
	Tag          string
	Category     string
	Status       *int
	SyncOfficial *int
	HasPrice     *bool
	SortBy       string
	SortOrder    string
}

var modelsMetaAllowedSorts = map[string]string{
	"id":            "models.id",
	"model_name":    "models.model_name",
	"created_time":  "models.created_time",
	"updated_time":  "models.updated_time",
	"status":        "models.status",
	"vendor_id":     "models.vendor_id",
	"sync_official": "models.sync_official",
}

func modelsMetaSortClause(sortBy string, sortOrder string) string {
	return safeSortClause(sortBy, modelsMetaAllowedSorts, "models.id", sortOrder)
}

func (mi *Model) Insert() error {
	return metadataTransaction(func(tx *gorm.DB) error {
		if err := validateModelVendor(tx, mi.VendorID); err != nil {
			return err
		}
		now := common.GetTimestamp()
		mi.CreatedTime, mi.UpdatedTime = now, now
		status, syncOfficial := mi.Status, mi.SyncOfficial
		if err := tx.Create(mi).Error; err != nil {
			return err
		}
		mi.Status, mi.SyncOfficial = status, syncOfficial
		return tx.Model(&Model{}).Where("id = ?", mi.Id).Updates(map[string]any{"status": status, "sync_official": syncOfficial}).Error
	})
}

func IsModelNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Model{}).Where("model_name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func (mi *Model) Update() error {
	return metadataTransaction(func(tx *gorm.DB) error {
		if err := validateModelVendor(tx, mi.VendorID); err != nil {
			return err
		}
		mi.UpdatedTime = common.GetTimestamp()
		return tx.Model(&Model{}).Where("id = ?", mi.Id).
			Select("model_name", "description", "icon", "tags", "categories", "vendor_id", "endpoints", "status", "sync_official", "billing_type", "name_rule", "updated_time").Updates(mi).Error
	})
}

func BatchUpdateModelVendor(ids []int, vendorID int, icon *string) (int64, error) {
	updates := map[string]interface{}{
		"vendor_id":    vendorID,
		"updated_time": common.GetTimestamp(),
	}
	if icon != nil {
		updates["icon"] = *icon
	}
	result := DB.Model(&Model{}).Where("id IN ?", ids).Updates(updates)
	return result.RowsAffected, result.Error
}

type AutoMatchModelVendorsResult struct {
	UpdatedCount   int64 `json:"updated_count"`
	UnmatchedCount int64 `json:"unmatched_count"`
	AmbiguousCount int64 `json:"ambiguous_count"`
	SkippedCount   int64 `json:"skipped_count"`
}

const ambiguousVendorMatch = -1

var vendorNamespaceAliases = map[string]string{
	"moonshotai": "moonshot",
}

func normalizeVendorMatchKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func buildVendorMatchIndexes(vendors []Vendor) (map[string]int, map[string]int) {
	nameIndex := make(map[string]int, len(vendors))
	aliasIndex := make(map[string]int, len(vendors))
	for _, vendor := range vendors {
		nameKey := normalizeVendorMatchKey(vendor.Name)
		if nameKey != "" {
			if existingID, exists := nameIndex[nameKey]; exists && existingID != vendor.Id {
				nameIndex[nameKey] = ambiguousVendorMatch
			} else {
				nameIndex[nameKey] = vendor.Id
			}
		}

		aliasKey := normalizeVendorMatchKey(vendor.Alias)
		if aliasKey == "" {
			continue
		}
		if existingID, exists := aliasIndex[aliasKey]; exists && existingID != vendor.Id {
			aliasIndex[aliasKey] = ambiguousVendorMatch
		} else {
			aliasIndex[aliasKey] = vendor.Id
		}
	}
	return nameIndex, aliasIndex
}

func matchModelVendor(modelName string, nameIndex map[string]int, aliasIndex map[string]int) (int, bool) {
	trimmedName := strings.TrimSpace(modelName)
	if trimmedName == "" {
		return 0, false
	}

	prefix, _, found := strings.Cut(trimmedName, "/")
	if !found {
		return 0, false
	}
	namespace := normalizeVendorMatchKey(prefix)
	if namespace == "" {
		return 0, false
	}
	return matchVendorNamespace(namespace, nameIndex, aliasIndex)
}

func matchVendorNamespace(namespace string, nameIndex map[string]int, aliasIndex map[string]int) (int, bool) {
	if vendorID, exists := nameIndex[namespace]; exists {
		return vendorID, vendorID == ambiguousVendorMatch
	}
	if vendorID, exists := aliasIndex[namespace]; exists {
		return vendorID, vendorID == ambiguousVendorMatch
	}

	canonicalKey, exists := vendorNamespaceAliases[namespace]
	if !exists {
		return 0, false
	}
	if vendorID, exists := nameIndex[canonicalKey]; exists {
		return vendorID, vendorID == ambiguousVendorMatch
	}
	if vendorID, exists := aliasIndex[canonicalKey]; exists {
		return vendorID, vendorID == ambiguousVendorMatch
	}
	return 0, false
}

func updateAutoMatchedModelVendors(tx *gorm.DB, ids []int, vendorID int, vendorIcon string, updatedTime int64, overwriteExisting bool) (int64, error) {
	const batchSize = 500
	var updatedCount int64
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		models := tx.Model(&Model{}).Where("id IN ?", ids[start:end])
		if overwriteExisting {
			models = models.Where("(vendor_id <> ? OR vendor_id IS NULL OR icon <> ? OR icon IS NULL)", vendorID, vendorIcon)
		} else {
			models = models.Where("(vendor_id = ? OR vendor_id IS NULL)", 0)
		}
		updateResult := models.
			Updates(map[string]interface{}{
				"vendor_id":    vendorID,
				"icon":         vendorIcon,
				"updated_time": updatedTime,
			})
		if updateResult.Error != nil {
			return updatedCount, updateResult.Error
		}
		updatedCount += updateResult.RowsAffected
	}
	return updatedCount, nil
}

// AutoMatchModelVendors assigns vendors and their icons from model namespaces.
// Existing assignments are only rematched when overwriteExisting is true.
func AutoMatchModelVendors(overwriteExisting bool) (AutoMatchModelVendorsResult, error) {
	result := AutoMatchModelVendorsResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var vendors []Vendor
		if err := tx.Where("status = ?", 1).Order("id ASC").Find(&vendors).Error; err != nil {
			return err
		}
		nameIndex, aliasIndex := buildVendorMatchIndexes(vendors)
		vendorsByID := make(map[int]Vendor, len(vendors))
		for _, vendor := range vendors {
			vendorsByID[vendor.Id] = vendor
		}

		var models []Model
		if err := tx.Select("id", "model_name", "vendor_id", "icon").Order("id ASC").Find(&models).Error; err != nil {
			return err
		}

		assignments := make(map[int][]int)
		for _, modelMeta := range models {
			if modelMeta.VendorID != 0 && !overwriteExisting {
				result.SkippedCount++
				continue
			}
			vendorID, ambiguous := matchModelVendor(modelMeta.ModelName, nameIndex, aliasIndex)
			if ambiguous {
				result.AmbiguousCount++
				continue
			}
			if vendorID == 0 {
				result.UnmatchedCount++
				continue
			}
			matchedVendor := vendorsByID[vendorID]
			if modelMeta.VendorID == vendorID && modelMeta.Icon == matchedVendor.Icon {
				result.SkippedCount++
				continue
			}
			assignments[vendorID] = append(assignments[vendorID], modelMeta.Id)
		}

		vendorIDs := make([]int, 0, len(assignments))
		for vendorID := range assignments {
			vendorIDs = append(vendorIDs, vendorID)
		}
		sort.Ints(vendorIDs)

		now := common.GetTimestamp()
		for _, vendorID := range vendorIDs {
			updatedCount, err := updateAutoMatchedModelVendors(tx, assignments[vendorID], vendorID, vendorsByID[vendorID].Icon, now, overwriteExisting)
			if err != nil {
				return err
			}
			result.UpdatedCount += updatedCount
		}
		return nil
	})
	return result, err
}

func BatchUpdateModelCategoryTags(ids []int, categoryTags []string) (int64, error) {
	categoryTagSet := map[string]struct{}{
		"text":  {},
		"image": {},
		"video": {},
		"stt":   {},
		"tts":   {},
	}
	nextCategoryTags := make([]string, 0, len(categoryTags))
	seenCategoryTags := make(map[string]struct{}, len(categoryTags))
	for _, tag := range categoryTags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if _, ok := categoryTagSet[normalized]; !ok {
			continue
		}
		if _, ok := seenCategoryTags[normalized]; ok {
			continue
		}
		seenCategoryTags[normalized] = struct{}{}
		nextCategoryTags = append(nextCategoryTags, normalized)
	}

	var updatedCount int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var models []Model
		if err := tx.Where("id IN ?", ids).Find(&models).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		for _, modelMeta := range models {
			mergedTags := make([]string, 0)
			seenTags := make(map[string]struct{})
			for _, tag := range strings.Split(modelMeta.Tags, ",") {
				trimmed := strings.TrimSpace(tag)
				if trimmed == "" {
					continue
				}
				if _, ok := categoryTagSet[strings.ToLower(trimmed)]; ok {
					continue
				}
				if _, ok := seenTags[trimmed]; ok {
					continue
				}
				seenTags[trimmed] = struct{}{}
				mergedTags = append(mergedTags, trimmed)
			}
			for _, tag := range nextCategoryTags {
				if _, ok := seenTags[tag]; ok {
					continue
				}
				seenTags[tag] = struct{}{}
				mergedTags = append(mergedTags, tag)
			}

			result := tx.Model(&Model{}).Where("id = ?", modelMeta.Id).Updates(map[string]interface{}{
				"tags":         strings.Join(mergedTags, ","),
				"updated_time": now,
			})
			if result.Error != nil {
				return result.Error
			}
			updatedCount += result.RowsAffected
		}
		return nil
	})
	return updatedCount, err
}

func normalizeModelCategories(categories []string) []string {
	normalized := make([]string, 0, len(categories))
	seen := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		for _, part := range strings.Split(category, ",") {
			value := strings.TrimSpace(part)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func BatchUpdateModelCategories(ids []int, categories []string) (int64, error) {
	nextCategories := normalizeModelCategories(categories)
	result := DB.Model(&Model{}).Where("id IN ?", ids).Updates(map[string]interface{}{
		"categories":   strings.Join(nextCategories, ","),
		"updated_time": common.GetTimestamp(),
	})
	return result.RowsAffected, result.Error
}

func (mi *Model) Delete() error {
	_, err := DeleteModelMetadata([]int{mi.Id}, false, false)
	return err
}

type ModelDeleteResult struct {
	DeletedCount    int `json:"deleted_count"`
	UpdatedChannels int `json:"updated_channels"`
}

// DeleteModelMetadata optionally removes exact model names from every channel.
// Channel removal requires exact-match metadata records. Pricing removal
// clears the selected names without expanding metadata matching rules.
func DeleteModelMetadata(ids []int, removeFromChannels, removePricing bool) (ModelDeleteResult, error) {
	result := ModelDeleteResult{}
	if len(ids) == 0 || len(ids) > 1000 {
		return result, errors.New("select between 1 and 1000 models")
	}
	selected := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return result, errors.New("invalid model ID")
		}
		selected[id] = struct{}{}
	}
	modelIDs := make([]int, 0, len(selected))
	for id := range selected {
		modelIDs = append(modelIDs, id)
	}
	sort.Ints(modelIDs)
	names := make(map[string]struct{}, len(modelIDs))
	deleteRecords := func(tx *gorm.DB) error {
		var records []Model
		if err := lockForUpdate(tx).Where("id IN ?", modelIDs).Order("id").Find(&records).Error; err != nil {
			return err
		}
		if len(records) != len(modelIDs) {
			return errors.New("selected models changed; reload before deleting")
		}
		for _, record := range records {
			if removeFromChannels && record.NameRule != NameRuleExact {
				return errors.New("only exact-match models can be removed from channels")
			}
			names[record.ModelName] = struct{}{}
		}
		if removeFromChannels {
			var channels []Channel
			// Read only routing fields. Lock channels in a consistent order, then
			// update models and abilities in the same transaction as metadata.
			if err := lockForUpdate(tx).Select("id", "models", "status", "group", "priority", "weight", "tag").Order("id").Find(&channels).Error; err != nil {
				return err
			}
			for _, channel := range channels {
				models := channel.GetModels()
				remaining := make([]string, 0, len(models))
				for _, name := range models {
					if _, remove := names[strings.TrimSpace(name)]; !remove {
						remaining = append(remaining, name)
					}
				}
				if len(remaining) == len(models) {
					continue
				}
				channel.Models = strings.Join(remaining, ",")
				if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Update("models", channel.Models).Error; err != nil {
					return err
				}
				if err := channel.UpdateAbilities(tx); err != nil {
					return err
				}
				result.UpdatedChannels++
			}
		}
		if err := tx.Where("id IN ?", modelIDs).Delete(&Model{}).Error; err != nil {
			return err
		}
		result.DeletedCount = len(records)
		return nil
	}
	var err error
	if removePricing {
		// Use the pricing mutation path so both option persistence and runtime
		// publication stay serialized with ordinary pricing saves. All database
		// writes share one transaction; runtime prices publish only after commit.
		metadataMutationMu.Lock()
		defer metadataMutationMu.Unlock()
		err = mutateModelPricingOptions(func(tx *gorm.DB, values map[string]map[string]any) error {
			if err := lockMetadataMutation(tx); err != nil {
				return err
			}
			if err := deleteRecords(tx); err != nil {
				return err
			}
			for _, entries := range values {
				for name := range names {
					delete(entries, name)
				}
			}
			return nil
		})
	} else {
		err = metadataTransaction(deleteRecords)
	}
	if err != nil {
		return ModelDeleteResult{}, err
	}
	if result.UpdatedChannels > 0 {
		InitChannelCache()
	}
	RefreshPricing()
	return result, nil
}

func GetVendorModelCounts() (map[int64]int64, error) {
	var stats []struct {
		VendorID int64
		Count    int64
	}
	if err := DB.Model(&Model{}).
		Select("vendor_id as vendor_id, count(*) as count").
		Group("vendor_id").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(stats))
	for _, s := range stats {
		m[s.VendorID] = s.Count
	}
	return m, nil
}

func GetAllModels(offset int, limit int) ([]*Model, error) {
	models, _, err := SearchModels("", "", "", "", offset, limit)
	return models, err
}

func applyModelsMetaFilters(db *gorm.DB, filter ModelsMetaFilter) *gorm.DB {
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		db = db.Where(
			"model_name LIKE ? OR description LIKE ? OR tags LIKE ? OR categories LIKE ?",
			like,
			like,
			like,
			like,
		)
	}
	if filter.Vendor != "" {
		if vendorID, err := strconv.Atoi(filter.Vendor); err == nil {
			db = db.Where("models.vendor_id = ?", vendorID)
		} else {
			db = db.Joins("JOIN vendors ON vendors.id = models.vendor_id").Where("vendors.name LIKE ?", "%"+filter.Vendor+"%")
		}
	}
	if filter.Status != nil {
		db = db.Where("models.status = ?", *filter.Status)
	}
	if filter.SyncOfficial != nil {
		db = db.Where("models.sync_official = ?", *filter.SyncOfficial)
	}
	if filter.Tag != "" {
		if filter.Tag == "__empty__" {
			db = db.Where("models.tags IS NULL OR TRIM(models.tags) = ''")
		} else {
			db = db.Where(
				"models.tags = ? OR models.tags LIKE ? OR models.tags LIKE ? OR models.tags LIKE ?",
				filter.Tag,
				filter.Tag+",%",
				"%,"+filter.Tag,
				"%,"+filter.Tag+",%",
			)
		}
	}
	if filter.Category != "" {
		if filter.Category == "__empty__" {
			db = db.Where("models.categories IS NULL OR TRIM(models.categories) = ''")
		} else {
			db = db.Where(
				"models.categories = ? OR models.categories LIKE ? OR models.categories LIKE ? OR models.categories LIKE ?",
				filter.Category,
				filter.Category+",%",
				"%,"+filter.Category,
				"%,"+filter.Category+",%",
			)
		}
	}
	return db
}

func GetModelTagCounts(filter ModelsMetaFilter) (map[string]int64, error) {
	filter.Tag = ""
	db := applyModelsMetaFilters(DB.Model(&Model{}), filter)

	var models []Model
	if err := db.Select("tags").Find(&models).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int64)
	for _, modelMeta := range models {
		tags := strings.Split(modelMeta.Tags, ",")
		hasTag := false
		seen := make(map[string]struct{}, len(tags))
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			counts[tag]++
			hasTag = true
		}
		if !hasTag {
			counts["__empty__"]++
		}
	}
	return counts, nil
}

func GetModelCategoryCounts(filter ModelsMetaFilter) (map[string]int64, error) {
	filter.Category = ""
	db := applyModelsMetaFilters(DB.Model(&Model{}), filter)

	var models []Model
	if err := db.Select("categories").Find(&models).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int64)
	for _, modelMeta := range models {
		categories := strings.Split(modelMeta.Categories, ",")
		hasCategory := false
		seen := make(map[string]struct{}, len(categories))
		for _, category := range categories {
			category = strings.TrimSpace(category)
			if category == "" {
				continue
			}
			if _, ok := seen[category]; ok {
				continue
			}
			seen[category] = struct{}{}
			counts[category]++
			hasCategory = true
		}
		if !hasCategory {
			counts["__empty__"]++
		}
	}
	return counts, nil
}

func ListModelsMeta(filter ModelsMetaFilter, offset int, limit int) ([]*Model, int64, error) {
	db := applyModelsMetaFilters(DB.Model(&Model{}), filter)
	orderClause := modelsMetaSortClause(filter.SortBy, filter.SortOrder)

	if filter.HasPrice == nil {
		var total int64
		if err := db.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		var models []*Model
		if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&models).Error; err != nil {
			return nil, 0, err
		}
		return models, total, nil
	}

	var candidates []*Model
	if err := db.Order(orderClause).Find(&candidates).Error; err != nil {
		return nil, 0, err
	}

	pricingItems := GetPricing()
	filtered := make([]*Model, 0, len(candidates))
	for _, modelMeta := range candidates {
		modelMeta.HasPrice = ModelMetaHasBillingConfig(modelMeta, pricingItems)
		if modelMeta.HasPrice == *filter.HasPrice {
			filtered = append(filtered, modelMeta)
		}
	}

	total := int64(len(filtered))
	if offset >= len(filtered) {
		return []*Model{}, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

func GetBoundChannelsByModelsMap(modelNames []string) (map[string][]BoundChannel, error) {
	result := make(map[string][]BoundChannel)
	if len(modelNames) == 0 {
		return result, nil
	}
	type row struct {
		Model string
		Name  string
		Type  int
	}
	var rows []row
	err := DB.Table("channels").
		Select("abilities.model as model, channels.name as name, channels.type as type").
		Joins("JOIN abilities ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ?", modelNames, true).
		Distinct().
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.Model] = append(result[r.Model], BoundChannel{Name: r.Name, Type: r.Type})
	}
	return result, nil
}

// ModelConnection describes an enabled route independently of catalog visibility or price.
type ModelConnection struct {
	AbilityWithChannel
	ChannelName string `json:"channel_name"`
}

func GetModelConnections() ([]ModelConnection, error) {
	var connections []ModelConnection
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type, channels.name as channel_name").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).
		Order("abilities.model, abilities.channel_id").
		Scan(&connections).Error
	return connections, err
}

func normalizeLookupValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func GetPreferredModelOwnerChannelTypes(modelNames []string, groups []string) (map[string]int, error) {
	result := make(map[string]int)
	modelNames = normalizeLookupValues(modelNames)
	if len(modelNames) == 0 {
		return result, nil
	}

	type row struct {
		Model       string
		ChannelType int
	}
	var rows []row

	query := DB.Table("abilities").
		Select("abilities.model as model, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ?", modelNames, true, common.ChannelStatusEnabled).
		Scopes(excludeActiveDisabledModels).
		Order("COALESCE(abilities.priority, 0) DESC").
		Order("abilities.weight DESC").
		Order("abilities.channel_id ASC")

	groups = normalizeLookupValues(groups)
	if len(groups) > 0 {
		query = query.Where("abilities."+commonGroupCol+" IN ?", groups)
	}

	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, r := range rows {
		if _, ok := result[r.Model]; ok {
			continue
		}
		result[r.Model] = r.ChannelType
	}
	return result, nil
}

func SearchModels(keyword string, vendor string, status string, syncOfficial string, offset int, limit int) ([]*Model, int64, error) {
	filter := ModelsMetaFilter{Keyword: keyword, Vendor: vendor}
	if statusValue, ok := parseModelStatusFilter(status); ok {
		filter.Status = &statusValue
	}
	if syncValue, ok := parseModelSyncFilter(syncOfficial); ok {
		filter.SyncOfficial = &syncValue
	}
	return ListModelsMeta(filter, offset, limit)
}

// parseModelStatusFilter maps UI/API status values to the models.status column.
// Returns ok=false when no status filter should be applied.
func parseModelStatusFilter(status string) (value int, ok bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "all":
		return 0, false
	case "enabled", "1":
		return 1, true
	case "disabled", "0":
		return 0, true
	default:
		n, err := strconv.Atoi(status)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}

// parseModelSyncFilter maps UI/API sync values to the models.sync_official column.
// Returns ok=false when no sync filter should be applied.
func parseModelSyncFilter(syncOfficial string) (value int, ok bool) {
	switch strings.ToLower(strings.TrimSpace(syncOfficial)) {
	case "", "all":
		return 0, false
	case "yes", "1":
		return 1, true
	case "no", "0":
		return 0, true
	default:
		n, err := strconv.Atoi(syncOfficial)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}
