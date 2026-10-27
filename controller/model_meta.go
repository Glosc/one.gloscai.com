package controller

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type batchUpdateModelVendorRequest struct {
	Ids      []int   `json:"ids"`
	VendorID *int    `json:"vendor_id"`
	Icon     *string `json:"icon"`
}

type autoMatchModelVendorsRequest struct {
	OverwriteExisting bool `json:"overwrite_existing"`
}

type batchUpdateModelCategoryTagsRequest struct {
	Ids  []int    `json:"ids"`
	Tags []string `json:"tags"`
}

type batchUpdateModelCategoriesRequest struct {
	Ids        []int    `json:"ids"`
	Categories []string `json:"categories"`
}

func parseModelIntFilter(raw string, enabledValues map[string]struct{}, disabledValues map[string]struct{}) *int {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == "all" {
		return nil
	}
	if _, ok := enabledValues[value]; ok {
		parsed := 1
		return &parsed
	}
	if _, ok := disabledValues[value]; ok {
		parsed := 0
		return &parsed
	}
	return nil
}

func parseModelBoolFilter(raw string) *bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "set", "configured", "yes", "true", "1":
		parsed := true
		return &parsed
	case "unset", "unconfigured", "no", "false", "0":
		parsed := false
		return &parsed
	default:
		return nil
	}
}

func getModelsMetaFilter(c *gin.Context) model.ModelsMetaFilter {
	priceFilter := c.Query("has_price")
	if strings.TrimSpace(priceFilter) == "" {
		priceFilter = c.Query("price_configured")
	}

	return model.ModelsMetaFilter{
		Keyword:  c.Query("keyword"),
		Vendor:   c.Query("vendor"),
		Tag:      strings.TrimSpace(c.Query("tag")),
		Category: strings.TrimSpace(c.Query("category")),
		Status: parseModelIntFilter(
			c.Query("status"),
			map[string]struct{}{"enabled": {}, "enable": {}, "true": {}, "1": {}},
			map[string]struct{}{"disabled": {}, "disable": {}, "false": {}, "0": {}},
		),
		SyncOfficial: parseModelIntFilter(
			c.Query("sync_official"),
			map[string]struct{}{"yes": {}, "sync": {}, "official": {}, "true": {}, "1": {}},
			map[string]struct{}{"no": {}, "none": {}, "false": {}, "0": {}},
		),
		HasPrice:  parseModelBoolFilter(priceFilter),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
	}
}

// GetAllModelsMeta 获取模型列表（分页）
func GetAllModelsMeta(c *gin.Context) {
	listModelsMeta(c, "", "")
}

// SearchModelsMeta 搜索模型列表
func SearchModelsMeta(c *gin.Context) {
	listModelsMeta(c, c.Query("keyword"), c.Query("vendor"))
}

func listModelsMeta(c *gin.Context, keyword, vendor string) {
	squareState := model.ModelSquareState(c.Query("square_state"))
	switch squareState {
	case "", model.ModelSquareVisible, model.ModelSquareUnavailable, model.ModelSquareHidden, model.ModelSquarePartial:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid model square state"})
		return
	}

	pageInfo := common.GetPageQuery(c)
	if squareState != "" && (pageInfo.GetPage() < 1 || pageInfo.GetPageSize() < 1) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid pagination"})
		return
	}
	offset, limit := pageInfo.GetStartIdx(), pageInfo.GetPageSize()
	if squareState != "" {
		// Visibility depends on live channels and metadata rules. Filter the
		// enriched candidate set before counting and paginating the results.
		offset, limit = 0, -1
	}
	filter := getModelsMetaFilter(c)
	filter.Keyword, filter.Vendor = keyword, vendor
	search := model.SearchModels
	if c.Query("include_channel_models") == "true" {
		search = model.SearchModelsWithChannels
	}
	var modelsMeta []*model.Model
	var total int64
	var err error
	if c.Query("include_channel_models") == "true" {
		modelsMeta, total, err = search(keyword, vendor, c.Query("status"), c.Query("sync_official"), offset, limit)
	} else {
		modelsMeta, total, err = model.ListModelsMeta(filter, offset, limit)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := enrichModels(modelsMeta); err != nil {
		common.ApiError(c, err)
		return
	}
	if squareState != "" {
		filtered := make([]*model.Model, 0, len(modelsMeta))
		for _, metadata := range modelsMeta {
			if metadata.SquareState == squareState {
				filtered = append(filtered, metadata)
			}
		}
		total = int64(len(filtered))
		start := len(filtered)
		if pageInfo.GetPage()-1 <= len(filtered)/pageInfo.GetPageSize() {
			start = (pageInfo.GetPage() - 1) * pageInfo.GetPageSize()
		}
		end := min(start+pageInfo.GetPageSize(), len(filtered))
		modelsMeta = filtered[start:end]
	}

	vendorCounts, _ := model.GetVendorModelCounts()
	tagCounts, _ := model.GetModelTagCounts(filter)
	categoryCounts, _ := model.GetModelCategoryCounts(filter)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, gin.H{
		"vendor_counts":   vendorCounts,
		"items":           modelsMeta,
		"total":           total,
		"page":            pageInfo.GetPage(),
		"page_size":       pageInfo.GetPageSize(),
		"tag_counts":      tagCounts,
		"category_counts": categoryCounts,
	})
}

// GetModelMeta 根据 ID 获取单条模型信息
func GetModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var m model.Model
	if err := model.DB.First(&m, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := enrichModels([]*model.Model{&m}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &m)
}

// CreateModelMeta 新建模型
func CreateModelMeta(c *gin.Context) {
	var m model.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.ModelName == "" {
		common.ApiErrorT(c, "Model name cannot be empty")
		return
	}
	if err := model.ValidateMetadataValues(model.MetadataValues{Endpoints: m.Endpoints, Status: m.Status, NameRule: m.NameRule}); err != nil {
		common.ApiError(c, err)
		return
	}
	// 名称冲突检查
	if dup, err := model.IsModelNameDuplicated(0, m.ModelName); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorT(c, "Model name already exists")
		return
	}

	if err := m.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	m.HasMetadata = m.Id > 0
	common.ApiSuccess(c, &m)
}

// UpdateModelMeta 更新模型
func UpdateModelMeta(c *gin.Context) {
	statusOnly := c.Query("status_only") == "true"

	var m model.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.Id == 0 {
		common.ApiErrorT(c, "Missing model ID")
		return
	}

	if statusOnly {
		if m.Status != 0 && m.Status != 1 {
			common.ApiErrorMsg(c, "invalid catalog visibility")
			return
		}
		// 只更新状态，防止误清空其他字段
		if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Update("status", m.Status).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		if strings.TrimSpace(m.ModelName) == "" {
			common.ApiErrorT(c, "Model name cannot be empty")
			return
		}
		if err := model.ValidateMetadataValues(model.MetadataValues{Endpoints: m.Endpoints, Status: m.Status, NameRule: m.NameRule}); err != nil {
			common.ApiError(c, err)
			return
		}
		// 名称冲突检查
		if dup, err := model.IsModelNameDuplicated(m.Id, m.ModelName); err != nil {
			common.ApiError(c, err)
			return
		} else if dup {
			common.ApiErrorT(c, "Model name already exists")
			return
		}

		if err := m.Update(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	model.RefreshPricing()
	m.HasMetadata = m.Id > 0
	common.ApiSuccess(c, &m)
}

func BatchUpdateModelVendor(c *gin.Context) {
	var req batchUpdateModelVendorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择至少一个模型")
		return
	}
	if req.VendorID == nil || *req.VendorID < 0 {
		common.ApiErrorMsg(c, "缺少供应商 ID")
		return
	}
	if *req.VendorID > 0 {
		if _, err := model.GetVendorByID(*req.VendorID); err != nil {
			common.ApiErrorMsg(c, "供应商不存在")
			return
		}
	}

	updatedCount, err := model.BatchUpdateModelVendor(req.Ids, *req.VendorID, req.Icon)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, gin.H{"updated_count": updatedCount})
}

func AutoMatchModelVendors(c *gin.Context) {
	var req autoMatchModelVendorsRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	result, err := model.AutoMatchModelVendors(req.OverwriteExisting)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, result)
}

func BatchUpdateModelCategoryTags(c *gin.Context) {
	var req batchUpdateModelCategoryTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择至少一个模型")
		return
	}
	allowedTags := map[string]struct{}{
		"text":  {},
		"image": {},
		"video": {},
		"stt":   {},
		"tts":   {},
	}
	for _, tag := range req.Tags {
		if _, ok := allowedTags[strings.ToLower(strings.TrimSpace(tag))]; !ok {
			common.ApiErrorMsg(c, "标签只能是 text、image、video、stt 或 tts")
			return
		}
	}

	updatedCount, err := model.BatchUpdateModelCategoryTags(req.Ids, req.Tags)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, gin.H{"updated_count": updatedCount})
}

func BatchUpdateModelCategories(c *gin.Context) {
	var req batchUpdateModelCategoriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择至少一个模型")
		return
	}

	updatedCount, err := model.BatchUpdateModelCategories(req.Ids, req.Categories)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, gin.H{"updated_count": updatedCount})
}

// DeleteModelMeta 删除模型
func DeleteModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	removeFromChannels, err := strconv.ParseBool(c.DefaultQuery("remove_from_channels", "false"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	removePricing, err := strconv.ParseBool(c.DefaultQuery("remove_pricing", "false"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if removePricing && c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Model pricing is managed by a super administrator."})
		return
	}
	result, err := model.DeleteModelMetadata([]int{id}, removeFromChannels, removePricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "model.delete", map[string]any{"model_ids": []int{id}, "remove_from_channels": removeFromChannels, "remove_pricing": removePricing, "updated_channels": result.UpdatedChannels})
	common.ApiSuccess(c, result)
}

func BatchDeleteModelMeta(c *gin.Context) {
	var request struct {
		ModelIDs           []int `json:"model_ids"`
		RemoveFromChannels bool  `json:"remove_from_channels"`
		RemovePricing      bool  `json:"remove_pricing"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.RemovePricing && c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Model pricing is managed by a super administrator."})
		return
	}
	result, err := model.DeleteModelMetadata(request.ModelIDs, request.RemoveFromChannels, request.RemovePricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "model.delete_batch", map[string]any{"model_ids": request.ModelIDs, "remove_from_channels": request.RemoveFromChannels, "remove_pricing": request.RemovePricing, "updated_channels": result.UpdatedChannels})
	common.ApiSuccess(c, result)
}

// enrichModels keeps configured endpoints intact and derives connections from
// enabled routes, including hidden or unpriced models absent from the catalog.
func enrichModels(models []*model.Model) error {
	if len(models) == 0 {
		return nil
	}
	configured, err := model.GetConfiguredModelChannels()
	if err != nil {
		return err
	}
	for _, metadata := range models {
		if metadata == nil {
			continue
		}
		metadata.HasMetadata = metadata.Id > 0
		channelIDs := make(map[int]struct{})
		for name, ids := range configured {
			if metadata.MatchesName(name) {
				for _, id := range ids {
					channelIDs[id] = struct{}{}
				}
			}
		}
		metadata.ConfiguredChannelCount = len(channelIDs)
	}
	connections, err := model.GetModelConnections()
	if err != nil {
		return err
	}
	if err := model.FillModelSquareStates(models, configured, connections); err != nil {
		return err
	}
	for _, metadata := range models {
		if metadata == nil {
			continue
		}
		channels := make(map[int]model.BoundChannel)
		groups := make(map[string]bool)
		names := make(map[string]bool)
		endpoints := make(map[string]bool)
		quotas := make(map[int]bool)
		for _, connection := range connections {
			name := connection.Model
			if !metadata.MatchesName(name) {
				continue
			}
			names[name] = true
			groups[connection.Group] = true
			channels[connection.ChannelId] = model.BoundChannel{Name: connection.ChannelName, Type: connection.ChannelType}
			for _, endpoint := range model.GetModelSupportEndpointTypes(name) {
				endpoints[string(endpoint)] = true
			}
			for _, quota := range model.GetModelQuotaTypes(name) {
				quotas[quota] = true
			}
		}
		metadata.BoundChannels = nil
		metadata.EnableGroups = nil
		metadata.SupportedEndpoints = nil
		metadata.QuotaTypes = nil
		metadata.MatchedModels = nil
		for _, channel := range channels {
			metadata.BoundChannels = append(metadata.BoundChannels, channel)
		}
		sort.Slice(metadata.BoundChannels, func(i, j int) bool {
			a, b := metadata.BoundChannels[i], metadata.BoundChannels[j]
			if a.Name == b.Name {
				return a.Type < b.Type
			}
			return a.Name < b.Name
		})
		for group := range groups {
			metadata.EnableGroups = append(metadata.EnableGroups, group)
		}
		for endpoint := range endpoints {
			metadata.SupportedEndpoints = append(metadata.SupportedEndpoints, endpoint)
		}
		for quota := range quotas {
			metadata.QuotaTypes = append(metadata.QuotaTypes, quota)
		}
		sort.Strings(metadata.EnableGroups)
		sort.Strings(metadata.SupportedEndpoints)
		sort.Ints(metadata.QuotaTypes)
		if metadata.NameRule != model.NameRuleExact {
			for name := range names {
				metadata.MatchedModels = append(metadata.MatchedModels, name)
			}
			sort.Strings(metadata.MatchedModels)
			metadata.MatchedCount = len(names)
		}
	}
	return nil
}
