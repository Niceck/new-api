package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 累计充值等级的「累计」只认 top_ups 里这个用户的成功订单的到账面值：
// 待付、已过期的订单不算，别人的订单不算，兑换码与后台加额度不在这张表里也就不算。

func TestGetUserTopUpFaceTotal_SumsOnlyThisUsersSuccessfulOrders(t *testing.T) {
	const userID, otherID = 9101, 9102
	rows := []struct {
		user   int
		amount int64
		money  float64
		status string
	}{
		{userID, 200, 196, common.TopUpStatusSuccess},
		{userID, 2000, 1840, common.TopUpStatusSuccess},
		{userID, 500, 485, common.TopUpStatusPending}, // 还没付：不算
		{userID, 100, 100, common.TopUpStatusExpired}, // 过期：不算
		{otherID, 5000, 4600, common.TopUpStatusSuccess},
	}
	balanceRow := &TopUp{ // 用余额买订阅：不是钱进来，不算
		UserId: userID, Amount: 300, Money: 300, Status: common.TopUpStatusSuccess,
		TradeNo: "tier-balance", PaymentMethod: PaymentMethodBalance, PaymentProvider: PaymentProviderBalance,
		CreateTime: 1_800_000_100,
	}
	require.NoError(t, DB.Create(balanceRow).Error)
	for i, row := range rows {
		require.NoError(t, DB.Create(&TopUp{
			UserId: row.user, Amount: row.amount, Money: row.money, Status: row.status,
			TradeNo: "tier-" + string(rune('a'+i)), PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
			CreateTime: 1_800_000_000 + int64(i),
		}).Error)
	}
	t.Cleanup(func() { DB.Where("trade_no LIKE ?", "tier-%").Delete(&TopUp{}) })

	total, err := GetUserTopUpFaceTotal(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(2200), total, "按到账面值累计，不按实付")

	total, err = GetUserTopUpFaceTotal(9999)
	require.NoError(t, err)
	assert.Zero(t, total, "没充过值的用户是 0，不是报错")
}

func TestUpdateOption_RejectsBrokenTierDiscountBeforeTouchingTheDatabase(t *testing.T) {
	err := UpdateOption(operation_setting.TierDiscountOptionKey, `{"1000":0.975}`)
	require.Error(t, err)
	assert.ErrorIs(t, err, operation_setting.ErrTierDiscountInvalid)
	assert.Error(t, UpdateOption(operation_setting.TierDiscountOptionKey, `null`))
	assert.Error(t, UpdateOptionsBulk(map[string]string{operation_setting.TierDiscountOptionKey: `{"1000":1.2}`}))
}

func TestValidateOptionValue_RefusesTokensDisplayWhileTiersExist(t *testing.T) {
	ps := operation_setting.GetPaymentSetting()
	original := ps.TierDiscount
	t.Cleanup(func() { ps.TierDiscount = original })

	ps.TierDiscount = map[int]float64{1000: 0.98}
	assert.Error(t, validateOptionValue(quotaDisplayTypeOptionKey, operation_setting.QuotaDisplayTypeTokens))
	assert.NoError(t, validateOptionValue(quotaDisplayTypeOptionKey, operation_setting.QuotaDisplayTypeCNY))

	ps.TierDiscount = map[int]float64{}
	assert.NoError(t, validateOptionValue(quotaDisplayTypeOptionKey, operation_setting.QuotaDisplayTypeTokens), "没有等级时随便切")
}
