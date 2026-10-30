package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

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
