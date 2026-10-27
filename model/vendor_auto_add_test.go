package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVendorAutoAddTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := DB
	oldLogDB := LOG_DB
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&Model{}, &Vendor{}, &Option{}))

	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLogDB
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestAutoAddVendorsFromCatalogCreatesOnlyReferencedMissingVendors(t *testing.T) {
	db := setupVendorAutoAddTestDB(t)

	openAI := Vendor{Name: "OpenAI", Description: "preserve me", Icon: "ExistingOpenAI", Status: 1}
	alibaba := Vendor{Name: "Alibaba Cloud", Icon: "AlibabaCloud.Color", Status: 1}
	meta := Vendor{Name: "Meta", Icon: "Ollama", Status: 1}
	deletedCohere := Vendor{Name: "Cohere Legacy", Alias: "Cohere", Icon: "Cohere.Color", Status: 1}
	for _, vendor := range []*Vendor{&openAI, &alibaba, &meta, &deletedCohere} {
		require.NoError(t, vendor.Insert())
	}
	require.NoError(t, db.Model(&Vendor{}).Where("id = ?", openAI.Id).Update("status", 0).Error)
	require.NoError(t, db.Delete(&deletedCohere).Error)

	models := []Model{
		{ModelName: "openai/gpt-5", VendorID: 42, Icon: "model-openai"},
		{ModelName: " Open-AI/gpt-4o ", Icon: "model-openai-duplicate"},
		{ModelName: "anthropic/claude-3-5", Icon: "model-anthropic"},
		{ModelName: "moonshotai/kimi-k2", Icon: "model-moonshot"},
		{ModelName: "moonshot/kimi-latest", Icon: "model-moonshot-canonical"},
		{ModelName: "unknown/model", Icon: "model-unknown"},
		{ModelName: "shared/model", Icon: "model-shared"},
		{ModelName: "cohere/command-r", Icon: "model-cohere"},
		{ModelName: "alibaba/qwen-max", Icon: "model-alibaba"},
		{ModelName: "ollama/llama-3", Icon: "model-ollama"},
		{ModelName: "model-without-namespace", Icon: "model-plain"},
		{ModelName: "/empty-namespace", Icon: "model-empty"},
	}
	for index := range models {
		require.NoError(t, db.Create(&models[index]).Error)
	}

	catalog := []VendorAutoAddCatalogItem{
		{Name: "OpenAI Catalog", Icon: "OpenAI", MatchKeys: []string{"openai"}},
		{Name: "Anthropic", Icon: "Anthropic.Color", MatchKeys: []string{"anthropic"}},
		{Name: "Moonshot", Alias: "月之暗面", Icon: "Moonshot", MatchKeys: []string{"moonshot"}},
		{Name: "Alibaba", Alias: "Alibaba Cloud", Icon: "AlibabaCloud.Color", MatchKeys: []string{"alibaba"}},
		{Name: "Ollama", Icon: "Ollama", MatchKeys: []string{"ollama"}},
		{Name: "Cohere", Icon: "Cohere.Color", MatchKeys: []string{"cohere"}},
		{Name: "Shared One", Icon: "OpenRouter", MatchKeys: []string{"shared"}},
		{Name: "Shared Two", Icon: "Together", MatchKeys: []string{"shared"}},
		{Name: "Unused", Icon: "Mistral.Color", MatchKeys: []string{"unused"}},
	}

	result, err := AutoAddVendorsFromCatalog(catalog)
	require.NoError(t, err)
	assert.Equal(t, int64(len(models)), result.ScannedModelCount)
	assert.Equal(t, 9, result.ScannedNamespaceCount)
	assert.Equal(t, 3, result.CreatedCount)
	assert.Equal(t, 4, result.CreatedNamespaceCount)
	assert.Equal(t, 2, result.ExistingCount)
	assert.Equal(t, 1, result.UnmatchedCount)
	assert.Equal(t, 1, result.AmbiguousCount)
	assert.Equal(t, 1, result.DeletedConflictCount)
	assert.Equal(t, []string{"anthropic", "moonshot", "moonshotai", "ollama"}, result.CreatedNamespaces)
	assert.Equal(t, []string{"alibaba", "openai"}, result.ExistingNamespaces)
	assert.Equal(t, []string{"unknown"}, result.UnmatchedNamespaces)
	assert.Equal(t, []string{"shared"}, result.AmbiguousNamespaces)
	assert.Equal(t, []string{"cohere"}, result.DeletedConflictNamespaces)
	require.Len(t, result.CreatedVendors, 3)
	assert.Equal(t, "Anthropic", result.CreatedVendors[0].Name)
	assert.Equal(t, "Anthropic.Color", result.CreatedVendors[0].Icon)
	assert.Equal(t, "Moonshot", result.CreatedVendors[1].Name)
	assert.Equal(t, "月之暗面", result.CreatedVendors[1].Alias)
	assert.Equal(t, 1, result.CreatedVendors[1].Status)
	assert.Positive(t, result.CreatedVendors[1].CreatedTime)
	assert.Equal(t, result.CreatedVendors[1].CreatedTime, result.CreatedVendors[1].UpdatedTime)
	assert.Equal(t, "Ollama", result.CreatedVendors[2].Name)

	var persistedOpenAI Vendor
	require.NoError(t, db.First(&persistedOpenAI, openAI.Id).Error)
	assert.Zero(t, persistedOpenAI.Status)
	assert.Equal(t, "preserve me", persistedOpenAI.Description)
	assert.Equal(t, "ExistingOpenAI", persistedOpenAI.Icon)

	var persistedModels []Model
	require.NoError(t, db.Order("id ASC").Find(&persistedModels).Error)
	require.Len(t, persistedModels, len(models))
	for index := range persistedModels {
		assert.Equal(t, models[index].VendorID, persistedModels[index].VendorID)
		assert.Equal(t, models[index].Icon, persistedModels[index].Icon)
	}

	var activeVendorCount int64
	require.NoError(t, db.Model(&Vendor{}).Count(&activeVendorCount).Error)
	assert.Equal(t, int64(6), activeVendorCount)

	secondResult, err := AutoAddVendorsFromCatalog(catalog)
	require.NoError(t, err)
	assert.Zero(t, secondResult.CreatedCount)
	assert.Empty(t, secondResult.CreatedVendors)
	assert.Empty(t, secondResult.CreatedNamespaces)
	assert.Equal(t, []string{"alibaba", "anthropic", "moonshot", "moonshotai", "ollama", "openai"}, secondResult.ExistingNamespaces)
	assert.Equal(t, []string{"unknown"}, secondResult.UnmatchedNamespaces)
	assert.Equal(t, []string{"shared"}, secondResult.AmbiguousNamespaces)
	assert.Equal(t, []string{"cohere"}, secondResult.DeletedConflictNamespaces)
	require.NoError(t, db.Model(&Vendor{}).Count(&activeVendorCount).Error)
	assert.Equal(t, int64(6), activeVendorCount)
}
