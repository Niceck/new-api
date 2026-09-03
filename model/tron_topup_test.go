package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTronReceiveAddress = "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f"
	testTronUSDTContract   = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
)

func createTronLedgerFixture(t *testing.T, tradeNo string, userID int, expectedMicros int64, expiresAtMS int64) (*TopUp, *TronTopupOrder) {
	t.Helper()
	user := &User{Id: userID, Username: "tron-user-" + tradeNo, AffCode: "tron-aff-" + tradeNo, Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId:          userID,
		Amount:          20,
		Money:           float64(expectedMicros) / 1_000_000,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodTron,
		PaymentProvider: PaymentProviderTron,
		CreateTime:      1_800_000_000,
		Status:          common.TopUpStatusPending,
	}
	order := &TronTopupOrder{
		UserID:               userID,
		TradeNo:              tradeNo,
		ReceiveAddress:       testTronReceiveAddress,
		TokenContract:        testTronUSDTContract,
		ExpectedAmountMicros: expectedMicros,
		RateCNYMicros:        7_250_000,
		QuoteUpdatedAtMS:     1_800_000_000_000,
		ExpiresAtMS:          expiresAtMS,
		CreditQuota:          10_000_000,
		CreatedAtMS:          1_800_000_000_000,
	}
	require.NoError(t, CreateTronTopupOrder(topUp, order))
	return topUp, order
}

func tronTransfer(txChar string, amountMicros, blockTimeMS int64) TronTransferRecord {
	return TronTransferRecord{
		TxID:             strings.Repeat(txChar, 64),
		BlockTimestampMS: blockTimeMS,
		FromAddress:      "TJmmqjb1DK9TTZbQXzRQ2AuA94z4gKAPFh",
		ToAddress:        testTronReceiveAddress,
		TokenContract:    testTronUSDTContract,
		AmountMicros:     amountMicros,
		ObservedAtMS:     blockTimeMS + 60_000,
	}
}

func TestCreateTronTopupOrder_CreatesGenericAndTronRowsAtomically(t *testing.T) {
	truncateTables(t)
	topUp, order := createTronLedgerFixture(t, "TRON-atomic", 8101, 2_750_123, 1_800_001_200_000)

	assert.NotZero(t, topUp.Id)
	assert.NotZero(t, order.ID)
	assert.Equal(t, topUp.Id, order.TopUpID)

	var topUpCount int64
	var orderCount int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", topUp.TradeNo).Count(&topUpCount).Error)
	require.NoError(t, DB.Model(&TronTopupOrder{}).Where("top_up_id = ?", topUp.Id).Count(&orderCount).Error)
	assert.Equal(t, int64(1), topUpCount)
	assert.Equal(t, int64(1), orderCount)
}

func TestCreateTronTopupOrder_ExpectedAmountIsGloballyUnique(t *testing.T) {
	truncateTables(t)
	createTronLedgerFixture(t, "TRON-unique-1", 8102, 2_750_124, 1_800_001_200_000)

	user := &User{Id: 8103, Username: "tron-unique-second", AffCode: "tron-aff-unique-second", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{UserId: user.Id, TradeNo: "TRON-unique-2", PaymentMethod: PaymentMethodTron, PaymentProvider: PaymentProviderTron, Status: common.TopUpStatusPending}
	order := &TronTopupOrder{UserID: user.Id, TradeNo: topUp.TradeNo, ReceiveAddress: testTronReceiveAddress, TokenContract: testTronUSDTContract, ExpectedAmountMicros: 2_750_124, RateCNYMicros: 7_250_000, CreditQuota: 1, ExpiresAtMS: 1_800_001_200_000}

	require.Error(t, CreateTronTopupOrder(topUp, order))
	assert.Zero(t, topUp.Id)
	assert.Nil(t, GetTopUpByTradeNo(topUp.TradeNo))
}

func TestSettleTronDeposit_CreditsExactlyOnceOnReplay(t *testing.T) {
	truncateTables(t)
	topUp, order := createTronLedgerFixture(t, "TRON-replay", 8104, 2_750_125, 1_800_001_200_000)
	transfer := tronTransfer("a", order.ExpectedAmountMicros, order.ExpiresAtMS)

	first, err := SettleTronDeposit(transfer, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, first.Credited)
	assert.False(t, first.AlreadyProcessed)

	second, err := SettleTronDeposit(transfer, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, second.Credited)
	assert.True(t, second.AlreadyProcessed)

	var user User
	require.NoError(t, DB.First(&user, topUp.UserId).Error)
	assert.Equal(t, 100+order.CreditQuota, user.Quota)
	assert.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo(topUp.TradeNo).Status)

	var deposits int64
	require.NoError(t, DB.Model(&TronDeposit{}).Where("tx_id = ?", transfer.TxID).Count(&deposits).Error)
	assert.Equal(t, int64(1), deposits)
}

func TestSettleTronDeposit_RejectsWrongProviderAmountContractAddressAndLateBlock(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*TopUp, *TronTopupOrder, *TronTransferRecord)
	}{
		{name: "wrong provider", mutate: func(topUp *TopUp, _ *TronTopupOrder, _ *TronTransferRecord) {
			require.NoError(t, DB.Model(topUp).Update("payment_provider", PaymentProviderEpay).Error)
		}},
		{name: "wrong amount", mutate: func(_ *TopUp, _ *TronTopupOrder, transfer *TronTransferRecord) { transfer.AmountMicros++ }},
		{name: "wrong contract", mutate: func(_ *TopUp, _ *TronTopupOrder, transfer *TronTransferRecord) { transfer.TokenContract = "wrong" }},
		{name: "wrong address", mutate: func(_ *TopUp, _ *TronTopupOrder, transfer *TronTransferRecord) { transfer.ToAddress = "wrong" }},
		{name: "one millisecond before order", mutate: func(_ *TopUp, order *TronTopupOrder, transfer *TronTransferRecord) {
			transfer.BlockTimestampMS = order.CreatedAtMS - 1
		}},
		{name: "late by one millisecond", mutate: func(_ *TopUp, order *TronTopupOrder, transfer *TronTransferRecord) {
			transfer.BlockTimestampMS = order.ExpiresAtMS + 1
		}},
	}

	for index, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			topUp, order := createTronLedgerFixture(t, "TRON-reject-"+string(rune('a'+index)), 8200+index, 3_100_100+int64(index), 1_800_001_200_000)
			txChars := []string{"b", "c", "d", "e", "f", "a"}
			transfer := tronTransfer(txChars[index], order.ExpectedAmountMicros, order.ExpiresAtMS)
			tc.mutate(topUp, order, &transfer)

			result, err := SettleTronDeposit(transfer, "tron-scanner")
			if tc.name == "late by one millisecond" {
				require.NoError(t, err)
				assert.True(t, result.NeedsReview)
			} else {
				require.Error(t, err)
			}
			assert.Equal(t, 100, getUserQuotaForPaymentGuardTest(t, topUp.UserId))
			assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(topUp.TradeNo).Status)
		})
	}
}

func TestRecordUnmatchedTronDeposit_IsIdempotent(t *testing.T) {
	truncateTables(t)
	transfer := tronTransfer("f", 9_999_999, 1_800_000_050_000)

	first, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	second, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, TronDepositStatusUnmatched, second.Status)
}

func TestSubmitTronClaim_PreventsCrossUserTxidClaim(t *testing.T) {
	truncateTables(t)
	_, firstOrder := createTronLedgerFixture(t, "TRON-claim-1", 8301, 5_000_101, 1_800_001_200_000)
	_, secondOrder := createTronLedgerFixture(t, "TRON-claim-2", 8302, 5_000_102, 1_800_001_200_000)
	transfer := tronTransfer("c", 5_000_999, 1_800_000_100_000)
	_, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)

	ticket, err := SubmitTronTopupClaim(firstOrder.UserID, firstOrder.TradeNo, transfer.TxID, "exchange deducted fee")
	require.NoError(t, err)
	assert.Equal(t, firstOrder.UserID, ticket.UserID)

	competing, err := SubmitTronTopupClaim(secondOrder.UserID, secondOrder.TradeNo, transfer.TxID, "mine")
	require.NoError(t, err)
	assert.NotEqual(t, ticket.ID, competing.ID)
	assert.Equal(t, ticket.TxID, competing.TxID)
}

func TestResolveTronTicket_UsesVerifiedDepositAndCannotReuseTxid(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-resolve", 8401, 6_000_000, 1_800_001_200_000)
	transfer := tronTransfer("d", 6_600_000, 1_800_000_100_000)
	_, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	ticket, err := SubmitTronTopupClaim(order.UserID, order.TradeNo, transfer.TxID, "wrong amount")
	require.NoError(t, err)
	_, otherOrder := createTronLedgerFixture(t, "TRON-resolve-other", 8402, 6_000_102, 1_800_001_200_000)
	competing, err := SubmitTronTopupClaim(otherOrder.UserID, otherOrder.TradeNo, transfer.TxID, "reuse")
	require.NoError(t, err)

	require.NoError(t, ResolveTronTopupTicket(ticket.ID, order.ID, transfer, 1, "verified ownership evidence"))
	assert.Equal(t, 100+11_000_000, getUserQuotaForPaymentGuardTest(t, order.UserID))

	var resolved TronTopupTicket
	require.NoError(t, DB.First(&resolved, ticket.ID).Error)
	assert.Equal(t, TronTicketStatusResolved, resolved.Status)
	assert.Equal(t, 1, resolved.ResolverID)
	assert.Equal(t, order.ID, resolved.ResolvedOrderID)
	assert.Equal(t, order.UserID, resolved.CreditedUserID)
	require.ErrorIs(t, ResolveTronTopupTicket(ticket.ID, otherOrder.ID, transfer, 1, "mismatched retry"), ErrTronTransferMismatch)

	require.ErrorIs(t, ResolveTronTopupTicket(competing.ID, otherOrder.ID, transfer, 1, "wrong owner"), ErrTronTicketNotOpen)
}

func TestResolveTronTicket_AllowsCompetingClaimsButCreditsSelectedRightfulOrder(t *testing.T) {
	truncateTables(t)
	_, rightfulOrder := createTronLedgerFixture(t, "TRON-rightful", 8501, 7_000_000, 1_800_001_200_000)
	_, attackerOrder := createTronLedgerFixture(t, "TRON-attacker", 8502, 7_000_001, 1_800_001_200_000)
	transfer := tronTransfer("e", rightfulOrder.ExpectedAmountMicros, rightfulOrder.CreatedAtMS+1)
	_, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	attackerTicket, err := SubmitTronTopupClaim(attackerOrder.UserID, attackerOrder.TradeNo, transfer.TxID, "attacker first")
	require.NoError(t, err)
	_, err = SubmitTronTopupClaim(rightfulOrder.UserID, rightfulOrder.TradeNo, transfer.TxID, "rightful claimant")
	require.NoError(t, err)

	require.NoError(t, ResolveTronTopupTicket(attackerTicket.ID, rightfulOrder.ID, transfer, 1, "matched external ownership proof"))
	assert.Equal(t, 100, getUserQuotaForPaymentGuardTest(t, attackerOrder.UserID))
	assert.Equal(t, 100+rightfulOrder.CreditQuota, getUserQuotaForPaymentGuardTest(t, rightfulOrder.UserID))
}

func TestSettleTronDeposit_NormalizesTxIDAndRejectsConflictingReplay(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-normalize", 8601, 8_000_001, 1_800_001_200_000)
	transfer := tronTransfer("A", order.ExpectedAmountMicros, order.CreatedAtMS)
	transfer.TxID = "  " + transfer.TxID + "  "

	_, err := SettleTronDeposit(transfer, "tron-scanner")
	require.NoError(t, err)
	lowercase := transfer
	lowercase.TxID = strings.Repeat("a", 64)
	result, err := SettleTronDeposit(lowercase, "tron-scanner")
	require.NoError(t, err)
	assert.True(t, result.AlreadyProcessed)

	conflict := lowercase
	conflict.FromAddress = "different"
	_, err = SettleTronDeposit(conflict, "tron-scanner")
	assert.ErrorIs(t, err, ErrTronTransferMismatch)
}

func TestResolveTronTicket_RejectsAmountOutsideReviewPolicy(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-review-bound", 8701, 9_000_000, 1_800_001_200_000)
	transfer := tronTransfer("f", 8_099_999, order.CreatedAtMS+1)
	_, err := RecordUnmatchedTronDeposit(transfer)
	require.NoError(t, err)
	ticket, err := SubmitTronTopupClaim(order.UserID, order.TradeNo, transfer.TxID, "too little")
	require.NoError(t, err)

	require.Error(t, ResolveTronTopupTicket(ticket.ID, order.ID, transfer, 1, "ownership proof"))
	assert.Equal(t, 100, getUserQuotaForPaymentGuardTest(t, order.UserID))
}

func TestSubmitTronClaim_RejectsNonPendingOrInconsistentOrder(t *testing.T) {
	truncateTables(t)
	topUp, order := createTronLedgerFixture(t, "TRON-claim-status", 8801, 10_000_001, 1_800_001_200_000)
	require.NoError(t, DB.Model(topUp).Update("status", common.TopUpStatusFailed).Error)

	_, err := SubmitTronTopupClaim(order.UserID, order.TradeNo, strings.Repeat("1", 64), "failed order")
	assert.ErrorIs(t, err, ErrTopUpStatusInvalid)
}

func TestRecordFailedTronSettlement_RejectsPreOrderTransferWithoutTicket(t *testing.T) {
	truncateTables(t)
	_, order := createTronLedgerFixture(t, "TRON-failed-preorder", 8901, 11_000_001, 1_800_001_200_000)
	transfer := tronTransfer("2", order.ExpectedAmountMicros, order.CreatedAtMS-1)

	needsReview, err := RecordFailedTronSettlement(transfer)
	require.NoError(t, err)
	assert.False(t, needsReview)
	var depositCount int64
	var ticketCount int64
	require.NoError(t, DB.Model(&TronDeposit{}).Count(&depositCount).Error)
	require.NoError(t, DB.Model(&TronTopupTicket{}).Count(&ticketCount).Error)
	assert.Equal(t, int64(1), depositCount)
	assert.Zero(t, ticketCount)
}
