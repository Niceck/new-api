package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every claim or dispute a user opens raises an operator alert. Per-order
// limits do not bound that, since orders are free to create; a user may open
// at most three in any rolling 24 hours, however many orders they hold.

func recordTolDeposit(t *testing.T, seq int) TronTransferRecord {
	t.Helper()
	transfer := tolTransfer(seq, 50_000_000+int64(seq), tolBaseMS+60_000)
	_, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	return transfer
}

func countUserTickets(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&TronTopupTicket{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func TestTronClaimBudget_ThreeNewTicketsPerUserPerDayAcrossOrders(t *testing.T) {
	truncateTables(t)
	first := tolOrder(t, "BUDGET-1", 9300, 50_000_000, tolBaseMS, tolFaceQuota)
	second := tolOrder(t, "BUDGET-2", 9300, 50_000_001, tolBaseMS+1, tolFaceQuota)
	deposits := []TronTransferRecord{recordTolDeposit(t, 301), recordTolDeposit(t, 302), recordTolDeposit(t, 303), recordTolDeposit(t, 304)}
	t0 := tolBaseMS + 10*60*1000

	_, err := submitTronTopupClaimAt(9300, first.TradeNo, deposits[0].TxID, "one", t0)
	require.NoError(t, err)
	_, err = submitTronTopupClaimAt(9300, first.TradeNo, deposits[1].TxID, "two", t0+1)
	require.NoError(t, err)
	third, err := submitTronTopupClaimAt(9300, second.TradeNo, deposits[2].TxID, "three", t0+2)
	require.NoError(t, err)

	_, err = submitTronTopupClaimAt(9300, second.TradeNo, deposits[3].TxID, "four", t0+3)
	require.ErrorIs(t, err, ErrTronClaimUserBudgetExceeded)
	assert.EqualValues(t, 3, countUserTickets(t, 9300))

	again, err := submitTronTopupClaimAt(9300, second.TradeNo, deposits[2].TxID, "retry", t0+4)
	require.NoError(t, err, "查看已有工单不受上限影响")
	assert.Equal(t, third.ID, again.ID)

	_, err = submitTronTopupClaimAt(9300, second.TradeNo, deposits[3].TxID, "a day later", t0+24*60*60*1000+1)
	require.NoError(t, err, "滚动 24 小时之后恢复")
}

func TestTronClaimBudget_DisputesShareTheBudget(t *testing.T) {
	truncateTables(t)
	_, credited := tolCreditedToUserA(t)
	claimOrder := tolOrder(t, "BUDGET-claims", 9310, 50_000_000, tolBaseMS, tolFaceQuota)
	deposits := []TronTransferRecord{recordTolDeposit(t, 311), recordTolDeposit(t, 312)}
	t0 := tolBaseMS + 30*60*1000

	for index, deposit := range deposits {
		_, err := submitTronTopupClaimAt(9310, claimOrder.TradeNo, deposit.TxID, "claim", t0+int64(index))
		require.NoError(t, err)
	}
	dispute, err := submitTronTopupClaimAt(9310, tolLateOrderB(t, "BUDGET-dispute-1", 9310).TradeNo, credited.TxID, "mine", t0+10)
	require.NoError(t, err)
	require.Equal(t, TronTicketReasonDispute, dispute.Reason)

	_, err = submitTronTopupClaimAt(9310, tolOrder(t, "BUDGET-dispute-2", 9310, 14_951_185, tolBaseMS-tolTTLMS-tronTolerantBlockerGraceMS-60_000, tolFaceQuota).TradeNo, credited.TxID, "mine again", t0+20)
	require.ErrorIs(t, err, ErrTronClaimUserBudgetExceeded)
	assert.EqualValues(t, 3, countUserTickets(t, 9310))
}

func TestTronClaimBudget_SystemTicketsDoNotCount(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "BUDGET-system", 9320, 50_000_000, tolBaseMS, tolFaceQuota)
	t0 := tolBaseMS + 10*60*1000
	for index, reason := range []string{TronTicketReasonLatePayment, TronTicketReasonCreditFailed, TronTicketReasonLatePayment} {
		require.NoError(t, DB.Create(&TronTopupTicket{UserID: 9320, OrderID: order.ID, ClaimKey: fmt.Sprintf("system:%d", index), TxID: fmt.Sprintf("%064x", 330+index), Reason: reason, Status: TronTicketStatusOpen, CreatedAtMS: t0, UpdatedAtMS: t0}).Error)
	}

	_, err := submitTronTopupClaimAt(9320, order.TradeNo, recordTolDeposit(t, 321).TxID, "mine", t0+1)
	require.NoError(t, err)
}

func TestTronClaimBudget_RefusedCorrectionLeavesTheOpenClaimUntouched(t *testing.T) {
	truncateTables(t)
	first := tolOrder(t, "BUDGET-fix-1", 9330, 50_000_000, tolBaseMS, tolFaceQuota)
	second := tolOrder(t, "BUDGET-fix-2", 9330, 50_000_001, tolBaseMS+1, tolFaceQuota)
	deposits := []TronTransferRecord{recordTolDeposit(t, 331), recordTolDeposit(t, 332), recordTolDeposit(t, 333), recordTolDeposit(t, 334)}
	t0 := tolBaseMS + 10*60*1000
	open, err := submitTronTopupClaimAt(9330, first.TradeNo, deposits[0].TxID, "one", t0)
	require.NoError(t, err)
	for index, deposit := range deposits[1:3] {
		_, err = submitTronTopupClaimAt(9330, second.TradeNo, deposit.TxID, "other order", t0+1+int64(index))
		require.NoError(t, err)
	}

	_, err = submitTronTopupClaimAt(9330, first.TradeNo, deposits[3].TxID, "correction", t0+10)
	require.ErrorIs(t, err, ErrTronClaimUserBudgetExceeded)
	var kept TronTopupTicket
	require.NoError(t, DB.First(&kept, open.ID).Error)
	assert.Equal(t, TronTicketStatusOpen, kept.Status)
	assert.Equal(t, open.UpdatedAtMS, kept.UpdatedAtMS)
	assert.Empty(t, kept.AdminNote)
}

func TestTronClaimBudget_WindowIsExactlyTwentyFourHours(t *testing.T) {
	truncateTables(t)
	earlier := tolOrder(t, "BUDGET-edge-earlier", 9340, 50_000_001, tolBaseMS, tolFaceQuota)
	order := tolOrder(t, "BUDGET-edge", 9340, 50_000_000, tolBaseMS, tolFaceQuota)
	t0 := tolBaseMS + 10*60*1000
	// 放在另一张订单上：本单的三次机会不受影响，只看跨订单的每日上限。
	for index := range 3 {
		require.NoError(t, DB.Create(&TronTopupTicket{UserID: 9340, OrderID: earlier.ID, ClaimKey: fmt.Sprintf("edge:%d", index), TxID: fmt.Sprintf("%064x", 340+index), Reason: TronTicketReasonUserClaim, Status: TronTicketStatusRejected, CreatedAtMS: t0, UpdatedAtMS: t0}).Error)
	}
	deposit := recordTolDeposit(t, 345)
	day := int64(24 * 60 * 60 * 1000)

	_, err := submitTronTopupClaimAt(9340, order.TradeNo, deposit.TxID, "one ms early", t0+day-1)
	require.ErrorIs(t, err, ErrTronClaimUserBudgetExceeded, "已驳回的工单照样计数")
	_, err = submitTronTopupClaimAt(9340, order.TradeNo, deposit.TxID, "exactly a day", t0+day)
	require.NoError(t, err)
}
