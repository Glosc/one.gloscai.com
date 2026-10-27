package model

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRedemptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := DB
	oldLogDB := LOG_DB
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&Redemption{}))

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

func seedRedemption(t *testing.T, db *gorm.DB, redemption Redemption) Redemption {
	t.Helper()

	require.NoError(t, db.Create(&redemption).Error)
	return redemption
}

func TestGetAllRedemptionsFiltersDisplayedStatus(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := common.GetTimestamp()

	active := seedRedemption(t, db, Redemption{
		Key:         "active",
		Name:        "active",
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: now,
		ExpiredTime: 0,
	})
	seedRedemption(t, db, Redemption{
		Key:         "expired",
		Name:        "expired",
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: now,
		ExpiredTime: now - 60,
	})
	disabled := seedRedemption(t, db, Redemption{
		Key:         "disabled",
		Name:        "disabled",
		Status:      common.RedemptionCodeStatusDisabled,
		CreatedTime: now,
		ExpiredTime: now - 60,
	})

	redemptions, total, err := GetAllRedemptions(0, 10, "1", "id", "asc")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, active.Id, redemptions[0].Id)

	redemptions, total, err = GetAllRedemptions(0, 10, "2", "id", "asc")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, disabled.Id, redemptions[0].Id)
}

func TestGetAllRedemptionsFiltersExpiredStatus(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := common.GetTimestamp()

	expired := seedRedemption(t, db, Redemption{
		Key:         "expired-enabled",
		Name:        "expired-enabled",
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: now,
		ExpiredTime: now - 60,
	})
	seedRedemption(t, db, Redemption{
		Key:         "expired-disabled",
		Name:        "expired-disabled",
		Status:      common.RedemptionCodeStatusDisabled,
		CreatedTime: now,
		ExpiredTime: now - 60,
	})
	seedRedemption(t, db, Redemption{
		Key:         "active",
		Name:        "active",
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: now,
		ExpiredTime: now + 60,
	})

	redemptions, total, err := GetAllRedemptions(
		0,
		10,
		redemptionStatusExpiredFilter,
		"id",
		"asc",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, expired.Id, redemptions[0].Id)
}

func TestSearchRedemptionsCombinesKeywordAndStatus(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := common.GetTimestamp()

	disabled := seedRedemption(t, db, Redemption{
		Key:         "match-disabled",
		Name:        "Match disabled",
		Status:      common.RedemptionCodeStatusDisabled,
		CreatedTime: now,
	})
	seedRedemption(t, db, Redemption{
		Key:         "match-used",
		Name:        "Match used",
		Status:      common.RedemptionCodeStatusUsed,
		CreatedTime: now,
	})
	seedRedemption(t, db, Redemption{
		Key:         "other-disabled",
		Name:        "Other disabled",
		Status:      common.RedemptionCodeStatusDisabled,
		CreatedTime: now,
	})

	redemptions, total, err := SearchRedemptions(
		"Match",
		0,
		10,
		"2",
		"id",
		"asc",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, disabled.Id, redemptions[0].Id)
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}

func TestRedeemRejectsWalletOverflow(t *testing.T) {
	userId, key := setupRedeemFixture(t, 11)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxWalletQuota-10).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, common.MaxWalletQuota-10, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	setupRedeemFixture(t, 500)

	redemption := &Redemption{
		Name:        "overflow-redemption",
		Key:         "10000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       common.MaxWalletQuota + 1,
		CreatedTime: common.GetTimestamp(),
	}
	require.Error(t, redemption.Insert())
}

func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}
