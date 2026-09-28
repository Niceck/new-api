package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// epayFixture 建一个 pending 的易支付订单与对应用户。
// amount 是美元数量（易支付口径：Amount * QuotaPerUnit = 应充额度）。
func epayFixture(t *testing.T, tradeNo string, userId int, amount int64, startQuota int) *TopUp {
	t.Helper()
	// aff_code 有唯一约束，空串会在第二个用户上撞车，必须逐个给值。
	require.NoError(t, DB.Create(&User{
		Id:       userId,
		Username: tradeNo + "-user",
		AffCode:  "epay-aff-" + tradeNo,
		Status:   common.UserStatusEnabled,
		Quota:    startQuota,
	}).Error)
	topUp := &TopUp{
		UserId:          userId,
		Amount:          amount,
		Money:           float64(amount),
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())
	return topUp
}

func userQuota(t *testing.T, userId int) int {
	t.Helper()
	user := &User{}
	require.NoError(t, DB.Select("id", "quota").Where("id = ?", userId).First(user).Error)
	return user.Quota
}

func topUpStatus(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := &TopUp{}
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(topUp).Error)
	return topUp.Status
}

// 正常落账：订单转 success 且额度按 Amount * QuotaPerUnit 增加，两者同事务。
func TestRechargeEpay_CreditsOrderAndQuotaTogether(t *testing.T) {
	topUp := epayFixture(t, "epay-ok", 91001, 10, 500)
	expected := int(decimalQuotaForAmount(10))

	quotaAdded, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)

	assert.Equal(t, expected, quotaAdded)
	assert.Equal(t, common.TopUpStatusSuccess, topUpStatus(t, topUp.TradeNo))
	assert.Equal(t, 500+expected, userQuota(t, 91001))
}

// 幂等：平台重投回调只入账一次。
func TestRechargeEpay_IsIdempotentOnReplay(t *testing.T) {
	topUp := epayFixture(t, "epay-replay", 91002, 10, 0)
	expected := int(decimalQuotaForAmount(10))

	first, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, expected, first)

	second, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err, "重投必须幂等成功，否则 webhook 会被平台反复重试")
	assert.Equal(t, 0, second, "重投不得再次入账")

	assert.Equal(t, expected, userQuota(t, 91002), "额度只能增加一次")
}

// 并发回调：同一订单并发到达，只有一个入账。
func TestRechargeEpay_ConcurrentCallbacksCreditOnce(t *testing.T) {
	topUp := epayFixture(t, "epay-concurrent", 91003, 10, 0)
	expected := int(decimalQuotaForAmount(10))

	const callers = 8
	var wg sync.WaitGroup
	credited := make([]int, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(idx int) {
			defer wg.Done()
			credited[idx], errs[idx] = RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
		}(i)
	}
	wg.Wait()

	total := 0
	for i := 0; i < callers; i++ {
		require.NoError(t, errs[i])
		total += credited[i]
	}
	assert.Equal(t, expected, total, "并发回调合计只能入账一次")
	assert.Equal(t, expected, userQuota(t, 91003))
}

// provider 隔离：TRON 订单不能走易支付结算路径。
func TestRechargeEpay_RejectsForeignProvider(t *testing.T) {
	topUp := epayFixture(t, "epay-foreign", 91004, 10, 0)
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", topUp.TradeNo).
		Update("payment_provider", PaymentProviderTron).Error)

	_, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, topUp.TradeNo))
	assert.Equal(t, 0, userQuota(t, 91004))
}

// 订单不存在。
func TestRechargeEpay_RejectsUnknownTradeNo(t *testing.T) {
	_, err := RechargeEpay("epay-missing", "alipay", "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpNotFound)
}

// 已关闭/过期订单不得入账。
func TestRechargeEpay_RejectsNonPendingOrder(t *testing.T) {
	topUp := epayFixture(t, "epay-closed", 91005, 10, 0)
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", topUp.TradeNo).
		Update("status", common.TopUpStatusExpired).Error)

	_, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)

	assert.Equal(t, 0, userQuota(t, 91005))
}

// 金额非法（Amount<=0 换算出 0 额度）必须拒绝，不能静默落一个 0 元成功单。
func TestRechargeEpay_RejectsNonPositiveQuota(t *testing.T) {
	topUp := epayFixture(t, "epay-zero", 91006, 0, 0)

	_, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.Error(t, err)

	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, topUp.TradeNo))
	assert.Equal(t, 0, userQuota(t, 91006))
}

// 钱包上限保护：入账不得把额度冲过 MaxQuota（与 TRON 路径同口径）。
func TestRechargeEpay_EnforcesQuotaUpperBound(t *testing.T) {
	topUp := epayFixture(t, "epay-overflow", 91007, 10, common.MaxQuota-1)

	_, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
	require.Error(t, err)

	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, topUp.TradeNo))
	assert.Equal(t, common.MaxQuota-1, userQuota(t, 91007), "越界时不得部分入账")
}

// 实际支付方式与下单不同时，落库要记实际值（回调携带的 method）。
func TestRechargeEpay_PersistsActualPaymentMethod(t *testing.T) {
	topUp := epayFixture(t, "epay-method", 91008, 10, 0)

	_, err := RechargeEpay(topUp.TradeNo, "wxpay", "127.0.0.1")
	require.NoError(t, err)

	stored := &TopUp{}
	require.NoError(t, DB.Where("trade_no = ?", topUp.TradeNo).First(stored).Error)
	assert.Equal(t, "wxpay", stored.PaymentMethod)
	assert.NotZero(t, stored.CompleteTime, "成功单必须有完成时间")
}
