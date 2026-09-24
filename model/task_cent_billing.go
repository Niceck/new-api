package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// SettleRoundedTaskQuota commits the task marker and all ledger deltas together.
// Reading the locked row makes retries (including stale polling copies) safe.
func SettleRoundedTaskQuota(task *Task, raw int, refund bool) (*common.BillingCharge, bool, error) {
	var previous *common.BillingCharge
	var current Task
	var delta int
	var tokenKey string
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		old := current.PrivateData.BillingCharge
		if old == nil || !old.Policy.Enabled {
			return fmt.Errorf("task has no CNY rounding snapshot")
		}
		copyOld := *old
		previous = &copyOld
		if old.Refunded {
			return nil
		}
		if refund {
			raw = 0
		}
		charged, err := old.Policy.Charge(raw)
		if err != nil {
			return err
		}
		if raw == old.RawQuota && charged == current.Quota && !refund {
			return nil
		}
		delta = charged - current.Quota
		rawDelta := raw - old.RawQuota
		if current.PrivateData.BillingSource == "subscription" {
			var sub UserSubscription
			if err := lockForUpdate(tx).First(&sub, current.PrivateData.SubscriptionId).Error; err != nil {
				return err
			}
			newUsed := sub.AmountUsed + int64(delta)
			if newUsed < 0 {
				newUsed = 0
			}
			if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
				return fmt.Errorf("subscription used exceeds total")
			}
			if err := tx.Model(&sub).Update("amount_used", newUsed).Error; err != nil {
				return err
			}
		} else if delta != 0 {
			result := tx.Model(&User{}).Where("id = ?", current.UserId).Update("quota", gorm.Expr("quota - ?", delta))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("task user missing")
			}
		}
		if current.PrivateData.TokenId > 0 && delta != 0 {
			var token Token
			err := tx.First(&token, current.PrivateData.TokenId).Error
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			if err == nil {
				tokenKey = token.Key
				if err := tx.Model(&token).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota - ?", delta), "used_quota": gorm.Expr("used_quota + ?", delta), "accessed_time": common.GetTimestamp()}).Error; err != nil {
					return err
				}
			}
		}
		if delta != 0 {
			if err := tx.Model(&User{}).Where("id = ?", current.UserId).Update("used_quota", gorm.Expr("used_quota + ?", delta)).Error; err != nil {
				return err
			}
		}
		if rawDelta != 0 {
			if err := tx.Model(&Channel{}).Where("id = ?", current.ChannelId).Update("used_quota", gorm.Expr("used_quota + ?", rawDelta)).Error; err != nil {
				return err
			}
		}
		current.Quota = charged
		current.PrivateData.BillingCharge = &common.BillingCharge{RawQuota: raw, ChargedQuota: charged, Policy: old.Policy, Refunded: refund}
		if err := tx.Model(&current).Updates(map[string]interface{}{"quota": current.Quota, "private_data": current.PrivateData}).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	task.Quota = current.Quota
	task.PrivateData = current.PrivateData
	if changed && delta != 0 {
		if current.PrivateData.BillingSource != "subscription" {
			if err := cacheIncrUserQuota(current.UserId, -int64(delta)); err != nil {
				common.SysError("task quota cache update: " + err.Error())
			}
		}
		if common.RedisEnabled && tokenKey != "" {
			if err := cacheIncrTokenQuota(tokenKey, -int64(delta)); err != nil {
				common.SysError("task token cache update: " + err.Error())
			}
		}
	}
	return previous, changed, nil
}

// RefundRoundedMidjourney preserves the original log quota while refunding the
// recorded wallet/token debit. Old rows keep their existing refund path.
func RefundRoundedMidjourney(task *Midjourney) (*common.BillingCharge, bool, error) {
	var current Midjourney
	var previous *common.BillingCharge
	var tokenKey string
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&current, task.Id).Error; err != nil {
			return err
		}
		if current.BillingCharge == nil || !current.BillingCharge.Policy.Enabled {
			return fmt.Errorf("missing Midjourney charge snapshot")
		}
		old := *current.BillingCharge
		previous = &old
		if old.Refunded {
			return nil
		}
		if err := tx.Model(&User{}).Where("id = ?", current.UserId).Updates(map[string]interface{}{"quota": gorm.Expr("quota + ?", current.Quota), "used_quota": gorm.Expr("used_quota - ?", current.Quota)}).Error; err != nil {
			return err
		}
		if current.TokenId > 0 {
			var token Token
			err := tx.First(&token, current.TokenId).Error
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			if err == nil {
				tokenKey = token.Key
				if err := tx.Model(&token).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota + ?", current.Quota), "used_quota": gorm.Expr("used_quota - ?", current.Quota)}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&Channel{}).Where("id = ?", current.ChannelId).Update("used_quota", gorm.Expr("used_quota - ?", old.RawQuota)).Error; err != nil {
			return err
		}
		current.Quota = 0
		current.BillingCharge = &common.BillingCharge{Policy: old.Policy, Refunded: true}
		if err := tx.Model(&current).Select("Quota", "BillingCharge").Updates(&current).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	task.Quota = current.Quota
	task.BillingCharge = current.BillingCharge
	if changed {
		if err := cacheIncrUserQuota(current.UserId, int64(previous.ChargedQuota)); err != nil {
			common.SysError("MJ refund quota cache: " + err.Error())
		}
		if common.RedisEnabled && tokenKey != "" {
			if err := cacheIncrTokenQuota(tokenKey, int64(previous.ChargedQuota)); err != nil {
				common.SysError("MJ refund token cache: " + err.Error())
			}
		}
	}
	return previous, changed, nil
}

// InsertChargedMidjourney atomically stores the accepted upstream task and its
// wallet/token debit. A failed token write or INSERT cannot leave a debit behind.
func InsertChargedMidjourney(task *Midjourney) error {
	if task.Id != 0 || task.BillingCharge == nil || task.Quota < 0 || task.Quota != task.BillingCharge.ChargedQuota {
		return fmt.Errorf("invalid Midjourney charge")
	}
	var tokenKey string
	err := DB.Transaction(func(tx *gorm.DB) error {
		if task.Quota > 0 {
			result := tx.Model(&User{}).Where("id = ?", task.UserId).Update("quota", gorm.Expr("quota - ?", task.Quota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("Midjourney user missing")
			}
		}
		if task.TokenId > 0 {
			var token Token
			if err := tx.First(&token, task.TokenId).Error; err != nil {
				return err
			}
			tokenKey = token.Key
			if err := tx.Model(&token).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota - ?", task.Quota), "used_quota": gorm.Expr("used_quota + ?", task.Quota), "accessed_time": common.GetTimestamp()}).Error; err != nil {
				return err
			}
		}
		return tx.Create(task).Error
	})
	if err != nil {
		return err
	}
	if err := cacheIncrUserQuota(task.UserId, -int64(task.Quota)); err != nil {
		common.SysError("MJ charge quota cache: " + err.Error())
	}
	if common.RedisEnabled && tokenKey != "" {
		if err := cacheIncrTokenQuota(tokenKey, -int64(task.Quota)); err != nil {
			common.SysError("MJ charge token cache: " + err.Error())
		}
	}
	return nil
}
