package controller

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxVendorAutoAddCatalogItems = 500
	maxVendorAutoAddMatchKeys    = 64
	maxVendorFieldLength         = 128
	maxVendorAutoAddBodyBytes    = 1 << 20
	maxVendorBatchItems          = 500
)

var vendorCatalogIconPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:\.Color)?$`)

type autoAddVendorsRequest struct {
	Catalog []model.VendorAutoAddCatalogItem `json:"catalog"`
}

type batchUpdateVendorStatusRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

type batchDeleteVendorsRequest struct {
	Ids []int `json:"ids"`
}

func listVendors(c *gin.Context, keyword string) {
	filter := model.VendorListFilter{Keyword: keyword}
	if rawStatus := strings.TrimSpace(c.Query("status")); rawStatus != "" {
		status, err := strconv.Atoi(rawStatus)
		if err != nil || (status != 0 && status != 1) {
			common.ApiErrorMsg(c, "供应商状态筛选无效")
			return
		}
		filter.Status = &status
	}
	if rawHasIcon := strings.TrimSpace(c.Query("has_icon")); rawHasIcon != "" {
		var hasIcon bool
		switch strings.ToLower(rawHasIcon) {
		case "true", "1":
			hasIcon = true
		case "false", "0":
			hasIcon = false
		default:
			common.ApiErrorMsg(c, "供应商图标筛选无效")
			return
		}
		filter.HasIcon = &hasIcon
	}

	pageInfo := common.GetPageQuery(c)
	vendors, total, err := model.ListVendors(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), c.Query("association"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.ApiSuccess(c, pageInfo)
}

// GetAllVendors 获取供应商列表（分页）
func GetAllVendors(c *gin.Context) {
	listVendors(c, "")
}

// SearchVendors 搜索供应商
func SearchVendors(c *gin.Context) {
	listVendors(c, c.Query("keyword"))
}

func normalizeVendorBatchIDs(ids []int) ([]int, error) {
	if len(ids) == 0 || len(ids) > maxVendorBatchItems {
		return nil, errors.New("请选择 1 到 500 个供应商")
	}
	uniqueIDs := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("供应商 ID 无效")
		}
		uniqueIDs[id] = struct{}{}
	}
	normalizedIDs := make([]int, 0, len(uniqueIDs))
	for id := range uniqueIDs {
		normalizedIDs = append(normalizedIDs, id)
	}
	sort.Ints(normalizedIDs)
	return normalizedIDs, nil
}

// AutoAddVendors 根据当前模型命名空间和前端提供的 LobeHub 图标目录补充供应商。
func AutoAddVendors(c *gin.Context) {
	var req autoAddVendorsRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxVendorAutoAddBodyBytes)
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Catalog) == 0 {
		common.ApiErrorMsg(c, "供应商目录不能为空")
		return
	}
	if len(req.Catalog) > maxVendorAutoAddCatalogItems {
		common.ApiErrorMsg(c, "供应商目录最多包含 500 项")
		return
	}

	for index := range req.Catalog {
		candidate := &req.Catalog[index]
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.Alias = strings.TrimSpace(candidate.Alias)
		candidate.Icon = strings.TrimSpace(candidate.Icon)
		if candidate.Name == "" {
			common.ApiErrorMsg(c, "供应商名称不能为空")
			return
		}
		if utf8.RuneCountInString(candidate.Name) > maxVendorFieldLength {
			common.ApiErrorMsg(c, "供应商名称不能超过 128 个字符")
			return
		}
		if utf8.RuneCountInString(candidate.Alias) > maxVendorFieldLength {
			common.ApiErrorMsg(c, "供应商别名不能超过 128 个字符")
			return
		}
		if candidate.Icon == "" {
			common.ApiErrorMsg(c, "供应商图标不能为空")
			return
		}
		if utf8.RuneCountInString(candidate.Icon) > maxVendorFieldLength || !vendorCatalogIconPattern.MatchString(candidate.Icon) {
			common.ApiErrorMsg(c, "供应商图标格式无效")
			return
		}
		if len(candidate.MatchKeys) == 0 {
			common.ApiErrorMsg(c, "供应商匹配键不能为空")
			return
		}
		if len(candidate.MatchKeys) > maxVendorAutoAddMatchKeys {
			common.ApiErrorMsg(c, "每个供应商最多包含 64 个匹配键")
			return
		}
		for keyIndex := range candidate.MatchKeys {
			candidate.MatchKeys[keyIndex] = strings.TrimSpace(candidate.MatchKeys[keyIndex])
			if candidate.MatchKeys[keyIndex] == "" {
				common.ApiErrorMsg(c, "供应商匹配键不能为空")
				return
			}
			if utf8.RuneCountInString(candidate.MatchKeys[keyIndex]) > maxVendorFieldLength {
				common.ApiErrorMsg(c, "供应商匹配键不能超过 128 个字符")
				return
			}
		}
	}

	result, err := model.AutoAddVendorsFromCatalog(req.Catalog)
	if err != nil {
		vendorAPIError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, result)
}

// GetVendorMeta 根据 ID 获取供应商
func GetVendorMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		vendorAPIError(c, err)
		return
	}
	v, err := model.GetVendorByID(id)
	if err != nil {
		vendorAPIError(c, err)
		return
	}
	common.ApiSuccess(c, v)
}

// CreateVendorMeta 新建供应商
func CreateVendorMeta(c *gin.Context) {
	var v model.Vendor
	if err := c.ShouldBindJSON(&v); err != nil {
		vendorAPIError(c, err)
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	v.Alias = strings.TrimSpace(v.Alias)
	if v.Name == "" {
		common.ApiErrorMsg(c, "供应商名称不能为空")
		return
	}
	// 创建前先检查名称
	if dup, err := model.IsVendorNameDuplicated(0, v.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "供应商名称已存在")
		return
	}
	// 别名用于名称匹配，需保证唯一
	if dup, err := model.IsVendorAliasDuplicated(0, v.Alias); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "供应商别名已存在")
		return
	}

	if err := v.Insert(); err != nil {
		vendorAPIError(c, err)
		return
	}
	model.RefreshPricing()

	recordManageAudit(c, "vendor.metadata.save", map[string]any{"vendor_id": v.Id, "name": v.Name})
	common.ApiSuccess(c, &v)
}

// UpdateVendorMeta 更新供应商
func UpdateVendorMeta(c *gin.Context) {
	var v model.Vendor
	if err := c.ShouldBindJSON(&v); err != nil {
		vendorAPIError(c, err)
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	v.Alias = strings.TrimSpace(v.Alias)
	if v.Id == 0 {
		common.ApiErrorT(c, "Missing vendor ID")
		return
	}
	if v.Name == "" {
		common.ApiErrorMsg(c, "供应商名称不能为空")
		return
	}
	// 名称冲突检查
	if dup, err := model.IsVendorNameDuplicated(v.Id, v.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "供应商名称已存在")
		return
	}
	// 别名冲突检查
	if dup, err := model.IsVendorAliasDuplicated(v.Id, v.Alias); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "供应商别名已存在")
		return
	}

	if err := v.Update(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "供应商不存在")
			return
		}
		common.ApiError(c, err)
		return
	}
	updatedVendor, err := model.GetVendorByID(v.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, updatedVendor)
}

func BatchUpdateVendorStatus(c *gin.Context) {
	var req batchUpdateVendorStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeVendorBatchIDs(req.Ids)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if req.Status != 0 && req.Status != 1 {
		common.ApiErrorMsg(c, "供应商状态无效")
		return
	}

	updatedCount, err := model.BatchUpdateVendorStatus(ids, req.Status)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if updatedCount > 0 {
		model.RefreshPricing()
	}
	common.ApiSuccess(c, gin.H{"updated_count": updatedCount})
}

func BatchDeleteVendors(c *gin.Context) {
	var req batchDeleteVendorsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := normalizeVendorBatchIDs(req.Ids)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	deletedCount, blockedNames, err := model.BatchDeleteVendors(ids)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "部分供应商不存在")
			return
		}
		common.ApiError(c, err)
		return
	}
	if len(blockedNames) > 0 {
		common.ApiErrorMsg(c, "以下供应商仍被模型引用："+strings.Join(blockedNames, "、"))
		return
	}
	if deletedCount > 0 {
		model.RefreshPricing()
	}
	common.ApiSuccess(c, gin.H{"deleted_count": deletedCount})
}

func RemoveUnusedVendors(c *gin.Context) {
	deletedCount, err := model.RemoveUnusedVendors()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if deletedCount > 0 {
		model.RefreshPricing()
	}
	common.ApiSuccess(c, gin.H{"deleted_count": deletedCount})
}

// DeleteVendorMeta 删除供应商
func DeleteVendorMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		vendorAPIError(c, err)
		return
	}
	if err := model.DeleteVendors([]int{id}); err != nil {
		vendorAPIError(c, err)
		return
	}
	recordManageAudit(c, "vendor.metadata.delete", map[string]any{"vendor_id": id})
	common.ApiSuccess(c, nil)
}

func vendorAPIError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	payload := gin.H{"success": false, "message": err.Error()}
	var references *model.VendorReferenceError
	if errors.Is(err, model.ErrVendorConflict) {
		status = http.StatusConflict
		payload["code"] = "VENDOR_CONFLICT"
	}
	if errors.As(err, &references) {
		status = http.StatusConflict
		payload["code"] = "VENDOR_REFERENCED"
		payload["reference_counts"] = references.Counts
	}
	c.JSON(status, payload)
}

func PreviewVendorOperation(c *gin.Context) {
	var request model.VendorOperation
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		vendorAPIError(c, err)
		return
	}
	preview, err := model.PreviewVendorOperation(request)
	if err != nil {
		vendorAPIError(c, err)
		return
	}
	common.ApiSuccess(c, preview)
}

func ApplyVendorOperation(c *gin.Context) {
	var request model.VendorOperation
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		vendorAPIError(c, err)
		return
	}
	result, err := model.ApplyVendorOperation(request)
	if err != nil {
		vendorAPIError(c, err)
		return
	}
	recordManageAudit(c, "vendor."+request.Action, map[string]any{"source_vendor_ids": request.VendorIDs, "target_vendor_id": request.TargetVendorID, "updated_model_ids": result.UpdatedModels, "deleted_vendor_ids": result.DeletedVendors})
	common.ApiSuccess(c, result)
}
