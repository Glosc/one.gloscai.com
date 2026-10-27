package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

type Redemption struct {
	Id           int            `json:"id"`
	UserId       int            `json:"user_id"`
	Key          string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status       int            `json:"status" gorm:"default:1"`
	Name         string         `json:"name" gorm:"index"`
	Quota        int            `json:"quota" gorm:"default:100"`
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime int64          `json:"redeemed_time" gorm:"bigint"`
	Count        int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId   int            `json:"used_user_id"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
	ExpiredTime  int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

var redemptionAllowedSorts = map[string]string{
	"id":            "id",
	"name":          "name",
	"status":        "status",
	"quota":         "quota",
	"created_time":  "created_time",
	"expired_time":  "expired_time",
	"used_user_id":  "used_user_id",
	"redeemed_time": "redeemed_time",
}

func redemptionSortClause(sortBy string, sortOrder string) string {
	return safeSortClause(sortBy, redemptionAllowedSorts, "id", sortOrder)
}

const redemptionStatusExpiredFilter = "expired"

func applyRedemptionStatusFilter(query *gorm.DB, status string) *gorm.DB {
	status = strings.TrimSpace(status)
	if status == "" {
		return query
	}

	if status == redemptionStatusExpiredFilter {
		return query.Where(
			"status = ? AND expired_time != 0 AND expired_time < ?",
			common.RedemptionCodeStatusEnabled,
			common.GetTimestamp(),
		)
	}

	statusValue, err := strconv.Atoi(status)
	if err != nil {
		return query
	}

	switch statusValue {
	case common.RedemptionCodeStatusEnabled:
		return query.Where(
			"status = ? AND (expired_time = 0 OR expired_time >= ?)",
			statusValue,
			common.GetTimestamp(),
		)
	case common.RedemptionCodeStatusDisabled, common.RedemptionCodeStatusUsed:
		return query.Where("status = ?", statusValue)
	default:
		return query
	}
}

func GetAllRedemptions(
	startIdx int,
	num int,
	status string,
	sortBy string,
	sortOrder string,
) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := applyRedemptionStatusFilter(tx.Model(&Redemption{}), status)

	// 获取总数
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = query.Order(redemptionSortClause(sortBy, sortOrder)).
		Limit(num).
		Offset(startIdx).
		Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(
	keyword string,
	startIdx int,
	num int,
	status string,
	sortBy string,
	sortOrder string,
) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Build query based on keyword type
	query := applyRedemptionStatusFilter(tx.Model(&Redemption{}), status)
	keyword = strings.TrimSpace(keyword)

	// Only try to convert to ID if the string represents a valid integer
	if keyword != "" {
		if id, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
		} else {
			query = query.Where("name LIKE ?", keyword+"%")
		}
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = query.Order(redemptionSortClause(sortBy, sortOrder)).
		Limit(num).
		Offset(startIdx).
		Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, common.NewMessage("ID is empty")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func Redeem(key string, userId int) (quota int, err error) {
	if key == "" {
		return 0, errors.New("redemption code not provided")
	}
	if userId == 0 {
		return 0, errors.New("invalid user id")
	}
	redemption := &Redemption{}

	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			return errors.New("invalid redemption code")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("redemption code already used")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("redemption code expired")
		}
		// Compare-and-swap on status: only the transaction that flips
		// enabled -> used may credit quota, so a concurrent redeem of the
		// same code loses here even without a row lock (e.g. on SQLite).
		result := tx.Model(&Redemption{}).
			Where("id = ? AND status = ?", redemption.Id, common.RedemptionCodeStatusEnabled).
			Updates(map[string]any{
				"redeemed_time": common.GetTimestamp(),
				"status":        common.RedemptionCodeStatusUsed,
				"used_user_id":  userId,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("redemption code already used")
		}
		return creditTopUpQuota(tx, userId, redemption.Quota, nil)
	})
	if err != nil {
		common.SysError(common.LogText("redemption failed: %s", err.Error()))
		return 0, ErrRedeemFailed
	}
	syncCreditUserQuotaCache(userId, redemption.Quota, common.LogText("redemption"))
	RecordLog(userId, LogTypeTopup, common.NewMessage("Topped up {{quota}} with a redemption code, code ID {{id}}", map[string]any{"quota": logger.FormatQuota(redemption.Quota), "id": redemption.Id}))
	return redemption.Quota, nil
}

func (redemption *Redemption) Insert() error {
	if redemption.Quota <= 0 {
		return errors.New("redemption quota must be positive")
	}
	if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
		return err
	}
	var err error
	err = DB.Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate() error {
	// This can update zero values
	return DB.Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update() error {
	if redemption.Quota <= 0 {
		return errors.New("redemption quota must be positive")
	}
	if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
		return err
	}
	var err error
	err = DB.Model(redemption).Select("name", "status", "quota", "redeemed_time", "expired_time").Updates(redemption).Error
	return err
}

func (redemption *Redemption) Delete() error {
	var err error
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return common.NewMessage("ID is empty")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}

// BatchDeleteRedemptions soft-deletes the selected codes in one statement.
func BatchDeleteRedemptions(ids []int) (int64, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return 0, errors.New("select between 1 and 1000 redemption codes")
	}
	for _, id := range ids {
		if id <= 0 {
			return 0, errors.New("redemption IDs must be positive")
		}
	}
	result := DB.Where("id IN ?", ids).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
