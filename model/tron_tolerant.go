package model

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

// Tolerant matching settles a payment whose amount is not the order's exact
// expected amount. The single shared receive address means ownership is
// inferred, never proven, so every rule below errs toward manual review.
const (
	TronMatchModeExact    = "exact"
	TronMatchModeTolerant = "tolerant"
	TronMatchModeManual   = "manual"

	tronTolerantRuleVersion = "v1"
	// An order must exist this long before the block, so nobody can create a
	// matching order after seeing someone else's broadcast transaction.
	tronTolerantMinLeadMS int64 = 60 * 1000
	// Another user's order keeps blocking this long after it expires: the money
	// could still be theirs, paid late. Outside the window it is an accepted risk.
	tronTolerantBlockerGraceMS int64 = 2 * 60 * 60 * 1000
	// Only a user's first few orders in a rolling day may settle by tolerance,
	// so a standing order cannot wait around for other people's mispayments.
	tronTolerantThrottleWindowMS int64 = 24 * 60 * 60 * 1000
	tronTolerantEligibleOrders   int64 = 3

	tronReviewReasonCreditFailed = "credit_failed"

	tronTolerantNoteNoCandidate       = "tolerant:no_candidate"
	tronTolerantNoteTooEarly          = "tolerant:too_early"
	tronTolerantNoteBlockedOtherUser  = "tolerant:blocked_other_user"
	tronTolerantNoteSameUserAmbiguous = "tolerant:same_user_ambiguous"
	tronTolerantNoteThrottled         = "tolerant:throttled"
	tronTolerantNoteConflictClaim     = "tolerant:conflict_claim"
	tronTolerantNoteConflictDeposit   = "tolerant:conflict_deposit"
	tronTolerantNoteExactAmountOrder  = "tolerant:exact_amount_order"
)

type tronTolerantSelection struct {
	order *TronTopupOrder
	topUp *TopUp
	quota int
	note  string
}

// tronAmountWithinPolicy reports whether actual is within 90%–110% of
// expected, both bounds inclusive, without floating point or overflow.
func tronAmountWithinPolicy(expectedMicros int64, actualMicros int64) bool {
	if expectedMicros <= 0 || actualMicros <= 0 {
		return false
	}
	actual := new(big.Int).Mul(big.NewInt(actualMicros), big.NewInt(100))
	low := new(big.Int).Mul(big.NewInt(expectedMicros), big.NewInt(tronClaimMinPercent))
	high := new(big.Int).Mul(big.NewInt(expectedMicros), big.NewInt(tronClaimMaxPercent))
	return actual.Cmp(low) >= 0 && actual.Cmp(high) <= 0
}

// tronSettlementQuota is the one formula for automatic and manual settlement:
// paying at least the expected amount credits the order's face value (an
// overpayment earns nothing extra); paying less credits the same share of the
// face value, rounded down. The order's locked price is never recomputed.
func tronSettlementQuota(expectedMicros int64, actualMicros int64, creditQuota int) (int, error) {
	if expectedMicros <= 0 || actualMicros <= 0 || creditQuota <= 0 || creditQuota > common.MaxQuota {
		return 0, errors.New("TRON claim amounts are out of range")
	}
	if !tronAmountWithinPolicy(expectedMicros, actualMicros) {
		return 0, errors.New("TRON claim amount is outside review policy")
	}
	if actualMicros >= expectedMicros {
		return creditQuota, nil
	}
	quota := new(big.Int).Mul(big.NewInt(int64(creditQuota)), big.NewInt(actualMicros))
	quota.Quo(quota, big.NewInt(expectedMicros))
	if !quota.IsInt64() || quota.Int64() <= 0 || quota.Int64() > int64(common.MaxQuota) {
		return 0, errors.New("TRON claim quota is out of range")
	}
	return int(quota.Int64()), nil
}

// tronPlausibleExpectedBounds returns the expected amounts for which actual
// could be within policy; callers still confirm with tronAmountWithinPolicy.
func tronPlausibleExpectedBounds(actualMicros int64) (int64, int64, bool) {
	if actualMicros <= 0 || actualMicros > math.MaxInt64/100-tronClaimMaxPercent {
		return 0, 0, false
	}
	low := (actualMicros*100 + tronClaimMaxPercent - 1) / tronClaimMaxPercent
	high := actualMicros * 100 / tronClaimMinPercent
	return low, high, true
}

func formatTronMicros(amountMicros int64) string {
	return fmt.Sprintf("%d.%06d", amountMicros/1_000_000, amountMicros%1_000_000)
}

func newTronDepositFromTransfer(transfer TronTransferRecord, status string) TronDeposit {
	return TronDeposit{
		TxID:             transfer.TxID,
		BlockTimestampMS: transfer.BlockTimestampMS,
		FromAddress:      transfer.FromAddress,
		ToAddress:        transfer.ToAddress,
		TokenContract:    transfer.TokenContract,
		AmountMicros:     transfer.AmountMicros,
		Status:           status,
		ObservedAtMS:     transfer.ObservedAtMS,
		SourceAddresses:  transfer.SourceAddresses,
	}
}

// processedTronDepositResult returns the stored decision for a transaction
// that was already classified, or nil when it has never been recorded.
func processedTronDepositResult(tx *gorm.DB, transfer TronTransferRecord) (*TronSettlementResult, error) {
	existing := TronDeposit{}
	err := tx.Where("tx_id = ?", transfer.TxID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !sameTronTransfer(&existing, transfer) {
		return nil, ErrTronTransferMismatch
	}
	if existing.Status != TronDepositStatusCredited && existing.Status != TronDepositStatusReview && existing.Status != TronDepositStatusUnmatched {
		return nil, errors.New("stored TRON deposit has invalid status")
	}
	result := TronSettlementResult{
		DepositID:        existing.ID,
		Credited:         existing.Status == TronDepositStatusCredited,
		NeedsReview:      existing.Status == TronDepositStatusReview,
		Unmatched:        existing.Status == TronDepositStatusUnmatched,
		MatchNote:        existing.MatchNote,
		AlreadyProcessed: true,
	}
	if existing.OrderID != nil {
		result.OrderID = *existing.OrderID
	}
	return &result, nil
}

// SettleTronDepositTolerant classifies a confirmed transfer that matched no
// order's exact amount. It decides once: the first committed classification is
// final, so a transfer recorded as unmatched is never settled automatically
// later, whatever changes around it. peers are the other inexact transfers of
// the same scan batch that have not been recorded yet.
func SettleTronDepositTolerant(transfer TronTransferRecord, peers []TronTransferRecord, callerIP string) (TronSettlementResult, error) {
	normalized, err := normalizeTronTransferRecord(transfer)
	if err != nil {
		return TronSettlementResult{}, err
	}
	transfer = normalized
	if transfer.SourceAddresses != "" {
		return TronSettlementResult{}, errors.New("multi-source TRON transfers are not settled by tolerance")
	}

	result := TronSettlementResult{}
	var credit *tronTolerantCredit
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTronSerialTx(tx); err != nil {
			return err
		}
		processed, err := processedTronDepositResult(tx, transfer)
		if err != nil {
			return err
		}
		if processed != nil {
			result = *processed
			return nil
		}
		selection, err := selectTronTolerantOrder(tx, transfer, peers)
		if err != nil {
			return err
		}
		if selection.order == nil {
			result, err = recordTronTolerantUnmatchedTx(tx, transfer, selection.note)
			return err
		}
		result, credit, err = settleTronTolerantSelectionTx(tx, transfer, selection)
		return err
	})
	if err != nil {
		return TronSettlementResult{}, err
	}
	if credit != nil {
		refreshTronQuotaCache(credit.userID, credit.quota)
		RecordTopupLog(credit.userID, credit.logContent, callerIP, PaymentMethodTron, PaymentProviderTron)
	}
	return result, nil
}

// tronTolerantCredit carries what must happen after the transaction commits.
type tronTolerantCredit struct {
	userID     int
	quota      int
	logContent string
}

func recordTronTolerantUnmatchedTx(tx *gorm.DB, transfer TronTransferRecord, note string) (TronSettlementResult, error) {
	deposit := newTronDepositFromTransfer(transfer, TronDepositStatusUnmatched)
	deposit.MatchRule = tronTolerantRuleVersion
	deposit.MatchNote = note
	if err := tx.Create(&deposit).Error; err != nil {
		return TronSettlementResult{}, err
	}
	return TronSettlementResult{DepositID: deposit.ID, Unmatched: true, MatchNote: note}, nil
}

// settleTronTolerantSelectionTx credits the selected order, or, when only the
// wallet cap stands in the way, keeps the funds for review bound to that order.
func settleTronTolerantSelectionTx(tx *gorm.DB, transfer TronTransferRecord, selection tronTolerantSelection) (TronSettlementResult, *tronTolerantCredit, error) {
	order := selection.order
	orderID := order.ID
	deposit := newTronDepositFromTransfer(transfer, TronDepositStatusReview)
	deposit.OrderID = &orderID
	deposit.MatchMode = TronMatchModeTolerant
	deposit.MatchRule = tronTolerantRuleVersion
	if err := tx.Create(&deposit).Error; err != nil {
		return TronSettlementResult{}, nil, err
	}
	result := TronSettlementResult{DepositID: deposit.ID, OrderID: order.ID}

	credited, _, err := creditTronOrderTx(tx, selection.topUp, order, &deposit, selection.quota, transfer.ObservedAtMS)
	if errors.Is(err, ErrTronCreditReviewRequired) {
		// The wallet cap is checked before anything is written, so the balance
		// is untouched. Keep the chosen order with the funds: a later lookup by
		// amount could not find it again.
		deposit.ReviewReason = tronReviewReasonCreditFailed
		if err := tx.Save(&deposit).Error; err != nil {
			return TronSettlementResult{}, nil, err
		}
		ticket := TronTopupTicket{UserID: order.UserID, OrderID: order.ID, DepositID: &deposit.ID, ClaimKey: "auto:credit_failed:" + transfer.TxID, TxID: transfer.TxID, Reason: TronTicketReasonCreditFailed, Status: TronTicketStatusOpen, CreatedAtMS: transfer.ObservedAtMS, UpdatedAtMS: transfer.ObservedAtMS}
		if err := tx.Create(&ticket).Error; err != nil {
			return TronSettlementResult{}, nil, err
		}
		result.NeedsReview = true
		return result, nil, nil
	}
	if err != nil {
		return TronSettlementResult{}, nil, err
	}
	result.Credited = true
	logContent := fmt.Sprintf("TRON USDT 自动入账（金额在识别范围内），订单: %s，txid: %s，实到: %s USDT，应付: %s USDT，入账额度: %v", order.TradeNo, transfer.TxID, formatTronMicros(transfer.AmountMicros), formatTronMicros(order.ExpectedAmountMicros), logger.FormatQuota(credited))
	return result, &tronTolerantCredit{userID: order.UserID, quota: credited, logContent: logContent}, nil
}

// selectTronTolerantOrder applies the attribution rules. It returns an order
// only when exactly one order can take the transfer and nothing else on record
// suggests the money could belong elsewhere; otherwise a note says why not.
func selectTronTolerantOrder(tx *gorm.DB, transfer TronTransferRecord, peers []TronTransferRecord) (tronTolerantSelection, error) {
	note, err := tronTolerantPrecheck(tx, transfer)
	if err != nil || note != "" {
		return tronTolerantSelection{note: note}, err
	}
	plausible, topUps, err := loadTronPlausibleOrders(tx, transfer)
	if err != nil {
		return tronTolerantSelection{}, err
	}
	candidate, quota, note := pickTronTolerantCandidate(transfer, plausible, topUps)
	if candidate == nil {
		return tronTolerantSelection{note: note}, nil
	}
	note, err = tronTolerantCandidateBlocked(tx, transfer, candidate, peers)
	if err != nil || note != "" {
		return tronTolerantSelection{note: note}, err
	}
	return lockTronTolerantCandidate(tx, candidate, quota)
}

// tronTolerantPrecheck rules tolerance out before any order is considered: an
// order with this exact amount belongs to the exact path and to review, and an
// open claim on the transaction means a person already asserts ownership.
func tronTolerantPrecheck(tx *gorm.DB, transfer TronTransferRecord) (string, error) {
	var exactOrders int64
	if err := tx.Model(&TronTopupOrder{}).Where("expected_amount_micros = ?", transfer.AmountMicros).Count(&exactOrders).Error; err != nil {
		return "", err
	}
	if exactOrders > 0 {
		return tronTolerantNoteExactAmountOrder, nil
	}
	var openClaims int64
	if err := tx.Model(&TronTopupTicket{}).Where("tx_id = ? AND status = ?", transfer.TxID, TronTicketStatusOpen).Count(&openClaims).Error; err != nil {
		return "", err
	}
	if openClaims > 0 {
		return tronTolerantNoteConflictClaim, nil
	}
	return "", nil
}

// pickTronTolerantCandidate chooses among the plausible orders without
// touching the database. An order is live when it is pending, was paid before
// it expired and existed long enough before the block. Exactly one live order
// may remain, no other user may have any plausible order, and the same user's
// other plausible orders must all settle to the identical credit.
func pickTronTolerantCandidate(transfer TronTransferRecord, plausible []TronTopupOrder, topUps map[int]TopUp) (*TronTopupOrder, int, string) {
	var live []*TronTopupOrder
	tooEarly := false
	for i := range plausible {
		order := &plausible[i]
		topUp, ok := topUps[order.TopUpID]
		pending := ok && tronTopUpMatchesOrder(topUp, order) && topUp.Status == common.TopUpStatusPending
		if !pending || transfer.BlockTimestampMS > order.ExpiresAtMS {
			continue
		}
		if order.CreatedAtMS+tronTolerantMinLeadMS > transfer.BlockTimestampMS {
			tooEarly = true
			continue
		}
		live = append(live, order)
	}
	if len(live) == 0 {
		if tooEarly {
			return nil, 0, tronTolerantNoteTooEarly
		}
		return nil, 0, tronTolerantNoteNoCandidate
	}
	candidate := live[0]
	for i := range plausible {
		if plausible[i].UserID != candidate.UserID {
			return nil, 0, tronTolerantNoteBlockedOtherUser
		}
	}
	if len(live) > 1 {
		return nil, 0, tronTolerantNoteSameUserAmbiguous
	}
	quota, err := tronSettlementQuota(candidate.ExpectedAmountMicros, transfer.AmountMicros, candidate.CreditQuota)
	if err != nil {
		return nil, 0, tronTolerantNoteNoCandidate
	}
	for i := range plausible {
		other := &plausible[i]
		if other.ID == candidate.ID {
			continue
		}
		otherQuota, err := tronSettlementQuota(other.ExpectedAmountMicros, transfer.AmountMicros, other.CreditQuota)
		if err != nil || other.CreditQuota != candidate.CreditQuota || otherQuota != quota {
			return nil, 0, tronTolerantNoteSameUserAmbiguous
		}
	}
	return candidate, quota, ""
}

// tronTolerantCandidateBlocked applies the checks that need the ledger: the
// user's rolling-day eligibility and rival payments for the same order.
func tronTolerantCandidateBlocked(tx *gorm.DB, transfer TronTransferRecord, candidate *TronTopupOrder, peers []TronTransferRecord) (string, error) {
	var recentOrders int64
	if err := tx.Model(&TronTopupOrder{}).
		Where("user_id = ? AND created_at_ms >= ? AND created_at_ms <= ?", candidate.UserID, transfer.BlockTimestampMS-tronTolerantThrottleWindowMS, transfer.BlockTimestampMS).
		Count(&recentOrders).Error; err != nil {
		return "", err
	}
	if recentOrders > tronTolerantEligibleOrders {
		return tronTolerantNoteThrottled, nil
	}
	conflict, err := tronTolerantDepositConflict(tx, transfer, candidate, peers)
	if err != nil {
		return "", err
	}
	if conflict {
		return tronTolerantNoteConflictDeposit, nil
	}
	return "", nil
}

// lockTronTolerantCandidate re-reads the chosen order and its top-up under row
// locks and confirms nothing about them changed since they were selected.
func lockTronTolerantCandidate(tx *gorm.DB, candidate *TronTopupOrder, quota int) (tronTolerantSelection, error) {
	order := TronTopupOrder{}
	if err := lockForUpdate(tx).Where("id = ?", candidate.ID).First(&order).Error; err != nil {
		return tronTolerantSelection{}, err
	}
	topUp := TopUp{}
	if err := lockForUpdate(tx).Where("id = ?", order.TopUpID).First(&topUp).Error; err != nil {
		return tronTolerantSelection{}, err
	}
	if !tronTopUpMatchesOrder(topUp, &order) || topUp.Status != common.TopUpStatusPending || order.ExpectedAmountMicros != candidate.ExpectedAmountMicros || order.CreditQuota != candidate.CreditQuota {
		return tronTolerantSelection{note: tronTolerantNoteNoCandidate}, nil
	}
	return tronTolerantSelection{order: &order, topUp: &topUp, quota: quota}, nil
}

func tronTopUpMatchesOrder(topUp TopUp, order *TronTopupOrder) bool {
	return topUp.PaymentProvider == PaymentProviderTron && topUp.PaymentMethod == PaymentMethodTron && topUp.UserId == order.UserID && topUp.TradeNo == order.TradeNo
}

// loadTronPlausibleOrders returns every order, whatever its state, that this
// transfer could plausibly have been meant for: same address and token, the
// block no later than the blocker grace after expiry, and the amount within
// policy. A paid or closed order stays in the set, because its owner may have
// paid a second time by mistake.
func loadTronPlausibleOrders(tx *gorm.DB, transfer TronTransferRecord) ([]TronTopupOrder, map[int]TopUp, error) {
	low, high, ok := tronPlausibleExpectedBounds(transfer.AmountMicros)
	if !ok {
		return nil, nil, nil
	}
	var candidates []TronTopupOrder
	if err := tx.
		Where("receive_address = ? AND token_contract = ?", transfer.ToAddress, transfer.TokenContract).
		Where("created_at_ms <= ? AND expires_at_ms >= ?", transfer.BlockTimestampMS, transfer.BlockTimestampMS-tronTolerantBlockerGraceMS).
		Where("expected_amount_micros >= ? AND expected_amount_micros <= ?", low, high).
		Order("id ASC").
		Find(&candidates).Error; err != nil {
		return nil, nil, err
	}
	plausible := make([]TronTopupOrder, 0, len(candidates))
	topUpIDs := make([]int, 0, len(candidates))
	for _, order := range candidates {
		if !tronAmountWithinPolicy(order.ExpectedAmountMicros, transfer.AmountMicros) {
			continue
		}
		plausible = append(plausible, order)
		topUpIDs = append(topUpIDs, order.TopUpID)
	}
	topUps := make(map[int]TopUp, len(plausible))
	if len(topUpIDs) == 0 {
		return plausible, topUps, nil
	}
	var rows []TopUp
	if err := tx.Where("id IN ?", topUpIDs).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		topUps[row.Id] = row
	}
	return plausible, topUps, nil
}

// tronTolerantDepositConflict reports whether another inexact payment, already
// on record and not attributed elsewhere or still waiting in this scan batch,
// could also be for the candidate order. Then neither is settled automatically:
// the order of traversal must not decide who gets the order.
func tronTolerantDepositConflict(tx *gorm.DB, transfer TronTransferRecord, candidate *TronTopupOrder, peers []TronTransferRecord) (bool, error) {
	fromMS := candidate.CreatedAtMS
	toMS := candidate.ExpiresAtMS + tronTolerantBlockerGraceMS
	var rivals []TronDeposit
	if err := tx.
		Where("tx_id <> ? AND to_address = ? AND token_contract = ?", transfer.TxID, transfer.ToAddress, transfer.TokenContract).
		Where("status IN ?", []string{TronDepositStatusUnmatched, TronDepositStatusReview}).
		Where("order_id IS NULL OR order_id = ?", candidate.ID).
		Where("block_timestamp_ms >= ? AND block_timestamp_ms <= ?", fromMS, toMS).
		Find(&rivals).Error; err != nil {
		return false, err
	}
	for _, rival := range rivals {
		if tronAmountWithinPolicy(candidate.ExpectedAmountMicros, rival.AmountMicros) {
			return true, nil
		}
	}
	for _, peer := range peers {
		if strings.ToLower(strings.TrimSpace(peer.TxID)) == transfer.TxID || peer.ToAddress != transfer.ToAddress || peer.TokenContract != transfer.TokenContract {
			continue
		}
		if peer.BlockTimestampMS < fromMS || peer.BlockTimestampMS > toMS {
			continue
		}
		if tronAmountWithinPolicy(candidate.ExpectedAmountMicros, peer.AmountMicros) {
			return true, nil
		}
	}
	return false, nil
}
