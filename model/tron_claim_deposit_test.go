package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A claim names a transaction the scanner has already recorded. A txid that
// is not in the ledger (made up, on another chain, or not yet scanned) opens
// no ticket, so it raises no alert and cannot pre-empt an automatic credit.

func TestSubmitTronClaim_RefusesTxidTheScannerHasNotRecorded(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-claim-unknown", 8401, 5_000_401, 1_800_001_200_000)

	_, err := submitTronTopupClaimAt(order.UserID, order.TradeNo, tronTransfer("d", 5_000_401, 1_800_000_100_000).TxID, "probe", 1_800_000_200_000)
	require.ErrorIs(t, err, ErrTronClaimDepositNotFound)

	var tickets int64
	require.NoError(t, DB.Model(&TronTopupTicket{}).Count(&tickets).Error)
	assert.Zero(t, tickets)
}

func TestSubmitTronClaim_LinksTheRecordedDeposit(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-claim-known", 8402, 5_000_402, 1_800_001_200_000)
	deposit, err := RecordUnmatchedTronDeposit(tronTransfer("e", 4_900_000, 1_800_000_100_000))
	require.NoError(t, err)

	ticket, err := submitTronTopupClaimAt(order.UserID, order.TradeNo, deposit.TxID, "exchange deducted fee", 1_800_000_200_000)
	require.NoError(t, err)
	require.NotNil(t, ticket.DepositID)
	assert.Equal(t, deposit.ID, *ticket.DepositID)
	assert.Equal(t, TronTicketStatusOpen, ticket.Status)
}

func TestTolerantSettle_ClaimCannotPreemptAutomaticCredit(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "TOL-preempt", 9180, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	tolOrder(t, "TOL-preempt-attacker", 9181, tolExpectedMicros+1, tolBaseMS-tolTTLMS-3*60*60*1000, tolFaceQuota)
	transfer := tolTransfer(180, tolPaidMicros, tolBaseMS+113_000)

	// Someone saw the broadcast before the scanner did.
	_, err := submitTronTopupClaimAt(9181, "TOL-preempt-attacker", transfer.TxID, "mine", tolBaseMS+120_000)
	require.ErrorIs(t, err, ErrTronClaimDepositNotFound)

	result, err := SettleTronDepositTolerant(transfer, nil, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.Credited)
	assert.Equal(t, tolFaceQuota, tolQuota(t, 9180))
	assert.Equal(t, common.TopUpStatusSuccess, tolTopUpStatus(t, order))
}

func TestTolerantSettle_OpenTicketWithoutDepositStillForcesManualReview(t *testing.T) {
	truncateTables(t)
	order := tolOrder(t, "TOL-legacy-claim", 9182, tolExpectedMicros, tolBaseMS, tolFaceQuota)
	transfer := tolTransfer(182, tolPaidMicros, tolBaseMS+113_000)
	// Tickets opened before claims required a recorded deposit have none.
	require.NoError(t, DB.Create(&TronTopupTicket{UserID: 9182, OrderID: order.ID, ClaimKey: "legacy:9182", TxID: transfer.TxID, Reason: TronTicketReasonUserClaim, Status: TronTicketStatusOpen, CreatedAtMS: tolBaseMS + 120_000, UpdatedAtMS: tolBaseMS + 120_000}).Error)

	requireTolUnmatched(t, transfer, nil, tronTolerantNoteConflictClaim)
	assert.Zero(t, tolQuota(t, 9182))
}

// recordClaimableTronDeposits records unmatched transfers whose txids repeat
// each given character, so a test can claim them.
func recordClaimableTronDeposits(t *testing.T, txChars ...string) {
	t.Helper()
	for _, txChar := range txChars {
		_, err := RecordUnmatchedTronDeposit(tronTransfer(txChar, 1_000_000, 1_800_000_100_000))
		require.NoError(t, err)
	}
}

func TestSubmitTronClaim_RefusedTxidUsesNoClaimAndKeepsTheOpenOne(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-claim-keep", 8403, 5_000_403, 1_800_001_200_000)
	recordClaimableTronDeposits(t, "4", "5", "6")
	open, err := submitTronTopupClaimAt(order.UserID, order.TradeNo, strings.Repeat("4", 64), "real", 1_800_000_200_000)
	require.NoError(t, err)

	for range 5 {
		_, err = submitTronTopupClaimAt(order.UserID, order.TradeNo, strings.Repeat("f", 64), "made up", 1_800_000_300_000)
		require.ErrorIs(t, err, ErrTronClaimDepositNotFound)
	}

	var kept TronTopupTicket
	require.NoError(t, DB.First(&kept, open.ID).Error)
	assert.Equal(t, TronTicketStatusOpen, kept.Status, "无效交易号不能顶掉已有的申诉")
	assert.Equal(t, open.UpdatedAtMS, kept.UpdatedAtMS)
	// 被拒的提交不占这张订单的三次机会。
	for _, txChar := range []string{"5", "6"} {
		_, err = submitTronTopupClaimAt(order.UserID, order.TradeNo, strings.Repeat(txChar, 64), "corrected", 1_800_000_400_000)
		require.NoError(t, err)
	}
}

func TestSubmitTronClaim_LegacyTicketWithoutDepositIsReturnedAsIs(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-claim-legacy", 8404, 5_000_404, 1_800_001_200_000)
	txID := strings.Repeat("9", 64)
	legacy := &TronTopupTicket{UserID: order.UserID, OrderID: order.ID, ClaimKey: fmt.Sprintf("claim:%d:%d:%s", order.UserID, order.ID, txID), TxID: txID, Reason: TronTicketReasonUserClaim, Status: TronTicketStatusOpen, UserNote: "before v34", CreatedAtMS: 1_800_000_150_000, UpdatedAtMS: 1_800_000_150_000}
	require.NoError(t, DB.Create(legacy).Error)

	again, err := submitTronTopupClaimAt(order.UserID, order.TradeNo, txID, "retry", 1_800_000_250_000)
	require.NoError(t, err)
	assert.Equal(t, legacy.ID, again.ID)
	assert.Nil(t, again.DepositID)
	assert.Equal(t, int64(1_800_000_150_000), again.UpdatedAtMS, "幂等返回不改时间")
	assert.Equal(t, "before v34", again.UserNote)
}

func TestSubmitTronClaim_DepositLookupFailureIsNotReportedAsMissing(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-claim-deposit-db", 8405, 5_000_405, 1_800_001_200_000)
	forcedErr := errors.New("forced deposit query failure")
	callbackName := "tron-test-deposit-query-failure"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "tron_deposits" {
			tx.AddError(forcedErr)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Query().Remove(callbackName) })

	_, err := submitTronTopupClaimAt(order.UserID, order.TradeNo, strings.Repeat("8", 64), "db down", 1_800_000_200_000)
	require.ErrorIs(t, err, forcedErr)
	assert.NotErrorIs(t, err, ErrTronClaimDepositNotFound)
}
