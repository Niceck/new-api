package model

import (
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则 v2（站长 2026-10-18 定）：别人的订单若已被一笔精确金额的款付清，只在系统确认到账后
// 2 分钟内挡住别人的容差入账；之后不再挡。其余情形照 v1 挡满「创建～到期后 2 小时」。
const (
	pbPaidBlockMS int64 = 2 * 60 * 1000
	// 2026-10-18 真实事件：用户 528 的订单 21 精确付清约一小时后，用户 531 的订单 22 多付 3.4%。
	pbBlockerExpected int64 = 14_960_115
	pbMineExpected    int64 = 14_960_023
	pbMinePaid        int64 = 15_474_080
	// 订单 21：建单后 39.405 秒上链，224.707 秒扫描入账。
	pbBlockerBlockLeadMS  int64 = 39_405
	pbBlockerCreditLeadMS int64 = 224_707
	// 订单 22 在订单 21 建单后 3714.912 秒创建，再过 239.493 秒上链。
	pbMineCreatedAfterMS int64 = 3_714_912
	pbMineBlockLeadMS    int64 = 239_493
)

// pbPayExact pays the order its exact amount at blockMS; the scanner credits it at creditMS.
func pbPayExact(t *testing.T, order *TronTopupOrder, seq int, blockMS, creditMS int64) {
	t.Helper()
	transfer := tolTransfer(seq, order.ExpectedAmountMicros, blockMS)
	transfer.ObservedAtMS = creditMS
	result, err := SettleTronDeposit(transfer, "tron-scanner")
	require.NoError(t, err)
	require.True(t, result.Credited)
	var stored TronTopupOrder
	require.NoError(t, DB.Where("id = ?", order.ID).First(&stored).Error)
	require.Equal(t, creditMS, stored.CompletedAtMS, "完成时间 = 扫描入账时间")
}

// pbMineAnHourLater creates the paying user's order an hour after the blocker's
// and returns the transfer that pays it 3.4% over, as in the real event.
func pbMineAnHourLater(t *testing.T, tradeNo string, userID int, seq int) (*TronTopupOrder, TronTransferRecord) {
	t.Helper()
	created := tolBaseMS + pbMineCreatedAfterMS
	order := tolOrder(t, tradeNo, userID, pbMineExpected, created, tolFaceQuota)
	return order, tolTransfer(seq, pbMinePaid, created+pbMineBlockLeadMS)
}

func TestTolerantSettle_PaidExactOrderStopsBlockingAfterItsCredit_RealEvent(t *testing.T) {
	truncateTables(t)
	blocker := tolOrder(t, "PB-528", 9400, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 400, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	mine, transfer := pbMineAnHourLater(t, "PB-531", 9401, 401)

	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited, "别人一小时前就精确付清了，这笔不再被他的订单挡住")
	assert.Equal(t, mine.ID, result.OrderID)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9401), "多付按订单面值入账")
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9400), "别人的余额只有他自己那一笔")
	assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, mine))
	deposit := tolDeposit(t, transfer)
	assert.Equal(t, TronMatchModeTolerant, deposit.MatchMode)
	assert.Equal(t, "v2", deposit.MatchRule, "按新规则判定的款要记新版本号")
}

func TestTolerantSettle_PaidExactOrderBlocksForTwoMinutesAfterItsCredit(t *testing.T) {
	require.Equal(t, pbPaidBlockMS, tronTolerantPaidExactBlockMS, "站长定的是 2 分钟")
	truncateTables(t)
	creditMS := tolBaseMS + pbBlockerCreditLeadMS
	setup := func(t *testing.T) {
		blocker := tolOrder(t, "PB-edge-other", 9410, pbBlockerExpected, tolBaseMS, tolFaceQuota)
		pbPayExact(t, blocker, 410, tolBaseMS+pbBlockerBlockLeadMS, creditMS)
		tolOrder(t, "PB-edge-mine", 9411, pbMineExpected, tolBaseMS+60_000, tolFaceQuota)
	}

	// 确认到账后恰好 2 分钟上链：仍挡（他可能还没看到「已到账」就又付了一笔）。
	setup(t)
	requireTolUnmatched(t, tolTransfer(411, pbMinePaid, creditMS+pbPaidBlockMS), nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9411))

	// 早于确认到账的款当然也挡。
	truncateTablesNow(t)
	setup(t)
	requireTolUnmatched(t, tolTransfer(412, pbMinePaid, creditMS-1), nil, tronTolerantNoteBlockedOtherUser)

	// 晚 1 毫秒：不再挡。
	truncateTablesNow(t)
	setup(t)
	result, err := SettleTronDepositTolerant(tolTransfer(413, pbMinePaid, creditMS+pbPaidBlockMS+1), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9411))
}

func TestTolerantSettle_OrderPaidByToleranceKeepsBlockingTheFullWindow(t *testing.T) {
	truncateTables(t)
	// 别人那张单本身是按容差入账的：归属是推断出来的，照旧挡满窗口。
	blocker := tolOrder(t, "PB-tol-other", 9420, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	first, err := SettleTronDepositTolerant(tolTransfer(420, 15_000_000, tolBaseMS+90_000), nil, "tron-scanner")
	require.NoError(t, err)
	require.True(t, first.Credited)
	require.Equal(t, blocker.ID, first.OrderID)

	_, transfer := pbMineAnHourLater(t, "PB-tol-mine", 9421, 421)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9421))
}

func TestTolerantSettle_OrderPaidThroughATicketKeepsBlockingTheFullWindow(t *testing.T) {
	truncateTables(t)
	// 别人那张单是管理员通过工单入账的：同样不是「一笔精确款付清」，照旧挡。
	blocker := tolOrder(t, "PB-man-other", 9430, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	claimed := tolTransfer(430, 14_100_000, tolBaseMS+113_000)
	_, err := RecordUnmatchedTronDeposit(claimed)
	require.NoError(t, err)
	ticket, err := submitTronTopupClaimAt(9430, blocker.TradeNo, claimed.TxID, "mine", tolBaseMS+300_000)
	require.NoError(t, err)
	require.NoError(t, ResolveTronTopupTicket(ticket.ID, blocker.ID, claimed, 1, "verified"))
	require.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, blocker))

	_, transfer := pbMineAnHourLater(t, "PB-man-mine", 9431, 431)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9431))
}

func TestTolerantSettle_LateExactPaymentApprovedThroughATicketKeepsBlocking(t *testing.T) {
	truncateTables(t)
	// 金额精确、但过期后才到账：系统不自动入账，管理员通过工单入账。不是扫描器自动入账的那一种，照旧挡。
	blocker := tolOrder(t, "PB-late-other", 9435, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	late := tolTransfer(435, pbBlockerExpected, tolBaseMS+tolTTLMS+60_000)
	held, err := SettleTronDeposit(late, "tron-scanner")
	require.NoError(t, err)
	require.True(t, held.NeedsReview)
	var ticket TronTopupTicket
	require.NoError(t, DB.Where("order_id = ? AND reason = ?", blocker.ID, TronTicketReasonLatePayment).First(&ticket).Error)
	require.NoError(t, ResolveTronTopupTicket(ticket.ID, blocker.ID, late, 1, "verified"))
	require.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, blocker))
	require.Equal(t, TronMatchModeManual, tolDeposit(t, late).MatchMode)

	_, transfer := pbMineAnHourLater(t, "PB-late-mine", 9436, 436)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9436))
}

func TestTolerantSettle_PaidExactOrderPaidTwiceKeepsBlocking(t *testing.T) {
	truncateTables(t)
	blocker := tolOrder(t, "PB-dup-other", 9440, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 440, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	// 他又按原金额付了一次：记为待复核的重复付款，且不挂订单号，只能按金额认出来。
	// 这个人在重复付款，他的单不算「已了结」。
	dup, err := SettleTronDeposit(tolTransfer(441, pbBlockerExpected, tolBaseMS+600_000), "tron-scanner")
	require.NoError(t, err)
	require.True(t, dup.NeedsReview)
	require.Nil(t, tolDeposit(t, tolTransfer(441, pbBlockerExpected, tolBaseMS+600_000)).OrderID)

	_, transfer := pbMineAnHourLater(t, "PB-dup-mine", 9441, 442)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9441))
}

func TestTolerantSettle_PaidExactOrderWithAnOpenTicketKeepsBlocking(t *testing.T) {
	truncateTables(t)
	blocker := tolOrder(t, "PB-tkt-other", 9450, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	// 付清之前他先就另一笔款提了申诉（那笔款的金额不在任何订单的识别范围内），工单还开着。
	other := tolTransfer(450, 5_000_000, tolBaseMS+70_000)
	_, err := RecordUnmatchedTronDeposit(other)
	require.NoError(t, err)
	_, err = submitTronTopupClaimAt(9450, blocker.TradeNo, other.TxID, "this one is mine too", tolBaseMS+80_000)
	require.NoError(t, err)
	pbPayExact(t, blocker, 451, tolBaseMS+90_000, tolBaseMS+290_000)

	_, transfer := pbMineAnHourLater(t, "PB-tkt-mine", 9451, 452)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9451))
}

func TestTolerantSettle_PaidExactOrderWhosePaymentIsDisputedKeepsBlocking(t *testing.T) {
	// 第三个人早半小时下过单（已过期、仍在申诉期），他说那笔精确款是他的。争议工单挂在他自己的
	// 订单上，只能按交易号认出来。两次运行只差这张工单：没有争议时同样的款自动入账。
	run := func(t *testing.T, disputed bool) TronTransferRecord {
		truncateTablesNow(t)
		claimant := tolOrder(t, "PB-dsp-claimant", 9492, 14_960_300, tolBaseMS-30*60*1000, tolFaceQuota)
		blocker := tolOrder(t, "PB-dsp-other", 9490, pbBlockerExpected, tolBaseMS, tolFaceQuota)
		exact := tolTransfer(490, pbBlockerExpected, tolBaseMS+pbBlockerBlockLeadMS)
		exact.ObservedAtMS = tolBaseMS + pbBlockerCreditLeadMS
		credited, err := SettleTronDeposit(exact, "tron-scanner")
		require.NoError(t, err)
		require.True(t, credited.Credited)
		require.Equal(t, blocker.ID, credited.OrderID)
		if disputed {
			ticket, err := submitTronTopupClaimAt(9492, claimant.TradeNo, exact.TxID, "that was my payment", tolBaseMS+300_000)
			require.NoError(t, err)
			require.Equal(t, TronTicketReasonDispute, ticket.Reason)
		}
		// 我的单在别人建单后 1 小时 55 分：仍在他那张单的阻断窗口内，已出了争议方那张单的窗口。
		mineCreated := tolBaseMS + 115*60*1000
		tolOrder(t, "PB-dsp-mine", 9491, pbMineExpected, mineCreated, tolFaceQuota)
		return tolTransfer(491, pbMinePaid, mineCreated+pbMineBlockLeadMS)
	}

	truncateTables(t)
	result, err := SettleTronDepositTolerant(run(t, false), nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited, "对照：没有争议时不挡")

	requireTolUnmatched(t, run(t, true), nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9491))
}

func TestTolerantSettle_PaidExactOrderWithoutCreditTimeKeepsBlocking(t *testing.T) {
	truncateTables(t)
	blocker := tolOrder(t, "PB-nots-other", 9460, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 460, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	require.NoError(t, DB.Model(&TronTopupOrder{}).Where("id = ?", blocker.ID).Update("completed_at_ms", 0).Error)

	_, transfer := pbMineAnHourLater(t, "PB-nots-mine", 9461, 461)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
}

func TestTolerantSettle_PaidExactOrderWithAnAbsurdCreditTimeKeepsBlocking(t *testing.T) {
	truncateTables(t)
	// 完成时间被改成极大值：「完成时间 + 2 分钟」会溢出成负数。必须照旧挡，不能因此放行。
	blocker := tolOrder(t, "PB-ovf-other", 9500, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 500, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	require.NoError(t, DB.Model(&TronTopupOrder{}).Where("id = ?", blocker.ID).Update("completed_at_ms", int64(math.MaxInt64)).Error)

	_, transfer := pbMineAnHourLater(t, "PB-ovf-mine", 9501, 501)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9501))
}

func TestTolerantSettle_PaidExactOrderFoundLateBlocksUntilTwoMinutesAfterThatCredit(t *testing.T) {
	truncateTables(t)
	// 每小时的宽对账晚了 3 小时才扫到别人的精确款，完成时间就是扫到的时间。
	// 在那之前上链的不精确款，哪怕比那笔精确款晚一个多小时，也照旧挡。
	blocker := tolOrder(t, "PB-recon-other", 9510, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 510, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+3*60*60*1000)

	_, transfer := pbMineAnHourLater(t, "PB-recon-mine", 9511, 511)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9511))
}

func TestTolerantSettle_ExactCreditWithoutMatchModeKeepsBlocking(t *testing.T) {
	truncateTables(t)
	// v31 之前入账的款没有记 match_mode：不认作「扫描器自动入账的精确款」。
	blocker := tolOrder(t, "PB-nomode-other", 9520, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 520, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	require.NoError(t, DB.Model(&TronDeposit{}).Where("order_id = ?", blocker.ID).Update("match_mode", "").Error)

	_, transfer := pbMineAnHourLater(t, "PB-nomode-mine", 9521, 521)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
}

func TestTolerantSettle_SameAmountPaidElsewhereStillCountsAgainstThePaidOrder(t *testing.T) {
	truncateTables(t)
	// 按金额认款时不分收款地址与代币：别处一笔同额的款也算这张单的又一笔付款，保守地照旧挡。
	blocker := tolOrder(t, "PB-addr-other", 9530, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, blocker, 530, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	elsewhere := TronDeposit{TxID: fmt.Sprintf("%064x", 531), BlockTimestampMS: tolBaseMS + 600_000, FromAddress: "TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh", ToAddress: "TLsV52sRDL79HXGGm9yzwKibb6BeruhUzy", TokenContract: testTronUSDTContract, AmountMicros: pbBlockerExpected, Status: TronDepositStatusUnmatched, ObservedAtMS: tolBaseMS + 800_000}
	require.NoError(t, DB.Create(&elsewhere).Error)

	_, transfer := pbMineAnHourLater(t, "PB-addr-mine", 9531, 532)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
}

func TestTolerantSettle_OnlyThePaidOffOrderStopsBlocking(t *testing.T) {
	truncateTables(t)
	paid := tolOrder(t, "PB-mix-paid", 9470, pbBlockerExpected, tolBaseMS, tolFaceQuota)
	pbPayExact(t, paid, 470, tolBaseMS+pbBlockerBlockLeadMS, tolBaseMS+pbBlockerCreditLeadMS)
	// 第三个人还挂着一张没付的同档单：它照旧挡。
	tolOrder(t, "PB-mix-pending", 9472, 14_960_500, tolBaseMS+pbMineCreatedAfterMS-5*60*1000, tolFaceQuota)
	_, transfer := pbMineAnHourLater(t, "PB-mix-mine", 9471, 471)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Zero(t, tolQuota(t, 9471))
	assert.Zero(t, tolQuota(t, 9472))
}

func TestTolerantSettle_UnmatchedDepositsRecordTheRuleVersion(t *testing.T) {
	truncateTables(t)
	tolOrder(t, "PB-ver-other", 9480, 14_951_184, tolBaseMS, tolFaceQuota)
	tolOrder(t, "PB-ver-mine", 9481, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(480, tolPaidMicros, tolBaseMS+113_000)
	requireTolUnmatched(t, transfer, nil, tronTolerantNoteBlockedOtherUser)
	assert.Equal(t, "v2", tolDeposit(t, transfer).MatchRule)
}
