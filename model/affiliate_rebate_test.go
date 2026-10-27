package model

import (
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAffiliateRebateLogsHidesInviteeUsername(t *testing.T) {
	truncateTables(t)

	inviter := &User{
		Id:       301,
		Username: "rebate_privacy_inviter", AffCode: "rebate_privacy_inviter",
		Status: common.UserStatusEnabled,
	}
	invitee := &User{
		Id:       302,
		Username: "private_invitee_name", AffCode: "private_invitee_name",
		Status:    common.UserStatusEnabled,
		InviterId: inviter.Id,
	}
	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)
	require.NoError(t, DB.Create(&AffiliateRebate{
		InviterId:       inviter.Id,
		InviteeId:       invitee.Id,
		TopUpId:         1,
		TradeNo:         "rebate-order-privacy",
		RechargeQuota:   1000,
		RechargeAmount:  1,
		RebateQuota:     50,
		RebateRatio:     5,
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: PaymentProviderEpay,
		CreatedAt:       123456,
	}).Error)

	pageInfo := &common.PageInfo{Page: 1, PageSize: 10}
	logs, total, err := GetAffiliateRebateLogs(inviter.Id, "", pageInfo)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, invitee.Id, logs[0].InviteeId)
	assert.Empty(t, logs[0].InviteeUsername)

	logs, total, err = GetAffiliateRebateLogs(inviter.Id, invitee.Username, pageInfo)
	require.NoError(t, err)
	assert.Empty(t, logs)
	assert.Equal(t, int64(0), total)

	logs, total, err = GetAffiliateRebateLogs(inviter.Id, "302", pageInfo)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, invitee.Id, logs[0].InviteeId)
}

func TestAffiliateRebateQuotaRejectsUnsafeAmounts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		quota   int
		ratio   float64
		want    int
		wantErr bool
	}{
		{name: "ordinary reward truncates", quota: 123, ratio: 5, want: 6},
		{name: "disabled", quota: common.MaxWalletQuota, ratio: 0},
		{name: "int32 boundary", quota: common.MaxQuota, ratio: 100, want: common.MaxQuota},
		{name: "oversized wallet reward", quota: common.MaxWalletQuota, ratio: 100, wantErr: true},
		{name: "nan", quota: 100, ratio: math.NaN(), wantErr: true},
		{name: "infinity", quota: 100, ratio: math.Inf(1), wantErr: true},
		{name: "negative", quota: 100, ratio: -1, wantErr: true},
		{name: "above full recharge", quota: 100, ratio: 101, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota, err := calculateAffiliateRebateQuota(tc.quota, tc.ratio)
			if tc.wantErr {
				require.Error(t, err)
				assert.Zero(t, quota)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, quota)
		})
	}
}

func TestAffiliateRebateCapacityPreservesPendingTopUp(t *testing.T) {
	oldRatio, oldUnit := common.AffiliateRebateRatio, common.QuotaPerUnit
	payment := operation_setting.GetPaymentSetting()
	oldCompliance, oldTerms := payment.ComplianceConfirmed, payment.ComplianceTermsVersion
	common.AffiliateRebateRatio, common.QuotaPerUnit = 10, 500000
	payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		common.AffiliateRebateRatio, common.QuotaPerUnit = oldRatio, oldUnit
		payment.ComplianceConfirmed, payment.ComplianceTermsVersion = oldCompliance, oldTerms
	})
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			if dialect == "sqlite" {
				driver = sqlite.Open(":memory:")
			} else {
				dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dsn == "" {
					t.Skip("database test DSN not configured")
				}
				if dialect == "mysql" {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.Open(dsn)
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "rebate_capacity_" + common.GetRandomString(8) + "_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousLogDB, previousMain, previousLog, previousRedis := DB, LOG_DB, common.MainDatabaseType(), common.LogDatabaseType(), common.RedisEnabled
			DB, LOG_DB, common.RedisEnabled = db, db, false
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&AffiliateRebate{}, &TopUp{}, &Log{}, &User{}))
				DB, LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
				common.SetDatabaseTypes(previousMain, previousLog)
				initCol()
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &Log{}, &AffiliateRebate{}))
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			for _, tc := range []struct {
				name             string
				balance, history int
				wantErr          bool
			}{
				{name: "exact maximum", balance: common.MaxQuota - 100000, history: common.MaxQuota - 100000},
				{name: "balance overflow", balance: common.MaxQuota - 99999, wantErr: true},
				{name: "history overflow", history: common.MaxQuota - 99999, wantErr: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Cleanup(func() {
						for _, table := range []any{&AffiliateRebate{}, &TopUp{}, &Log{}, &User{}} {
							require.NoError(t, DB.Unscoped().Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error)
						}
					})
					inviter := &User{Id: 310, Username: "rebate_capacity_inviter", AffCode: "rebate_capacity_inviter", Status: common.UserStatusEnabled, AffQuota: tc.balance, AffHistoryQuota: tc.history}
					invitee := &User{Id: 311, Username: "rebate_capacity_invitee", AffCode: "rebate_capacity_invitee", Status: common.UserStatusEnabled, InviterId: inviter.Id, Quota: 7}
					require.NoError(t, DB.Create(inviter).Error)
					require.NoError(t, DB.Create(invitee).Error)
					insertTopUpForPaymentGuardTest(t, "rebate-capacity-order", invitee.Id, PaymentProviderEpay)
					err := RechargeDirectTopUp("rebate-capacity-order", PaymentProviderEpay, PaymentMethodAlipay, "127.0.0.1")
					var updated User
					require.NoError(t, DB.First(&updated, inviter.Id).Error)
					var count int64
					require.NoError(t, DB.Model(&AffiliateRebate{}).Count(&count).Error)
					if tc.wantErr {
						require.Error(t, err)
						assert.Equal(t, tc.balance, updated.AffQuota)
						assert.Equal(t, tc.history, updated.AffHistoryQuota)
						assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, invitee.Id))
						assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "rebate-capacity-order"))
						assert.Zero(t, count)
						return
					}
					require.NoError(t, err)
					assert.Equal(t, common.MaxQuota, updated.AffQuota)
					assert.Equal(t, common.MaxQuota, updated.AffHistoryQuota)
					assert.Equal(t, 1000007, getUserQuotaForPaymentGuardTest(t, invitee.Id))
					assert.Equal(t, int64(1), count)
				})
			}
			for _, tc := range []struct {
				name             string
				wallet, transfer int
				wantErr          bool
			}{
				{name: "transfer reaches wallet ceiling", wallet: common.MaxWalletQuota - 500000, transfer: 500000},
				{name: "transfer rejects wallet overflow", wallet: common.MaxWalletQuota - 499999, transfer: 500000, wantErr: true},
				{name: "transfer rejects negative quantity", wallet: 7, transfer: -500000, wantErr: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					user := &User{Id: 312, Username: "rebate_transfer", AffCode: "rebate_transfer", Status: common.UserStatusEnabled, Quota: tc.wallet, AffQuota: 500000}
					require.NoError(t, DB.Create(user).Error)
					t.Cleanup(func() { require.NoError(t, DB.Unscoped().Delete(&User{}, user.Id).Error) })
					err := user.TransferAffQuotaToQuota(tc.transfer)
					var updated User
					require.NoError(t, DB.First(&updated, user.Id).Error)
					if tc.wantErr {
						require.Error(t, err)
						assert.Equal(t, tc.wallet, updated.Quota)
						assert.Equal(t, 500000, updated.AffQuota, "a rejected transfer must not consume invitation rewards")
						return
					}
					require.NoError(t, err)
					assert.Equal(t, common.MaxWalletQuota, updated.Quota)
					assert.Zero(t, updated.AffQuota)
				})
			}

		})
	}
}
