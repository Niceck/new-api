package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 额度更新打在不存在的行上时，必须报错而不是静默成功。
//
// 为什么重要：调用方拿到 nil 就认为额度已落库。若用户/令牌已被删除，
// UPDATE 影响 0 行，退款与入账会静默丢失，且没有任何日志留痕。
// 与 quota <= 0 之类的参数校验不同，这类失败只能靠 RowsAffected 发现。

func TestIncreaseUserQuota_FailsWhenUserMissing(t *testing.T) {
	err := IncreaseUserQuota(920001, 100, true)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDecreaseUserQuota_FailsWhenUserMissing(t *testing.T) {
	err := DecreaseUserQuota(920002, 100, true)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestIncreaseTokenQuota_FailsWhenTokenMissing(t *testing.T) {
	err := IncreaseTokenQuota(920003, "missing-token-key-inc", 100)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDecreaseTokenQuota_FailsWhenTokenMissing(t *testing.T) {
	err := DecreaseTokenQuota(920004, "missing-token-key-dec", 100)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// 存在的行照常成功，且欠费语义保持不变：余额不足也照扣，允许扣成负数。
// 这是本站刻意的设计（保「日志与余额变动一致」），不能被 RowsAffected 改动波及。
func TestDecreaseUserQuota_StillAllowsNegativeBalance(t *testing.T) {
	require.NoError(t, DB.Create(&User{
		Id:       920005,
		Username: "rows-affected-negative",
		AffCode:  "rows-aff-negative",
		Status:   common.UserStatusEnabled,
		Quota:    50,
	}).Error)

	require.NoError(t, DecreaseUserQuota(920005, 200, true), "余额不足不得报错，欠费是允许的")

	user := &User{}
	require.NoError(t, DB.Select("id", "quota").Where("id = ?", 920005).First(user).Error)
	assert.Equal(t, -150, user.Quota, "余额必须被扣成负数，保住对账一致")
}

func TestIncreaseUserQuota_SucceedsForExistingUser(t *testing.T) {
	require.NoError(t, DB.Create(&User{
		Id:       920006,
		Username: "rows-affected-increase",
		AffCode:  "rows-aff-increase",
		Status:   common.UserStatusEnabled,
		Quota:    10,
	}).Error)

	require.NoError(t, IncreaseUserQuota(920006, 90, true))

	user := &User{}
	require.NoError(t, DB.Select("id", "quota").Where("id = ?", 920006).First(user).Error)
	assert.Equal(t, 100, user.Quota)
}

func TestTokenQuotaUpdates_SucceedForExistingToken(t *testing.T) {
	require.NoError(t, DB.Create(&Token{
		Id:          920007,
		UserId:      920006,
		Key:         "rows-affected-token-key",
		Name:        "rows-affected-token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 500,
		UsedQuota:   0,
	}).Error)

	require.NoError(t, DecreaseTokenQuota(920007, "rows-affected-token-key", 200))
	require.NoError(t, IncreaseTokenQuota(920007, "rows-affected-token-key", 50))

	token := &Token{}
	require.NoError(t, DB.Select("id", "remain_quota", "used_quota").Where("id = ?", 920007).First(token).Error)
	assert.Equal(t, 350, token.RemainQuota)
	assert.Equal(t, 150, token.UsedQuota)
}
