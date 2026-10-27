package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type vendorMetaTestResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Data    *model.Vendor `json:"data"`
}

type autoAddVendorsTestResponse struct {
	Success bool                       `json:"success"`
	Message string                     `json:"message"`
	Data    model.AutoAddVendorsResult `json:"data"`
}

type vendorListTestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []model.Vendor `json:"items"`
		Total int            `json:"total"`
	} `json:"data"`
}

type vendorBatchTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		UpdatedCount int64 `json:"updated_count"`
		DeletedCount int64 `json:"deleted_count"`
	} `json:"data"`
}

func setupVendorMetaControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.Model{},
		&model.Vendor{},
		&model.Option{},
	))

	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func newVendorMetaTestContext(t *testing.T, method string, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	return ctx, recorder
}

func decodeVendorMetaTestResponse(t *testing.T, recorder *httptest.ResponseRecorder) vendorMetaTestResponse {
	t.Helper()

	var response vendorMetaTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func decodeAutoAddVendorsTestResponse(t *testing.T, recorder *httptest.ResponseRecorder) autoAddVendorsTestResponse {
	t.Helper()

	var response autoAddVendorsTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func decodeVendorListTestResponse(t *testing.T, recorder *httptest.ResponseRecorder) vendorListTestResponse {
	t.Helper()

	var response vendorListTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func decodeVendorBatchTestResponse(t *testing.T, recorder *httptest.ResponseRecorder) vendorBatchTestResponse {
	t.Helper()

	var response vendorBatchTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestListVendorsFiltersByStatusAndIcon(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	vendors := []model.Vendor{
		{Name: "Enabled With Icon", Icon: "OpenAI", Status: 1},
		{Name: "Enabled Without Icon", Status: 1},
		{Name: "Disabled With Icon", Icon: "Anthropic", Status: 1},
		{Name: "Disabled Without Icon", Status: 1},
	}
	for index := range vendors {
		require.NoError(t, db.Create(&vendors[index]).Error)
	}
	require.NoError(t, db.Model(&model.Vendor{}).
		Where("id IN ?", []int{vendors[2].Id, vendors[3].Id}).
		Update("status", 0).Error)

	ctx, recorder := newVendorMetaTestContext(t, http.MethodGet, "/api/vendors/?status=1&has_icon=true", "")
	GetAllVendors(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeVendorListTestResponse(t, recorder)
	require.True(t, response.Success)
	assert.Equal(t, 1, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, "Enabled With Icon", response.Data.Items[0].Name)

	searchCtx, searchRecorder := newVendorMetaTestContext(
		t,
		http.MethodGet,
		"/api/vendors/search?keyword=without&status=0&has_icon=false",
		"",
	)
	SearchVendors(searchCtx)

	require.Equal(t, http.StatusOK, searchRecorder.Code)
	searchResponse := decodeVendorListTestResponse(t, searchRecorder)
	require.True(t, searchResponse.Success)
	assert.Equal(t, 1, searchResponse.Data.Total)
	require.Len(t, searchResponse.Data.Items, 1)
	assert.Equal(t, "Disabled Without Icon", searchResponse.Data.Items[0].Name)
}

func TestBatchUpdateVendorStatus(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	enabled := model.Vendor{Name: "Enabled", Status: 1}
	disabled := model.Vendor{Name: "Disabled", Status: 1}
	require.NoError(t, db.Create(&enabled).Error)
	require.NoError(t, db.Create(&disabled).Error)
	require.NoError(t, db.Model(&model.Vendor{}).Where("id = ?", disabled.Id).Update("status", 0).Error)

	body, err := common.Marshal(gin.H{
		"ids":    []int{enabled.Id, disabled.Id, enabled.Id},
		"status": 0,
	})
	require.NoError(t, err)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPut, "/api/vendors/batch_status", string(body))
	BatchUpdateVendorStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeVendorBatchTestResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, int64(1), response.Data.UpdatedCount)

	var persisted []model.Vendor
	require.NoError(t, db.Order("id ASC").Find(&persisted).Error)
	require.Len(t, persisted, 2)
	assert.Zero(t, persisted[0].Status)
	assert.Zero(t, persisted[1].Status)
}

func TestBatchDeleteVendorsIsAtomicWhenVendorIsReferenced(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	freeVendor := model.Vendor{Name: "Free Vendor", Status: 1}
	inUseVendor := model.Vendor{Name: "In Use Vendor", Status: 1}
	require.NoError(t, db.Create(&freeVendor).Error)
	require.NoError(t, db.Create(&inUseVendor).Error)
	require.NoError(t, db.Create(&model.Model{
		ModelName: "in-use/model",
		VendorID:  inUseVendor.Id,
		Status:    1,
	}).Error)

	body, err := common.Marshal(gin.H{"ids": []int{freeVendor.Id, inUseVendor.Id}})
	require.NoError(t, err)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/batch_delete", string(body))
	BatchDeleteVendors(ctx)

	response := decodeVendorBatchTestResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, inUseVendor.Name)
	for _, vendorID := range []int{freeVendor.Id, inUseVendor.Id} {
		_, err := model.GetVendorByID(vendorID)
		require.NoError(t, err)
	}

	freeBody, err := common.Marshal(gin.H{"ids": []int{freeVendor.Id}})
	require.NoError(t, err)
	freeCtx, freeRecorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/batch_delete", string(freeBody))
	BatchDeleteVendors(freeCtx)

	freeResponse := decodeVendorBatchTestResponse(t, freeRecorder)
	require.True(t, freeResponse.Success, freeResponse.Message)
	assert.Equal(t, int64(1), freeResponse.Data.DeletedCount)
	assert.ErrorIs(t, db.First(&model.Vendor{}, freeVendor.Id).Error, gorm.ErrRecordNotFound)
	_, err = model.GetVendorByID(inUseVendor.Id)
	require.NoError(t, err)
}

func TestRemoveUnusedVendorsPreservesReferencedVendorsAndIsIdempotent(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	usedVendor := model.Vendor{Name: "Used Vendor", Status: 1}
	unusedVendor := model.Vendor{Name: "Unused Vendor", Status: 1}
	disabledUnusedVendor := model.Vendor{Name: "Disabled Unused Vendor", Status: 1}
	for _, vendor := range []*model.Vendor{&usedVendor, &unusedVendor, &disabledUnusedVendor} {
		require.NoError(t, db.Create(vendor).Error)
	}
	require.NoError(t, db.Model(&model.Vendor{}).
		Where("id = ?", disabledUnusedVendor.Id).
		Update("status", 0).Error)
	require.NoError(t, db.Create(&model.Model{
		ModelName: "used/model",
		VendorID:  usedVendor.Id,
		Status:    1,
	}).Error)

	ctx, recorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/remove_unused", "")
	RemoveUnusedVendors(ctx)

	response := decodeVendorBatchTestResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, int64(2), response.Data.DeletedCount)
	_, err := model.GetVendorByID(usedVendor.Id)
	require.NoError(t, err)
	for _, vendorID := range []int{unusedVendor.Id, disabledUnusedVendor.Id} {
		assert.ErrorIs(t, db.First(&model.Vendor{}, vendorID).Error, gorm.ErrRecordNotFound)
	}

	secondCtx, secondRecorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/remove_unused", "")
	RemoveUnusedVendors(secondCtx)
	secondResponse := decodeVendorBatchTestResponse(t, secondRecorder)
	require.True(t, secondResponse.Success, secondResponse.Message)
	assert.Zero(t, secondResponse.Data.DeletedCount)
}

func TestAutoAddVendorsValidatesCatalogAndCreatesMatchedVendor(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	require.NoError(t, db.Create(&model.Model{ModelName: "anthropic/claude-3-5-haiku", Icon: "model-icon"}).Error)

	body, err := common.Marshal(gin.H{
		"catalog": []gin.H{{
			"name":       " Anthropic ",
			"alias":      " Claude ",
			"icon":       " Anthropic.Color ",
			"match_keys": []string{" anthropic "},
		}},
	})
	require.NoError(t, err)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/auto_add", string(body))
	AutoAddVendors(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeAutoAddVendorsTestResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, 1, response.Data.CreatedCount)
	assert.Equal(t, []string{"anthropic"}, response.Data.CreatedNamespaces)
	require.Len(t, response.Data.CreatedVendors, 1)
	assert.Equal(t, "Anthropic", response.Data.CreatedVendors[0].Name)
	assert.Equal(t, "Claude", response.Data.CreatedVendors[0].Alias)
	assert.Equal(t, "Anthropic.Color", response.Data.CreatedVendors[0].Icon)

	var persistedModel model.Model
	require.NoError(t, db.First(&persistedModel).Error)
	assert.Zero(t, persistedModel.VendorID)
	assert.Equal(t, "model-icon", persistedModel.Icon)
}

func TestAutoAddVendorsRejectsInvalidCatalogWithoutWriting(t *testing.T) {
	setupVendorMetaControllerTestDB(t)

	tooManyCatalogItems := make([]model.VendorAutoAddCatalogItem, maxVendorAutoAddCatalogItems+1)
	for index := range tooManyCatalogItems {
		tooManyCatalogItems[index] = model.VendorAutoAddCatalogItem{
			Name:      fmt.Sprintf("Vendor%d", index),
			Icon:      "OpenAI",
			MatchKeys: []string{fmt.Sprintf("vendor%d", index)},
		}
	}
	tooManyBody, err := common.Marshal(gin.H{"catalog": tooManyCatalogItems})
	require.NoError(t, err)

	tests := []struct {
		name    string
		body    string
		message string
	}{
		{name: "empty catalog", body: `{"catalog":[]}`, message: "供应商目录不能为空"},
		{name: "blank name", body: `{"catalog":[{"name":" ","icon":"OpenAI","match_keys":["openai"]}]}`, message: "供应商名称不能为空"},
		{name: "blank icon", body: `{"catalog":[{"name":"OpenAI","icon":" ","match_keys":["openai"]}]}`, message: "供应商图标不能为空"},
		{name: "unsafe icon", body: `{"catalog":[{"name":"OpenAI","icon":"OpenAI.Avatar.type={'platform'}","match_keys":["openai"]}]}`, message: "供应商图标格式无效"},
		{name: "blank match key", body: `{"catalog":[{"name":"OpenAI","icon":"OpenAI","match_keys":[" "]}]}`, message: "供应商匹配键不能为空"},
		{name: "too many catalog items", body: string(tooManyBody), message: "供应商目录最多包含 500 项"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/auto_add", test.body)
			AutoAddVendors(ctx)

			response := decodeAutoAddVendorsTestResponse(t, recorder)
			assert.False(t, response.Success)
			assert.Equal(t, test.message, response.Message)
		})
	}

	oversizedBody := fmt.Sprintf(
		`{"catalog":[{"name":"OpenAI","alias":"%s","icon":"OpenAI","match_keys":["openai"]}]}`,
		strings.Repeat("a", maxVendorAutoAddBodyBytes),
	)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/auto_add", oversizedBody)
	AutoAddVendors(ctx)
	response := decodeAutoAddVendorsTestResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "request body too large")

	var count int64
	require.NoError(t, model.DB.Model(&model.Vendor{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateVendorMetaPersistsEditableFieldsAndPreservesCreatedTime(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	const originalCreatedTime int64 = 1_700_000_000

	vendor := model.Vendor{
		Name:        "Original",
		Alias:       "original-alias",
		Description: "original description",
		Icon:        "OriginalIcon",
		Status:      1,
		CreatedTime: originalCreatedTime,
		UpdatedTime: originalCreatedTime,
	}
	require.NoError(t, db.Create(&vendor).Error)

	body := fmt.Sprintf(`{
		"id": %d,
		"name": "Updated",
		"alias": "updated-alias",
		"description": "updated description",
		"icon": "UpdatedIcon",
		"status": 0,
		"created_time": 1,
		"updated_time": 1
	}`, vendor.Id)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPut, "/api/vendors/", body)
	UpdateVendorMeta(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeVendorMetaTestResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	require.NotNil(t, response.Data)
	assert.Equal(t, originalCreatedTime, response.Data.CreatedTime)
	assert.Equal(t, "updated-alias", response.Data.Alias)
	assert.Zero(t, response.Data.Status)

	var got model.Vendor
	require.NoError(t, db.First(&got, vendor.Id).Error)
	assert.Equal(t, "Updated", got.Name)
	assert.Equal(t, "updated-alias", got.Alias)
	assert.Equal(t, "updated description", got.Description)
	assert.Equal(t, "UpdatedIcon", got.Icon)
	assert.Zero(t, got.Status)
	assert.Equal(t, originalCreatedTime, got.CreatedTime)
	assert.Greater(t, got.UpdatedTime, originalCreatedTime)

	var cachedVendor *model.PricingVendor
	for _, candidate := range model.GetVendors() {
		if candidate.ID == vendor.Id {
			candidate := candidate
			cachedVendor = &candidate
			break
		}
	}
	require.NotNil(t, cachedVendor)
	assert.Equal(t, "Updated", cachedVendor.Name)
	assert.Equal(t, "updated-alias", cachedVendor.Alias)
	assert.Equal(t, "UpdatedIcon", cachedVendor.Icon)
}

func TestCreateVendorMetaRejectsBlankAndAmbiguousNames(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	existing := model.Vendor{
		Name:        "Existing",
		Alias:       "known-alias",
		Status:      1,
		CreatedTime: 100,
		UpdatedTime: 100,
	}
	require.NoError(t, db.Create(&existing).Error)

	tests := []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "blank name",
			body:    `{"name":"   ","status":1}`,
			message: "供应商名称不能为空",
		},
		{
			name:    "name matches another vendor alias",
			body:    `{"name":"known-alias","status":1}`,
			message: "供应商名称已存在",
		},
		{
			name:    "alias matches another vendor name",
			body:    `{"name":"New Vendor","alias":"Existing","status":1}`,
			message: "供应商别名已存在",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newVendorMetaTestContext(t, http.MethodPost, "/api/vendors/", test.body)
			CreateVendorMeta(ctx)

			response := decodeVendorMetaTestResponse(t, recorder)
			assert.False(t, response.Success)
			assert.Equal(t, test.message, response.Message)
		})
	}
}

func TestUpdateVendorMetaRejectsBlankName(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	vendor := model.Vendor{
		Name:        "Original",
		Alias:       "original-alias",
		Description: "original description",
		Status:      1,
		CreatedTime: 100,
		UpdatedTime: 100,
	}
	require.NoError(t, db.Create(&vendor).Error)

	body := fmt.Sprintf(`{"id":%d,"name":"   ","alias":"changed","status":0}`, vendor.Id)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPut, "/api/vendors/", body)
	UpdateVendorMeta(ctx)

	response := decodeVendorMetaTestResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Equal(t, "供应商名称不能为空", response.Message)

	var got model.Vendor
	require.NoError(t, db.First(&got, vendor.Id).Error)
	assert.Equal(t, "Original", got.Name)
	assert.Equal(t, "original-alias", got.Alias)
	assert.Equal(t, 1, got.Status)
	assert.Equal(t, int64(100), got.UpdatedTime)
}

func TestUpdateVendorMetaDoesNotUpsertMissingID(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	const missingID = 404

	body := fmt.Sprintf(`{"id":%d,"name":"Missing","alias":"missing-alias","status":1}`, missingID)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodPut, "/api/vendors/", body)
	UpdateVendorMeta(ctx)

	response := decodeVendorMetaTestResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Equal(t, "供应商不存在", response.Message)

	var count int64
	require.NoError(t, db.Unscoped().Model(&model.Vendor{}).Where("id = ?", missingID).Count(&count).Error)
	assert.Zero(t, count)
}

func TestDeleteVendorMetaSoftDeletesAndReportsMissingID(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	vendor := model.Vendor{
		Name:        "Delete Me",
		Alias:       "delete-me",
		Status:      1,
		CreatedTime: 100,
		UpdatedTime: 100,
	}
	require.NoError(t, db.Create(&vendor).Error)
	target := fmt.Sprintf("/api/vendors/%d", vendor.Id)

	ctx, recorder := newVendorMetaTestContext(t, http.MethodDelete, target, "")
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", vendor.Id)}}
	DeleteVendorMeta(ctx)

	response := decodeVendorMetaTestResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.ErrorIs(t, db.First(&model.Vendor{}, vendor.Id).Error, gorm.ErrRecordNotFound)

	var deleted model.Vendor
	require.NoError(t, db.Unscoped().First(&deleted, vendor.Id).Error)
	assert.True(t, deleted.DeletedAt.Valid)

	missingCtx, missingRecorder := newVendorMetaTestContext(t, http.MethodDelete, target, "")
	missingCtx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", vendor.Id)}}
	DeleteVendorMeta(missingCtx)

	missingResponse := decodeVendorMetaTestResponse(t, missingRecorder)
	assert.False(t, missingResponse.Success)
	assert.Contains(t, missingResponse.Message, "source vendor does not exist")
}

func TestDeleteVendorMetaRejectsVendorReferencedByModels(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	vendor := model.Vendor{
		Name:        "In Use",
		Status:      1,
		CreatedTime: 100,
		UpdatedTime: 100,
	}
	require.NoError(t, db.Create(&vendor).Error)
	require.NoError(t, db.Create(&model.Model{
		ModelName:   "referencing-model",
		VendorID:    vendor.Id,
		Status:      1,
		CreatedTime: 100,
		UpdatedTime: 100,
	}).Error)

	target := fmt.Sprintf("/api/vendors/%d", vendor.Id)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodDelete, target, "")
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", vendor.Id)}}
	DeleteVendorMeta(ctx)

	response := decodeVendorMetaTestResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Equal(t, "vendors are still referenced by models; transfer or clear their assignments first", response.Message)
	_, err := model.GetVendorByID(vendor.Id)
	require.NoError(t, err)
}

func TestDeleteVendorMetaDoesNotRecreateDefaultVendorDuringPricingRefresh(t *testing.T) {
	db := setupVendorMetaControllerTestDB(t)
	vendor := model.Vendor{
		Name:        "OpenAI",
		Icon:        "OpenAI",
		Status:      1,
		CreatedTime: 100,
		UpdatedTime: 100,
	}
	require.NoError(t, db.Create(&vendor).Error)

	channel := model.Channel{
		Name:   "OpenAI channel",
		Key:    "test-key",
		Status: 1,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-delete-test",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	target := fmt.Sprintf("/api/vendors/%d", vendor.Id)
	ctx, recorder := newVendorMetaTestContext(t, http.MethodDelete, target, "")
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", vendor.Id)}}
	DeleteVendorMeta(ctx)

	response := decodeVendorMetaTestResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var activeCount int64
	require.NoError(t, db.Model(&model.Vendor{}).Where("name = ?", "OpenAI").Count(&activeCount).Error)
	assert.Zero(t, activeCount)
	for _, cachedVendor := range model.GetVendors() {
		assert.NotEqual(t, "OpenAI", cachedVendor.Name)
	}
}
