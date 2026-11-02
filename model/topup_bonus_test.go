package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 充值赠送的入账：只认建单时的快照 BonusQuota。金额比对仍只看实付，重投不重复赠送，累计不算赠送。

func bonusEpayOrder(t *testing.T, tradeNo string, userId int, amount int64, bonus int64, startQuota int) *TopUp {
	t.Helper()
	topUp := epayFixture(t, tradeNo, userId, amount, startQuota)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("bonus_quota", bonus).Error)
	topUp.BonusQuota = bonus
	return topUp
}

func TestRechargeEpay_CreditsFacePlusBonusSnapshot(t *testing.T) {
	topUp := bonusEpayOrder(t, "bonus-ok", 92001, 500, 8_750_000, 100)
	face := int(decimalQuotaForAmount(500))

	added, err := RechargeEpay(topUp.TradeNo, "alipay", "500.00", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, face+8_750_000, added)
	assert.Equal(t, 100+face+8_750_000, userQuota(t, 92001))
	assert.Equal(t, common.TopUpStatusSuccess, topUpStatus(t, topUp.TradeNo))

	// 重投：不重复入账，赠送也不重复
	added, err = RechargeEpay(topUp.TradeNo, "alipay", "500.00", "127.0.0.1")
	require.NoError(t, err)
	assert.Zero(t, added)
	assert.Equal(t, 100+face+8_750_000, userQuota(t, 92001))
}

func TestRechargeEpay_OldDiscountOrderStillCreditsFaceOnly(t *testing.T) {
	// 切换前下的折扣单：实付 485、面值 500、没有赠送。切换后付款仍按下单时的承诺结算。
	topUp := epayFixture(t, "bonus-old-discount", 92002, 500, 0)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("money", 485).Error)

	_, err := RechargeEpay(topUp.TradeNo, "alipay", "500.00", "127.0.0.1")
	assert.ErrorIs(t, err, ErrTopUpAmountMismatch, "按新价回调不能入账")

	added, err := RechargeEpay(topUp.TradeNo, "alipay", "485.00", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, int(decimalQuotaForAmount(500)), added)
}

func TestRechargeEpay_BonusDoesNotLoosenTheAmountCheck(t *testing.T) {
	topUp := bonusEpayOrder(t, "bonus-mismatch", 92003, 500, 8_750_000, 0)
	_, err := RechargeEpay(topUp.TradeNo, "alipay", "485.00", "127.0.0.1")
	assert.ErrorIs(t, err, ErrTopUpAmountMismatch)
	assert.Zero(t, userQuota(t, 92003))
	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, topUp.TradeNo))
}

func TestRechargeEpay_WalletLimitCountsBonus(t *testing.T) {
	// 面值 2000 装得下、加赠送装不下：整单回滚，不部分入账
	start := common.MaxQuota - int(decimalQuotaForAmount(2000))
	topUp := bonusEpayOrder(t, "bonus-over-limit", 92004, 2000, 90_000_000, start)
	_, err := RechargeEpay(topUp.TradeNo, "alipay", "2000.00", "127.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, start, userQuota(t, 92004))
	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, topUp.TradeNo))
}

func TestManualCompleteTopUp_CreditsBonusSnapshot(t *testing.T) {
	topUp := bonusEpayOrder(t, "bonus-manual", 92005, 1000, 27_500_000, 0)
	require.NoError(t, ManualCompleteTopUp(topUp.TradeNo, "127.0.0.1"))
	assert.Equal(t, int(decimalQuotaForAmount(1000))+27_500_000, userQuota(t, 92005))
}

func TestTopUpCreditQuota_IncludesBonusOnlyForAmountBasedProviders(t *testing.T) {
	face := decimalQuotaForAmount(100)
	assert.Equal(t, face+1_000, topUpCreditQuota(&TopUp{Amount: 100, BonusQuota: 1_000, PaymentProvider: PaymentProviderEpay}))
	assert.Equal(t, face, topUpCreditQuota(&TopUp{Amount: 100, PaymentProvider: PaymentProviderEpay}), "老订单没有赠送")
	assert.Zero(t, topUpCreditQuota(&TopUp{Amount: 100, BonusQuota: -1, PaymentProvider: PaymentProviderEpay}), "负的赠送不合法")
	assert.Equal(t, int64(common.MaxQuota)+1, topUpCreditQuota(&TopUp{Amount: 1, BonusQuota: int64(common.MaxQuota), PaymentProvider: PaymentProviderEpay}), "加起来超限要被拦")
}

func TestInsertTopUpWithinWalletCapacity_ReservesBonusOfPendingOrders(t *testing.T) {
	capacityUser(t, 92006, 0)
	now := common.GetTimestamp()
	first := epayOrder(92006, "bonus-pending-1", 2000, now)
	first.BonusQuota = 90_000_000
	require.NoError(t, InsertTopUpWithinWalletCapacity(first))

	// 第一张待付单连赠送占 2180。再来 2200：只算面值 4200 装得下，连赠送 4380 超过 4294.97，必须被拒
	second := epayOrder(92006, "bonus-pending-2", 2200, now)
	capacity := requireWalletFull(t, InsertTopUpWithinWalletCapacity(second))
	assert.Equal(t, int64(yuan(2000))+90_000_000, capacity.Reserved, "待付单连赠送一起占容量")
	assert.False(t, orderExists(t, "bonus-pending-2"))
}

func TestGetUserTopUpFaceTotal_DoesNotCountBonus(t *testing.T) {
	topUp := bonusEpayOrder(t, "bonus-total", 92007, 500, 8_750_000, 0)
	_, err := RechargeEpay(topUp.TradeNo, "alipay", "500.00", "127.0.0.1")
	require.NoError(t, err)
	total, err := GetUserTopUpFaceTotal(92007)
	require.NoError(t, err)
	assert.Equal(t, int64(500), total)
}

func TestTronOrderValidation_RejectsAbsurdBonus(t *testing.T) {
	topUp, order, _ := tronCreatePair(92008, "bonus-tron-bad", 1_000)
	topUp.BonusQuota = 1_000
	assert.Error(t, validateNewTronTopupOrder(topUp, order), "赠送不能吃掉全部到账额度")
	topUp.BonusQuota = -1
	assert.Error(t, validateNewTronTopupOrder(topUp, order))
	topUp.BonusQuota = 999
	assert.NoError(t, validateNewTronTopupOrder(topUp, order))
}

func TestTronSettlementQuota_ScalesBonusWithPartialPayment(t *testing.T) {
	// 面值 ¥100 + 赠送 ¥3.5：付 95% 到账 95% 的总额；多付不多送
	const credit = 51_750_000
	quota, err := tronSettlementQuota(100_000_000, 95_000_000, credit)
	require.NoError(t, err)
	assert.Equal(t, 49_162_500, quota)
	quota, err = tronSettlementQuota(100_000_000, 105_000_000, credit)
	require.NoError(t, err)
	assert.Equal(t, credit, quota)
}
