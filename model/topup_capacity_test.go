package model

import (
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 钱包容量的下单前检查：余额 + 仍可能被付款的待付订单 + 本次 ≤ 上限。
//
// 入账环节本来就拦着超限，但那时客户已经付了钱。这里的用例钉住的是「付款之前就拒绝」。

const capacityNow int64 = 1_800_000_000 // 用例里的「现在」（秒）

// yuan 把充值数量（下单口径的整数单位）换成额度。
func yuan(amount int64) int {
	return int(decimalQuotaForAmount(amount))
}

func capacityUser(t *testing.T, userID int, quota int) {
	t.Helper()
	tag := common.GetRandomString(8)
	require.NoError(t, DB.Create(&User{
		Id: userID, Username: "cap-user-" + tag, AffCode: "cap-aff-" + tag,
		Status: common.UserStatusEnabled, Quota: quota,
	}).Error)
}

// epayOrder 构造一张还没落库的易支付订单。
func epayOrder(userID int, tradeNo string, amount int64, createTime int64) *TopUp {
	return &TopUp{
		UserId: userID, Amount: amount, Money: float64(amount), TradeNo: tradeNo,
		PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CreateTime: createTime, Status: common.TopUpStatusPending,
	}
}

// seedOrder 直接落库一张已有订单（不经容量检查），用来摆出「之前下过的单」。
func seedOrder(t *testing.T, order *TopUp, status string) {
	t.Helper()
	order.Status = status
	require.NoError(t, DB.Create(order).Error)
}

// seedTronOrder 落库一张 TRON 待付订单：容量按它自己的有效期与 CreditQuota 计。
func seedTronOrder(t *testing.T, userID int, tradeNo string, creditQuota int, expiresAtMS int64) {
	t.Helper()
	createdMS := (capacityNow - 60) * 1000
	topUp := &TopUp{
		UserId: userID, Amount: 100, Money: 100, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron,
		CreateTime: createdMS / 1000, Status: common.TopUpStatusPending,
	}
	order := &TronTopupOrder{
		UserID: userID, TradeNo: tradeNo, ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract,
		ExpectedAmountMicros: 13_000_000 + int64(userID), RateCNYMicros: 7_250_000, QuoteUpdatedAtMS: createdMS,
		ExpiresAtMS: expiresAtMS, CreditQuota: creditQuota, CreatedAtMS: createdMS,
	}
	require.NoError(t, DB.Create(topUp).Error)
	order.TopUpID = topUp.Id
	require.NoError(t, DB.Create(order).Error)
}

func orderExists(t *testing.T, tradeNo string) bool {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", tradeNo).Count(&count).Error)
	return count == 1
}

func requireWalletFull(t *testing.T, err error) TopUpCapacity {
	t.Helper()
	require.ErrorIs(t, err, ErrTopUpWalletFull)
	var full *TopUpWalletFullError
	require.True(t, errors.As(err, &full))
	return full.Capacity
}

func TestTopUpCapacity_Fits(t *testing.T) {
	limit := int64(common.MaxQuota)
	cases := []struct {
		name     string
		capacity TopUpCapacity
		credit   int64
		fits     bool
	}{
		{"空钱包充到正好等于上限", TopUpCapacity{Limit: limit}, limit, true},
		{"空钱包充超过上限一个单位", TopUpCapacity{Limit: limit}, limit + 1, false},
		{"余额加本次正好等于上限", TopUpCapacity{Balance: limit - 5, Limit: limit}, 5, true},
		{"余额加本次超出一个单位", TopUpCapacity{Balance: limit - 5, Limit: limit}, 6, false},
		{"待付订单占掉的容量也算", TopUpCapacity{Balance: 10, Reserved: limit - 20, Limit: limit}, 11, false},
		{"待付订单占掉后正好够", TopUpCapacity{Balance: 10, Reserved: limit - 20, Limit: limit}, 10, true},
		{"欠费余额不放宽单笔上限", TopUpCapacity{Balance: -100, Limit: limit}, limit + 50, false},
		{"欠费余额时仍可充到单笔上限", TopUpCapacity{Balance: -100, Limit: limit}, limit, true},
		{"零额度不是充值", TopUpCapacity{Limit: limit}, 0, false},
		{"负额度不是充值", TopUpCapacity{Limit: limit}, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.fits, tc.capacity.Fits(tc.credit))
		})
	}
}

func TestTopUpCapacity_Room(t *testing.T) {
	limit := int64(common.MaxQuota)
	assert.Equal(t, limit, TopUpCapacity{Limit: limit}.Room())
	assert.Equal(t, int64(70), TopUpCapacity{Balance: limit - 100, Reserved: 30, Limit: limit}.Room())
	assert.Equal(t, int64(0), TopUpCapacity{Balance: limit, Reserved: 30, Limit: limit}.Room(), "不为负")
	assert.Equal(t, limit, TopUpCapacity{Balance: -500, Limit: limit}.Room(), "欠费也不超过单笔上限")
}

func TestInsertTopUpWithinWalletCapacity_InsertsWhenThereIsRoom(t *testing.T) {
	capacityUser(t, 93001, yuan(522))
	order := epayOrder(93001, "cap-room", 2000, capacityNow)

	require.NoError(t, InsertTopUpWithinWalletCapacity(order))
	assert.True(t, orderExists(t, "cap-room"))
	assert.NotZero(t, order.Id)
}

// 余额 2522 再充 2000 = 4522，超过上限（约 4294.97）：付款前就拒绝，不建单。
func TestInsertTopUpWithinWalletCapacity_RejectsWhenBalanceLeavesNoRoom(t *testing.T) {
	capacityUser(t, 93002, yuan(2522))
	order := epayOrder(93002, "cap-noroom", 2000, capacityNow)

	capacity := requireWalletFull(t, InsertTopUpWithinWalletCapacity(order))
	assert.False(t, orderExists(t, "cap-noroom"))
	assert.Equal(t, int64(yuan(2522)), capacity.Balance)
	assert.Zero(t, capacity.Reserved)
	assert.Equal(t, int64(common.MaxQuota), capacity.Limit)
}

// Codex 指出的并发情形的串行等价：余额 522，已有一张刚下的 2000 待付单，再下一张 2000
// 两张都付就是 4522。第二张必须在下单时被拒。
func TestInsertTopUpWithinWalletCapacity_CountsRecentPendingOrders(t *testing.T) {
	capacityUser(t, 93003, yuan(522))
	require.NoError(t, InsertTopUpWithinWalletCapacity(epayOrder(93003, "cap-first", 2000, capacityNow-30)))

	capacity := requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93003, "cap-second", 2000, capacityNow)))
	assert.False(t, orderExists(t, "cap-second"))
	assert.Equal(t, int64(yuan(2000)), capacity.Reserved)
	assert.Equal(t, 1, capacity.Pending)

	// 金额降到剩余容量以内就能下
	require.NoError(t, InsertTopUpWithinWalletCapacity(epayOrder(93003, "cap-smaller", 1000, capacityNow)))
}

// 超过预留窗口的待付单多半是放弃支付的：不再占容量，否则客户会被自己的弃单长期挡住。
func TestInsertTopUpWithinWalletCapacity_IgnoresPendingOrdersPastTheWindow(t *testing.T) {
	capacityUser(t, 93004, yuan(522))
	seedOrder(t, epayOrder(93004, "cap-stale", 2000, capacityNow-TopUpReservationWindowSeconds-1), common.TopUpStatusPending)

	require.NoError(t, InsertTopUpWithinWalletCapacity(epayOrder(93004, "cap-after-stale", 2000, capacityNow)))

	// 窗口边界：恰好在窗口内的仍然算
	capacityUser(t, 93005, yuan(522))
	seedOrder(t, epayOrder(93005, "cap-edge", 2000, capacityNow-TopUpReservationWindowSeconds+1), common.TopUpStatusPending)
	requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93005, "cap-after-edge", 2000, capacityNow)))
}

func TestInsertTopUpWithinWalletCapacity_IgnoresOtherUsersAndFinishedOrders(t *testing.T) {
	capacityUser(t, 93006, yuan(522))
	capacityUser(t, 93007, 0)
	seedOrder(t, epayOrder(93007, "cap-other-user", 2000, capacityNow-10), common.TopUpStatusPending)
	// 已入账的订单已经体现在余额里；失败、过期的不会再付款
	seedOrder(t, epayOrder(93006, "cap-done", 2000, capacityNow-10), common.TopUpStatusSuccess)
	seedOrder(t, epayOrder(93006, "cap-failed", 2000, capacityNow-10), common.TopUpStatusFailed)
	seedOrder(t, epayOrder(93006, "cap-expired", 2000, capacityNow-10), common.TopUpStatusExpired)

	require.NoError(t, InsertTopUpWithinWalletCapacity(epayOrder(93006, "cap-clean", 2000, capacityNow)))
}

// TRON 待付订单按它自己的有效期与 CreditQuota 占容量，不看易支付的窗口。
func TestInsertTopUpWithinWalletCapacity_CountsActiveTronOrderByItsOwnExpiry(t *testing.T) {
	capacityUser(t, 93008, yuan(522))
	seedTronOrder(t, 93008, "cap-tron-active", yuan(2000), (capacityNow+600)*1000)
	capacity := requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93008, "cap-vs-tron", 2000, capacityNow)))
	assert.Equal(t, int64(yuan(2000)), capacity.Reserved)
	assert.Equal(t, 1, capacity.Pending)

	capacityUser(t, 93009, yuan(522))
	seedTronOrder(t, 93009, "cap-tron-expired", yuan(2000), (capacityNow-1)*1000)
	require.NoError(t, InsertTopUpWithinWalletCapacity(epayOrder(93009, "cap-vs-expired-tron", 2000, capacityNow)))
}

// 单笔金额本身就超过钱包上限：余额是 0 也不行（以前这种单付了款只会卡在待处理）。
func TestInsertTopUpWithinWalletCapacity_RejectsSingleOrderLargerThanTheWallet(t *testing.T) {
	capacityUser(t, 93010, 0)
	requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93010, "cap-huge", 5000, capacityNow)))
	assert.False(t, orderExists(t, "cap-huge"))

	capacityUser(t, 93011, -yuan(100))
	requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93011, "cap-huge-debt", 4300, capacityNow)))
	require.NoError(t, InsertTopUpWithinWalletCapacity(epayOrder(93011, "cap-debt-ok", 4000, capacityNow)))
}

func TestInsertTopUpWithinWalletCapacity_UnknownUserIsAnErrorNotAnOrder(t *testing.T) {
	err := InsertTopUpWithinWalletCapacity(epayOrder(93999, "cap-ghost", 10, capacityNow))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTopUpWalletFull)
	assert.False(t, orderExists(t, "cap-ghost"))
}

func tronCreatePair(userID int, tradeNo string, creditQuota int) (*TopUp, *TronTopupOrder, int64) {
	nowMS := capacityNow * 1000
	topUp := &TopUp{
		UserId: userID, Amount: 2000, Money: 2000, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron,
		CreateTime: capacityNow, Status: common.TopUpStatusPending,
	}
	order := &TronTopupOrder{
		UserID: userID, TradeNo: tradeNo, ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract,
		ExpectedAmountMicros: 270_000_000 + int64(userID), RateCNYMicros: 7_250_000, QuoteUpdatedAtMS: nowMS,
		ExpiresAtMS: nowMS + 20*60*1000, CreditQuota: creditQuota, CreatedAtMS: nowMS,
	}
	return topUp, order, nowMS
}

func TestCreateOrGetActiveTronTopupOrder_RejectsWhenWalletHasNoRoom(t *testing.T) {
	capacityUser(t, 93020, yuan(2522))
	topUp, order, nowMS := tronCreatePair(93020, "cap-tron-noroom", yuan(2000))

	stored, created, err := CreateOrGetActiveTronTopupOrder(topUp, order, nowMS)
	capacity := requireWalletFull(t, err)
	assert.Nil(t, stored)
	assert.False(t, created)
	assert.False(t, orderExists(t, "cap-tron-noroom"))
	assert.Equal(t, int64(yuan(2522)), capacity.Balance)
	assert.Zero(t, topUp.Id, "失败后不留下半截主键")
}

func TestCreateOrGetActiveTronTopupOrder_CountsRecentEpayPendingOrders(t *testing.T) {
	capacityUser(t, 93021, yuan(522))
	seedOrder(t, epayOrder(93021, "cap-epay-before-tron", 2000, capacityNow-30), common.TopUpStatusPending)
	topUp, order, nowMS := tronCreatePair(93021, "cap-tron-after-epay", yuan(2000))

	_, _, err := CreateOrGetActiveTronTopupOrder(topUp, order, nowMS)
	capacity := requireWalletFull(t, err)
	assert.Equal(t, int64(yuan(2000)), capacity.Reserved)
}

func TestCreateOrGetActiveTronTopupOrder_CreatesWhenThereIsRoom(t *testing.T) {
	capacityUser(t, 93022, yuan(522))
	topUp, order, nowMS := tronCreatePair(93022, "cap-tron-room", yuan(2000))

	stored, created, err := CreateOrGetActiveTronTopupOrder(topUp, order, nowMS)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "cap-tron-room", stored.TradeNo)
}

// 已有一张有效的 TRON 订单时原样返回它：没有新增占用，不该因为容量被拒。
func TestCreateOrGetActiveTronTopupOrder_ReturnsActiveOrderEvenWhenWalletIsFull(t *testing.T) {
	capacityUser(t, 93023, yuan(522))
	first, firstOrder, nowMS := tronCreatePair(93023, "cap-tron-active-1", yuan(2000))
	_, created, err := CreateOrGetActiveTronTopupOrder(first, firstOrder, nowMS)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 93023).Update("quota", common.MaxQuota).Error)

	second, secondOrder, _ := tronCreatePair(93023, "cap-tron-active-2", yuan(2000))
	secondOrder.ExpectedAmountMicros++
	stored, created, err := CreateOrGetActiveTronTopupOrder(second, secondOrder, nowMS)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, "cap-tron-active-1", stored.TradeNo)
}

// 管理员补单共用同一个上限：不能把余额补到上限以外（之后任何一笔充值都会卡住）。
func TestManualCompleteTopUp_RefusesToPushWalletPastTheLimit(t *testing.T) {
	capacityUser(t, 93030, yuan(2522))
	seedOrder(t, epayOrder(93030, "cap-manual-over", 2000, capacityNow), common.TopUpStatusPending)

	err := ManualCompleteTopUp("cap-manual-over", "127.0.0.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "上限")
	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, "cap-manual-over"))
	assert.Equal(t, yuan(2522), userQuota(t, 93030))
}

func TestManualCompleteTopUp_StillCompletesWithinTheLimit(t *testing.T) {
	capacityUser(t, 93031, yuan(522))
	seedOrder(t, epayOrder(93031, "cap-manual-ok", 2000, capacityNow), common.TopUpStatusPending)

	require.NoError(t, ManualCompleteTopUp("cap-manual-ok", "127.0.0.1"))
	assert.Equal(t, common.TopUpStatusSuccess, topUpStatus(t, "cap-manual-ok"))
	assert.Equal(t, yuan(2522), userQuota(t, 93031))
}

func TestManualCompleteTopUp_RefusesSingleOrderLargerThanTheWallet(t *testing.T) {
	capacityUser(t, 93032, 0)
	seedOrder(t, epayOrder(93032, "cap-manual-huge", 5000, capacityNow), common.TopUpStatusPending)

	require.Error(t, ManualCompleteTopUp("cap-manual-huge", "127.0.0.1"))
	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, "cap-manual-huge"))
	assert.Zero(t, userQuota(t, 93032))
}

// 占用额度要与各渠道结算时实际入账的额度同一口径，否则会把钱包误判成满的（或没满）。
func TestTopUpCreditQuota_FollowsEachProvidersSettlementRule(t *testing.T) {
	unit := int64(common.QuotaPerUnit)
	assert.Equal(t, 10*unit, topUpCreditQuota(&TopUp{PaymentProvider: PaymentProviderEpay, Amount: 10, Money: 73}))
	assert.Equal(t, 10*unit, topUpCreditQuota(&TopUp{PaymentProvider: PaymentProviderWaffo, Amount: 10, Money: 73}))
	// Stripe 按 Money（经分组倍率换算后的数量）入账，不看 Amount
	assert.Equal(t, 12*unit+unit/2, topUpCreditQuota(&TopUp{PaymentProvider: PaymentProviderStripe, Amount: 10, Money: 12.5}))
	// Creem 的 Amount 本身就是额度，不能再乘一遍
	assert.Equal(t, int64(300_000), topUpCreditQuota(&TopUp{PaymentProvider: PaymentProviderCreem, Amount: 300_000}))
}

// 先比较完整的十进制数再转整数：超大数量乘出来超过 int64 时，转换会绕回成一个看似正常的值。
func TestTopUpCreditQuota_SaturatesInsteadOfOverflowing(t *testing.T) {
	limit := int64(common.MaxQuota)
	absurd := []*TopUp{
		{PaymentProvider: PaymentProviderEpay, Amount: 1_000_000_000_000_000}, // × 500000 = 5e20，超过 int64
		{PaymentProvider: PaymentProviderEpay, Amount: 36_893_488_147_420},    // × 500000 ≈ 2^64 + 小量，绕回后是个小正数
		{PaymentProvider: PaymentProviderStripe, Money: 1e300},
		{PaymentProvider: PaymentProviderStripe, Money: math.Inf(1)},
		{PaymentProvider: PaymentProviderStripe, Money: math.NaN()},
		{PaymentProvider: PaymentProviderCreem, Amount: math.MaxInt64},
	}
	for _, order := range absurd {
		credit := topUpCreditQuota(order)
		assert.Equal(t, limit+1, credit, "超过上限的一律按「上限 + 1」算：%+v", order)
		assert.False(t, TopUpCapacity{Limit: limit}.Fits(credit))
	}
	// 非正数不是充值：不占容量，也不可能通过 Fits
	for _, order := range []*TopUp{
		{PaymentProvider: PaymentProviderEpay, Amount: 0},
		{PaymentProvider: PaymentProviderEpay, Amount: -5},
		{PaymentProvider: PaymentProviderEpay, Amount: math.MinInt64},
		{PaymentProvider: PaymentProviderStripe, Money: -1e300},
		{PaymentProvider: PaymentProviderStripe, Money: math.Inf(-1)},
	} {
		assert.Zero(t, topUpCreditQuota(order), "%+v", order)
	}
}

func TestInsertTopUpWithinWalletCapacity_RejectsAbsurdAmountWithoutOverflow(t *testing.T) {
	capacityUser(t, 93040, 0)
	requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93040, "cap-absurd", 36_893_488_147_420, capacityNow)))
	assert.False(t, orderExists(t, "cap-absurd"))
}

// 库里已有一张数量离谱的待付单（历史脏数据）：按「占满」保守处理，而不是让它绕回成一个小数。
func TestInsertTopUpWithinWalletCapacity_TreatsAbnormalPendingOrderAsFillingTheWallet(t *testing.T) {
	capacityUser(t, 93041, 0)
	seedOrder(t, epayOrder(93041, "cap-dirty", 36_893_488_147_420, capacityNow-10), common.TopUpStatusPending)
	requireWalletFull(t, InsertTopUpWithinWalletCapacity(epayOrder(93041, "cap-after-dirty", 10, capacityNow)))
}

func TestManualCompleteTopUp_RefusesAbsurdAmountWithoutOverflow(t *testing.T) {
	capacityUser(t, 93042, 0)
	seedOrder(t, epayOrder(93042, "cap-manual-absurd", 36_893_488_147_420, capacityNow), common.TopUpStatusPending)

	require.Error(t, ManualCompleteTopUp("cap-manual-absurd", "127.0.0.1"))
	assert.Equal(t, common.TopUpStatusPending, topUpStatus(t, "cap-manual-absurd"))
	assert.Zero(t, userQuota(t, 93042))
}

// 补单与回调入账用同一个口径：Creem 订单的 Amount 已经是额度。
func TestManualCompleteTopUp_CreditsCreemOrderByItsAmount(t *testing.T) {
	capacityUser(t, 93043, 0)
	order := epayOrder(93043, "cap-manual-creem", 300_000, capacityNow)
	order.PaymentProvider, order.PaymentMethod = PaymentProviderCreem, PaymentMethodCreem
	seedOrder(t, order, common.TopUpStatusPending)

	require.NoError(t, ManualCompleteTopUp("cap-manual-creem", "127.0.0.1"))
	assert.Equal(t, 300_000, userQuota(t, 93043))
}
