package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	tolBaseMS    int64 = 1_800_000_000_000
	tolTTLMS     int64 = 20 * 60 * 1000
	tolFaceQuota       = 50_000_000
	// 2026-10-13 那笔真实到账：应付 14.952525，实到 15.018578，订单创建后 113 秒打包。
	tolExpectedMicros int64 = 14_952_525
	tolPaidMicros     int64 = 15_018_578
)

func tolUser(t *testing.T, userID int, quota int) {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Count(&count).Error)
	if count > 0 {
		return
	}
	name := fmt.Sprintf("tol-user-%d", userID)
	require.NoError(t, DB.Create(&User{Id: userID, Username: name, AffCode: "aff-" + name, Status: common.UserStatusEnabled, Quota: quota}).Error)
}

func tolOrder(t *testing.T, tradeNo string, userID int, expectedMicros, createdMS int64, creditQuota int) *TronTopupOrder {
	t.Helper()
	tolUser(t, userID, 0)
	topUp := &TopUp{UserId: userID, Amount: 100, Money: 100, TradeNo: tradeNo, PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, CreateTime: createdMS / 1000, Status: common.TopUpStatusPending}
	order := &TronTopupOrder{UserID: userID, TradeNo: tradeNo, ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: expectedMicros, RateCNYMicros: 6_687_834, QuoteUpdatedAtMS: createdMS, ExpiresAtMS: createdMS + tolTTLMS, CreditQuota: creditQuota, CreatedAtMS: createdMS}
	require.NoError(t, CreateTronTopupOrder(topUp, order))
	return order
}

func tolTransfer(seq int, amountMicros, blockTimeMS int64) TronTransferRecord {
	return TronTransferRecord{TxID: fmt.Sprintf("%064x", seq), BlockTimestampMS: blockTimeMS, FromAddress: "TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh", ToAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, AmountMicros: amountMicros, ObservedAtMS: blockTimeMS + 200_000}
}

func tolQuota(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func tolTopUpStatus(t *testing.T, order *TronTopupOrder) string {
	t.Helper()
	var topUp TopUp
	require.NoError(t, DB.Where("id = ?", order.TopUpID).First(&topUp).Error)
	return topUp.Status
}

func tolDeposit(t *testing.T, transfer TronTransferRecord) TronDeposit {
	t.Helper()
	var deposit TronDeposit
	require.NoError(t, DB.Where("tx_id = ?", transfer.TxID).First(&deposit).Error)
	return deposit
}

func requireTolUnmatched(t *testing.T, transfer TronTransferRecord, peers []TronTransferRecord, note string) {
	t.Helper()
	result, err := SettleTronDepositTolerant(transfer, peers, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Unmatched, "应落未匹配")
	assert.False(t, result.Credited)
	assert.Equal(t, note, result.MatchNote)
	deposit := tolDeposit(t, transfer)
	assert.Equal(t, TronDepositStatusUnmatched, deposit.Status)
	assert.Nil(t, deposit.OrderID)
	assert.Zero(t, deposit.CreditedQuota)
	assert.Equal(t, note, deposit.MatchNote)
}

func TestTronSettlementQuota_CapsOverpayAndProratesUnderpay(t *testing.T) {
	cases := []struct {
		name     string
		expected int64
		actual   int64
		credit   int
		want     int
		wantErr  bool
	}{
		{name: "真实事件多付 0.44% 给面值", expected: tolExpectedMicros, actual: tolPaidMicros, credit: tolFaceQuota, want: tolFaceQuota},
		{name: "恰好相等给面值", expected: 10_000_000, actual: 10_000_000, credit: 10_000_000, want: 10_000_000},
		{name: "110% 边界仍给面值", expected: 10_000_000, actual: 11_000_000, credit: 10_000_000, want: 10_000_000},
		{name: "超过 110% 一个微单位拒绝", expected: 10_000_000, actual: 11_000_001, credit: 10_000_000, wantErr: true},
		{name: "90% 边界按比例", expected: 10_000_000, actual: 9_000_000, credit: 10_000_000, want: 9_000_000},
		{name: "低于 90% 一个微单位拒绝", expected: 10_000_000, actual: 8_999_999, credit: 10_000_000, wantErr: true},
		{name: "少付向下取整", expected: 10_000_003, actual: 9_500_000, credit: 10_000_000, want: 9_499_997},
		{name: "交易所扣 1 USDT 手续费", expected: tolExpectedMicros, actual: 13_952_525, credit: tolFaceQuota, want: 46_656_083},
		{name: "大数不溢出", expected: 2_000_000_000_000, actual: 1_900_000_000_000, credit: common.MaxQuota, want: 2_040_109_464},
		{name: "应付非正", expected: 0, actual: 1, credit: 1, wantErr: true},
		{name: "实到非正", expected: 1, actual: 0, credit: 1, wantErr: true},
		{name: "额度非正", expected: 1, actual: 1, credit: 0, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tronSettlementQuota(tc.expected, tc.actual, tc.credit)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTolerantSettle_CreditsFaceValueForOverpayExactlyOnce(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "TOL-over", 9001, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(1, tolPaidMicros, tolBaseMS+113_000)

	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.False(t, result.AlreadyProcessed)
	assert.Equal(t, order.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9001), "多付不多给：只入账订单面值")
	assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, order))

	deposit := tolDeposit(t, transfer)
	assert.Equal(t, TronDepositStatusCredited, deposit.Status)
	assert.Equal(t, tolFaceQuota, deposit.CreditedQuota)
	assert.Equal(t, tolPaidMicros, deposit.AmountMicros, "实到金额原样保留")
	require.NotNil(t, deposit.OrderID)
	assert.Equal(t, order.ID, *deposit.OrderID)
	assert.Equal(t, TronMatchModeTolerant, deposit.MatchMode)
	assert.Equal(t, tronTolerantRuleVersion, deposit.MatchRule)

	replay, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, replay.AlreadyProcessed)
	assert.True(t, replay.Credited)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9001), "重扫不得再次入账")
	var logs int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("user_id = ? AND type = ?", 9001, LogTypeTopup).Count(&logs).Error)
	assert.Equal(t, int64(1), logs)
}

func TestTolerantSettle_ProratesUnderpayAndSettlesOrder(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "TOL-under", 9002, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(2, 13_952_525, tolBaseMS+300_000)

	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, 46_656_083, tolQuota(t, 9002), "少付按实到 ÷ 应付向下取整")
	assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, order), "少付即结清")
	assert.Equal(t, 46_656_083, tolDeposit(t, transfer).CreditedQuota)

	var stored TronTopupOrder
	require.NoError(t, DB.Where("id = ?", order.ID).First(&stored).Error)
	assert.Equal(t, tolFaceQuota, stored.CreditQuota, "订单面值与应付保持原值")
	assert.Equal(t, tolExpectedMicros, stored.ExpectedAmountMicros)
}

func TestTolerantSettle_AmountBandBoundaries(t *testing.T) {
	truncateTables(t)
	// 应付 10 USDT：90% = 9.000000，110% = 11.000000，边界包含。
	low := tolOrder(t, "TOL-band-low", 9010, 10_000_000, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(10, 9_000_000, tolBaseMS+120_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, low.ID, result.OrderID)
	assert.Equal(t, 45_000_000, tolQuota(t, 9010))

	truncateTablesNow(t)
	tolOrder(t, "TOL-band-below", 9011, 10_000_000, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(11, 8_999_999, tolBaseMS+120_000), nil, tronTolerantNoteNoCandidate)
	assert.Zero(t, tolQuota(t, 9011))

	truncateTablesNow(t)
	high := tolOrder(t, "TOL-band-high", 9012, 10_000_000, tolBaseMS, tolFaceQuota)
	result, err = SettleTronDepositTolerant(tolTransfer(12, 11_000_000, tolBaseMS+120_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, high.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9012))

	truncateTablesNow(t)
	tolOrder(t, "TOL-band-above", 9013, 10_000_000, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(13, 11_000_001, tolBaseMS+120_000), nil, tronTolerantNoteNoCandidate)
	assert.Zero(t, tolQuota(t, 9013))
}

func TestTolerantSettle_RequiresOrderToPredatePaymentBySixtySeconds(t *testing.T) {
	truncateTables(t)
	tolOrder(t, "TOL-early", 9020, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(20, tolPaidMicros, tolBaseMS+59_999), nil, tronTolerantNoteTooEarly)
	assert.Zero(t, tolQuota(t, 9020))

	truncateTablesNow(t)
	tolOrder(t, "TOL-early-ok", 9021, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(21, tolPaidMicros, tolBaseMS+60_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
}

func TestTolerantSettle_RequiresPaymentBeforeExpiry(t *testing.T) {
	truncateTables(t)
	tolOrder(t, "TOL-late", 9030, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(30, tolPaidMicros, tolBaseMS+tolTTLMS+1), nil, tronTolerantNoteNoCandidate)

	truncateTablesNow(t)
	tolOrder(t, "TOL-late-ok", 9031, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(31, tolPaidMicros, tolBaseMS+tolTTLMS), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
}

func TestTolerantSettle_OtherUsersOrderBlocksWithinTwoHoursAfterExpiry(t *testing.T) {
	truncateTables(t)
	blockTime := tolBaseMS + 10*60*60*1000
	// 另一个用户的同档弃单：过期后恰好 2 小时仍算「这笔钱也可能是他的」。
	tolOrder(t, "TOL-other-edge", 9040, 14_951_184, blockTime-tolTTLMS-tronTolerantBlockerGraceMS, tolFaceQuota)
	tolOrder(t, "TOL-mine-blocked", 9041, tolExpectedMicros, blockTime-113_000, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(40, tolPaidMicros, blockTime), nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9041))
	assert.Zero(t, tolQuota(t, 9040))

	truncateTablesNow(t)
	// 过期超过 2 小时 1 毫秒：不再阻断（这是站长接受的窗口外风险）。
	tolOrder(t, "TOL-other-out", 9042, 14_951_184, blockTime-tolTTLMS-tronTolerantBlockerGraceMS-1, tolFaceQuota)
	mine := tolOrder(t, "TOL-mine-ok", 9043, tolExpectedMicros, blockTime-113_000, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(41, tolPaidMicros, blockTime), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, mine.ID, result.OrderID)
	assert.Zero(t, tolQuota(t, 9042))
}

func TestTolerantSettle_OtherUsersOrderOutsideAmountBandDoesNotBlock(t *testing.T) {
	truncateTables(t)
	// 另一个用户的 ¥500 档订单（约 74.58 USDT）与这笔 15 USDT 的款无关。
	tolOrder(t, "TOL-other-tier", 9045, 74_582_856, tolBaseMS, 250_000_000)
	mine := tolOrder(t, "TOL-mine-tier", 9046, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(45, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, mine.ID, result.OrderID)
}

func TestTolerantSettle_OtherUsersPaidOrderStillBlocks(t *testing.T) {
	truncateTables(t)
	// 别人的订单已精确付清；他误付的第二笔不得被我的订单接走。
	paid := tolOrder(t, "TOL-other-paid", 9050, 14_951_184, tolBaseMS, tolFaceQuota)
	exact, err := SettleTronDeposit(tolTransfer(50, 14_951_184, tolBaseMS+90_000), "tron-scanner")
	require.NoError(t, err)
	require.True(t, exact.Credited)
	require.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, paid))

	tolOrder(t, "TOL-mine-vs-paid", 9051, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(51, tolPaidMicros, tolBaseMS+113_000), nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9051))
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9050), "别人的余额只有他自己那一笔")
}

func TestTolerantSettle_SameUserHistoryOnlyExemptWhenCreditWouldBeIdentical(t *testing.T) {
	truncateTables(t)
	// 真实事件形态：同一用户两张同面值的过期单 + 当前有效单，多付时三张算出的入账额度都是面值。
	tolOrder(t, "TOL-own-old1", 9060, 14_951_723, tolBaseMS-101*60*1000, tolFaceQuota)
	tolOrder(t, "TOL-own-old2", 9060, 14_954_021, tolBaseMS-21*60*1000, tolFaceQuota)
	live := tolOrder(t, "TOL-own-live", 9060, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(60, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, live.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9060))

	truncateTablesNow(t)
	// 少付时两张同面值单应付不同 → 入账额度不同 → 不能替用户选，转人工。
	tolOrder(t, "TOL-own-diffE-old", 9061, 16_000_000, tolBaseMS-21*60*1000, tolFaceQuota)
	tolOrder(t, "TOL-own-diffE-live", 9061, 15_000_000, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(61, 14_500_000, tolBaseMS+113_000), nil, tronTolerantNoteSameUserAmbiguous)
	assert.Zero(t, tolQuota(t, 9061))

	truncateTablesNow(t)
	// 面值不同的历史单同样转人工。
	tolOrder(t, "TOL-own-diffC-old", 9062, 14_951_723, tolBaseMS-21*60*1000, 55_000_000)
	tolOrder(t, "TOL-own-diffC-live", 9062, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(62, tolPaidMicros, tolBaseMS+113_000), nil, tronTolerantNoteSameUserAmbiguous)
	assert.Zero(t, tolQuota(t, 9062))
}

func TestTolerantSettle_OnlyFirstThreeOrdersPerRollingDayAreEligible(t *testing.T) {
	truncateTables(t)
	blockTime := tolBaseMS + 30*60*60*1000
	hour := int64(60 * 60 * 1000)
	// 前 24 小时内已有三张订单（都在金额带之外的时间窗，不构成阻断），当前是第四张。
	tolOrder(t, "TOL-thr-1", 9070, 14_950_001, blockTime-23*hour, tolFaceQuota)
	tolOrder(t, "TOL-thr-2", 9070, 14_950_002, blockTime-10*hour, tolFaceQuota)
	tolOrder(t, "TOL-thr-3", 9070, 14_950_003, blockTime-5*hour, tolFaceQuota)
	tolOrder(t, "TOL-thr-4", 9070, tolExpectedMicros, blockTime-113_000, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(70, tolPaidMicros, blockTime), nil, tronTolerantNoteThrottled)
	assert.Zero(t, tolQuota(t, 9070))

	truncateTablesNow(t)
	// 24 小时之外的订单不计数：当前是窗口内第三张，仍有资格。
	tolOrder(t, "TOL-thr-old", 9071, 14_950_001, blockTime-24*hour-1, tolFaceQuota)
	tolOrder(t, "TOL-thr-a", 9071, 14_950_002, blockTime-10*hour, tolFaceQuota)
	tolOrder(t, "TOL-thr-b", 9071, 14_950_003, blockTime-5*hour, tolFaceQuota)
	third := tolOrder(t, "TOL-thr-c", 9071, tolExpectedMicros, blockTime-113_000, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(71, tolPaidMicros, blockTime), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, third.ID, result.OrderID)
}

func TestTolerantSettle_ThrottledOrderStillBlocksOtherUsers(t *testing.T) {
	truncateTables(t)
	blockTime := tolBaseMS + 30*60*60*1000
	hour := int64(60 * 60 * 1000)
	tolOrder(t, "TOL-para-1", 9075, 14_950_001, blockTime-23*hour, tolFaceQuota)
	tolOrder(t, "TOL-para-2", 9075, 14_950_002, blockTime-10*hour, tolFaceQuota)
	tolOrder(t, "TOL-para-3", 9075, 14_950_003, blockTime-5*hour, tolFaceQuota)
	tolOrder(t, "TOL-para-4", 9075, 14_950_004, blockTime-113_000, tolFaceQuota)
	tolOrder(t, "TOL-victim", 9076, tolExpectedMicros, blockTime-113_000, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(75, tolPaidMicros, blockTime), nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9075))
	assert.Zero(t, tolQuota(t, 9076))
}

func TestTolerantSettle_MultipleInexactPaymentsForOneOrderAllGoManual(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "TOL-multi", 9090, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	first := tolTransfer(90, tolPaidMicros, tolBaseMS+113_000)
	second := tolTransfer(91, 14_100_000, tolBaseMS+200_000)

	// 同一批里两笔都对得上这张单：谁先遍历到都不算数，一起转人工。
	requireTolUnmatched(t, first, []TronTransferRecord{second}, tronTolerantNoteConflictDeposit)
	requireTolUnmatched(t, second, []TronTransferRecord{first}, tronTolerantNoteConflictDeposit)
	assert.Zero(t, tolQuota(t, 9090))
	assert.Equal(t, common.TopUpStatusPending, tolTopUpStatus(t, order))

	// 之后再来第三笔（另一批）：库里已有两笔未处置的在档款，仍转人工。
	requireTolUnmatched(t, tolTransfer(92, tolPaidMicros+1, tolBaseMS+400_000), nil, tronTolerantNoteConflictDeposit)
}

func TestTolerantSettle_DecisionIsMadeOnceAndNeverRevisited(t *testing.T) {
	truncateTables(t)
	blocker := tolOrder(t, "TOL-once-other", 9100, 14_951_184, tolBaseMS, tolFaceQuota)
	order := tolOrder(t, "TOL-once-mine", 9101, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(100, tolPaidMicros, tolBaseMS+113_000)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)

	// 阻断订单事后消失，重扫也不得把已经落未匹配的款自动入账。
	require.NoError(t, DB.Where("id = ?", blocker.ID).Delete(&TronTopupOrder{}).Error)
	replay, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, replay.AlreadyProcessed)
	assert.False(t, replay.Credited)
	assert.Zero(t, tolQuota(t, 9101))
	assert.Equal(t, common.TopUpStatusPending, tolTopUpStatus(t, order))
	assert.Equal(t, TronDepositStatusUnmatched, tolDeposit(t, transfer).Status)
}

func TestTolerantSettle_WalletCapFailureKeepsTheSelectedOrderForReview(t *testing.T) {
	truncateTables(t)
	tolUser(t, 9110, common.MaxQuota-10)
	order := tolOrder(t, "TOL-cap", 9110, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(110, tolPaidMicros, tolBaseMS+113_000)

	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.False(t, result.Credited)
	assert.True(t, result.NeedsReview)
	assert.Equal(t, order.ID, result.OrderID)
	assert.Equal(t, common.MaxQuota-10, tolQuota(t, 9110), "余额不变")
	assert.Equal(t, common.TopUpStatusPending, tolTopUpStatus(t, order))

	deposit := tolDeposit(t, transfer)
	assert.Equal(t, TronDepositStatusReview, deposit.Status)
	require.NotNil(t, deposit.OrderID)
	assert.Equal(t, order.ID, *deposit.OrderID, "留单必须带着已选定的订单")
	assert.Equal(t, TronMatchModeTolerant, deposit.MatchMode)
	assert.Zero(t, deposit.CreditedQuota)

	var tickets []TronTopupTicket
	require.NoError(t, DB.Where("tx_id = ?", transfer.TxID).Find(&tickets).Error)
	require.Len(t, tickets, 1)
	assert.Equal(t, TronTicketReasonCreditFailed, tickets[0].Reason)
	assert.Equal(t, TronTicketStatusOpen, tickets[0].Status)
	assert.Equal(t, order.ID, tickets[0].OrderID)

	replay, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, replay.AlreadyProcessed)
	require.NoError(t, DB.Where("tx_id = ?", transfer.TxID).Find(&tickets).Error)
	assert.Len(t, tickets, 1, "重扫不重复建工单")
}

func TestTolerantSettle_ExactAmountOrderIsNeverBypassed(t *testing.T) {
	truncateTables(t)
	// 金额恰好等于某张订单的应付（哪怕那张单是付款之后才建的）：留给精确路径与人工，不绕到容差。
	tolOrder(t, "TOL-exact-later", 9120, tolPaidMicros, tolBaseMS+600_000, tolFaceQuota)
	tolOrder(t, "TOL-exact-mine", 9121, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	requireTolUnmatched(t, tolTransfer(120, tolPaidMicros, tolBaseMS+113_000), nil, tronTolerantNoteExactAmountOrder)
	assert.Zero(t, tolQuota(t, 9121))
}

func TestTolerantSettle_RejectsMultiSourceTransfers(t *testing.T) {
	truncateTables(t)
	tolOrder(t, "TOL-multi-src", 9130, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(130, tolPaidMicros, tolBaseMS+113_000)
	transfer.SourceAddresses = "TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh,TX5YfLmACY2XttkbqS4485VbLsAByFtjG5"
	_, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.Error(t, err)
	assert.Zero(t, tolQuota(t, 9130))
	var deposits int64
	require.NoError(t, DB.Model(&TronDeposit{}).Count(&deposits).Error)
	assert.Zero(t, deposits)
}

func TestTronSerialLock_MissingRowFailsClosed(t *testing.T) {
	truncateTables(t)
	tolOrder(t, "TOL-serial", 9140, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	require.NoError(t, DB.Where("name = ?", tronSerialLockName).Delete(&TronSerialLock{}).Error)
	t.Cleanup(func() { require.NoError(t, EnsureTronSerialLock()) })

	_, err := SettleTronDepositTolerant(tolTransfer(140, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
	require.ErrorIs(t, err, ErrTronSerialLockUnavailable)
	_, err = SettleTronDeposit(tolTransfer(141, tolExpectedMicros, tolBaseMS+113_000), "tron-scanner")
	require.ErrorIs(t, err, ErrTronSerialLockUnavailable)
	assert.Zero(t, tolQuota(t, 9140))
	var deposits int64
	require.NoError(t, DB.Model(&TronDeposit{}).Count(&deposits).Error)
	assert.Zero(t, deposits)

	require.NoError(t, EnsureTronSerialLock())
	require.NoError(t, EnsureTronSerialLock(), "重复预建必须幂等")
	var rows int64
	require.NoError(t, DB.Model(&TronSerialLock{}).Count(&rows).Error)
	assert.Equal(t, int64(1), rows)
}

// truncateTablesNow clears the TRON ledger immediately so one test can stage
// several independent scenarios; truncateTables only cleans up at test end.
func truncateTablesNow(t *testing.T) {
	t.Helper()
	for _, table := range []string{"logs", "users", "top_ups", "tron_topup_orders", "tron_deposits", "tron_topup_tickets"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
}

func TestTronSerialLock_EveryLedgerWriteTakesItFirst(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "TOL-serial-all", 9150, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	claimed := tolTransfer(150, 14_100_000, tolBaseMS+113_000)
	_, err := RecordUnmatchedTronDeposit(claimed)
	require.NoError(t, err)
	ticket, err := submitTronTopupClaimAt(9150, order.TradeNo, claimed.TxID, "mine", tolBaseMS+200_000)
	require.NoError(t, err)

	require.NoError(t, DB.Where("name = ?", tronSerialLockName).Delete(&TronSerialLock{}).Error)
	t.Cleanup(func() { require.NoError(t, EnsureTronSerialLock()) })

	draftTopUp := func(tradeNo string) (*TopUp, *TronTopupOrder) {
		topUp := &TopUp{UserId: 9150, Amount: 100, Money: 100, TradeNo: tradeNo, PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, CreateTime: tolBaseMS / 1000, Status: common.TopUpStatusPending}
		draft := &TronTopupOrder{UserID: 9150, TradeNo: tradeNo, ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: 77_000_000, RateCNYMicros: 6_687_834, QuoteUpdatedAtMS: tolBaseMS, ExpiresAtMS: tolBaseMS + tolTTLMS, CreditQuota: tolFaceQuota, CreatedAtMS: tolBaseMS}
		return topUp, draft
	}
	multiSource := tolTransfer(151, tolPaidMicros, tolBaseMS+113_000)
	multiSource.SourceAddresses = "a,b"
	writes := map[string]func() error{
		"建单（旧入口）": func() error { topUp, draft := draftTopUp("TOL-serial-new1"); return CreateTronTopupOrder(topUp, draft) },
		"建单": func() error {
			topUp, draft := draftTopUp("TOL-serial-new2")
			_, _, err := CreateOrGetActiveTronTopupOrderAt(topUp, draft, func() int64 { return tolBaseMS + tolTTLMS + 1 })
			return err
		},
		"精确入账": func() error {
			_, err := SettleTronDeposit(tolTransfer(152, tolExpectedMicros, tolBaseMS+113_000), "tron-scanner")
			return err
		},
		"容差入账": func() error {
			_, err := SettleTronDepositTolerant(tolTransfer(153, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
			return err
		},
		"未匹配落库": func() error {
			_, err := RecordUnmatchedTronDeposit(tolTransfer(154, 99_000_000, tolBaseMS+113_000))
			return err
		},
		"超限留单": func() error {
			_, err := RecordFailedTronSettlement(tolTransfer(155, tolExpectedMicros, tolBaseMS+113_000))
			return err
		},
		"多来源隔离": func() error { _, err := RecordReviewTronDeposit(multiSource, "multiple_senders"); return err },
		"提交申诉": func() error {
			_, err := submitTronTopupClaimAt(9150, order.TradeNo, tolTransfer(156, 1, 1).TxID, "x", tolBaseMS+300_000)
			return err
		},
		"通过工单": func() error { return ResolveTronTopupTicket(ticket.ID, order.ID, claimed, 1, "verified") },
		"驳回工单": func() error { return RejectTronTopupTicket(ticket.ID, 1, "no", tolBaseMS+400_000) },
	}
	for name, write := range writes {
		assert.ErrorIs(t, write(), ErrTronSerialLockUnavailable, "%s 必须先取账本串行锁", name)
	}
	assert.Zero(t, tolQuota(t, 9150))
	var deposits, orders int64
	require.NoError(t, DB.Model(&TronDeposit{}).Count(&deposits).Error)
	require.NoError(t, DB.Model(&TronTopupOrder{}).Count(&orders).Error)
	assert.EqualValues(t, 1, deposits, "只剩申诉前记下的那一笔")
	assert.EqualValues(t, 1, orders)
}

// failTronWrites makes every create or update of one table fail until the
// returned function is called, to prove a settlement is all-or-nothing.
func failTronWrites(t *testing.T, operation string, table string) func() {
	t.Helper()
	name := "tol_fail_" + operation + "_" + table
	hook := func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == table {
			_ = tx.AddError(errors.New("injected " + operation + " failure on " + table))
		}
	}
	remove := func() {}
	switch operation {
	case "create":
		require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(name, hook))
		remove = func() { _ = DB.Callback().Create().Remove(name) }
	case "update":
		require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(name, hook))
		remove = func() { _ = DB.Callback().Update().Remove(name) }
	default:
		t.Fatalf("unknown operation %q", operation)
	}
	t.Cleanup(remove)
	return remove
}

func TestTolerantSettle_AnyFailedWriteRollsEverythingBackAndRetryCreditsOnce(t *testing.T) {
	steps := []struct{ operation, table string }{
		{"create", "tron_deposits"},
		{"update", "users"},
		{"update", "top_ups"},
		{"update", "tron_topup_orders"},
		{"update", "tron_deposits"},
	}
	for i, step := range steps {
		t.Run(step.operation+" "+step.table, func(t *testing.T) {
			truncateTables(t)
			truncateTablesNow(t)
			userID := 9300 + i
			order := tolOrder(t, fmt.Sprintf("TOL-fault-%d", i), userID, tolExpectedMicros, tolBaseMS, tolFaceQuota)
			transfer := tolTransfer(300+i, tolPaidMicros, tolBaseMS+113_000)

			restore := failTronWrites(t, step.operation, step.table)
			_, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
			require.Error(t, err, "写入失败必须报错，让扫描窗口不推进")
			assert.Zero(t, tolQuota(t, userID), "不得留下加了一半的余额")
			assert.Equal(t, common.TopUpStatusPending, tolTopUpStatus(t, order))
			var deposits, tickets int64
			require.NoError(t, DB.Model(&TronDeposit{}).Count(&deposits).Error)
			require.NoError(t, DB.Model(&TronTopupTicket{}).Count(&tickets).Error)
			assert.Zero(t, deposits, "失败不落收款记录，重扫时重新判定")
			assert.Zero(t, tickets)
			var stored TronTopupOrder
			require.NoError(t, DB.Where("id = ?", order.ID).First(&stored).Error)
			assert.Zero(t, stored.CompletedAtMS)

			restore()
			result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
			require.NoError(t, err)
			assert.True(t, result.Credited)
			assert.Equal(t, tolFaceQuota, tolQuota(t, userID), "重试只入账一次")
		})
	}
}

func TestTolerantSettle_FailedReviewTicketDoesNotLeaveFundsWithoutATicket(t *testing.T) {
	truncateTables(t)
	tolUser(t, 9310, common.MaxQuota-10)
	order := tolOrder(t, "TOL-fault-ticket", 9310, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(310, tolPaidMicros, tolBaseMS+113_000)

	restore := failTronWrites(t, "create", "tron_topup_tickets")
	_, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.Error(t, err)
	var deposits int64
	require.NoError(t, DB.Model(&TronDeposit{}).Count(&deposits).Error)
	assert.Zero(t, deposits, "工单没建成就不能留下一笔没人跟进的待复核款")

	restore()
	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.NeedsReview)
	assert.Equal(t, order.ID, result.OrderID)
	assert.Equal(t, common.MaxQuota-10, tolQuota(t, 9310))
}

func TestTolerantSettle_WalletCapIsCheckedAgainstTheAmountActuallyCredited(t *testing.T) {
	truncateTables(t)
	// 恰好装满：允许。
	tolUser(t, 9320, common.MaxQuota-tolFaceQuota)
	tolOrder(t, "TOL-cap-exact", 9320, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(320, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, common.MaxQuota, tolQuota(t, 9320))

	truncateTablesNow(t)
	// 多出一个单位：留单。
	tolUser(t, 9321, common.MaxQuota-tolFaceQuota+1)
	tolOrder(t, "TOL-cap-over", 9321, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err = SettleTronDepositTolerant(tolTransfer(321, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.NeedsReview)
	assert.Equal(t, common.MaxQuota-tolFaceQuota+1, tolQuota(t, 9321))

	truncateTablesNow(t)
	// 面值装不下、但少付后的实际入账额度装得下：按实际额度入账，不因面值被拒。
	tolUser(t, 9322, common.MaxQuota-47_000_000)
	tolOrder(t, "TOL-cap-under", 9322, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err = SettleTronDepositTolerant(tolTransfer(322, 13_952_525, tolBaseMS+113_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, common.MaxQuota-47_000_000+46_656_083, tolQuota(t, 9322))
}

func TestTolerantSettle_LatePaymentHeldForAnOlderOrderDoesNotBlockTheNewOne(t *testing.T) {
	truncateTables(t)
	// 同一用户：旧单过期后才收到一笔精确金额的款（按精确规则归到旧单、待人工）。
	// 这笔已有归属的待复核款不应把他新单的容差入账永远挡住。
	oldOrder := tolOrder(t, "TOL-late-old", 9330, 14_951_723, tolBaseMS-30*60*1000, tolFaceQuota)
	late, err := SettleTronDeposit(tolTransfer(330, 14_951_723, tolBaseMS+30_000), "tron-scanner")
	require.NoError(t, err)
	require.True(t, late.NeedsReview)
	require.Equal(t, oldOrder.ID, late.OrderID)

	newOrder := tolOrder(t, "TOL-late-new", 9330, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	result, err := SettleTronDepositTolerant(tolTransfer(331, tolPaidMicros, tolBaseMS+113_000), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, newOrder.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9330), "只有新单这一笔入账；旧单那笔仍待人工")
}

func TestCreateOrGetActiveTronTopupOrderAt_FailedInsertRestoresTheDraftAndLeavesNoHalfOrder(t *testing.T) {
	truncateTables(t)
	tolOrder(t, "CLK-taken", 9340, tolExpectedMicros, tolBaseMS-2*tolTTLMS, tolFaceQuota)
	provisional := tolBaseMS
	topUp := &TopUp{UserId: 9340, Amount: 100, Money: 100, TradeNo: "CLK-collide", PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, CreateTime: provisional / 1000, Status: common.TopUpStatusPending}
	// 应付金额与已有订单相同：订单行插入会撞唯一索引，而充值单行此时已经插入。
	draft := &TronTopupOrder{UserID: 9340, TradeNo: "CLK-collide", ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: tolExpectedMicros, RateCNYMicros: 6_687_834, QuoteUpdatedAtMS: provisional, ExpiresAtMS: provisional + tolTTLMS, CreditQuota: tolFaceQuota, CreatedAtMS: provisional}

	_, created, err := CreateOrGetActiveTronTopupOrderAt(topUp, draft, func() int64 { return provisional + 45_000 })
	require.Error(t, err)
	assert.False(t, created)
	assert.Zero(t, topUp.Id)
	assert.Zero(t, draft.ID)
	assert.Zero(t, draft.TopUpID)
	assert.Equal(t, provisional, draft.CreatedAtMS)
	assert.Equal(t, provisional+tolTTLMS, draft.ExpiresAtMS)
	assert.Equal(t, provisional/1000, topUp.CreateTime)
	var topUps, orders int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", "CLK-collide").Count(&topUps).Error)
	require.NoError(t, DB.Model(&TronTopupOrder{}).Where("trade_no = ?", "CLK-collide").Count(&orders).Error)
	assert.Zero(t, topUps, "充值单行随事务回滚，不留半张订单")
	assert.Zero(t, orders)
}
