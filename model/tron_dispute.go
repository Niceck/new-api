package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// openTronDisputeTx records that a user contests a transaction already
// credited to another user's order. It moves no money, never re-credits the
// transaction and tells the claimant nothing about the credited account; an
// administrator investigates and closes the ticket.
//
// Only a claimant whose own order could plausibly have received the payment
// may dispute it; anyone else gets the same answer as before.
func openTronDisputeTx(tx *gorm.DB, userID int, order *TronTopupOrder, deposit *TronDeposit, note string, nowMS int64) (*TronTopupTicket, error) {
	if deposit.OrderID == nil {
		return nil, ErrTronTxAlreadyClaimed
	}
	credited := TronTopupOrder{}
	err := tx.Where("id = ?", *deposit.OrderID).First(&credited).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTronTxAlreadyClaimed
	}
	if err != nil {
		return nil, err
	}
	plausible := deposit.ToAddress == order.ReceiveAddress && deposit.TokenContract == order.TokenContract &&
		deposit.BlockTimestampMS >= order.CreatedAtMS && deposit.BlockTimestampMS <= order.ExpiresAtMS+TronClaimGracePeriodMS &&
		tronAmountWithinPolicy(order.ExpectedAmountMicros, deposit.AmountMicros)
	if credited.UserID == userID || !plausible {
		return nil, ErrTronTxAlreadyClaimed
	}

	claimKey := fmt.Sprintf("dispute:%d:%d:%s", userID, order.ID, deposit.TxID)
	existing := TronTopupTicket{}
	err = tx.Where("claim_key = ?", claimKey).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var disputeCount int64
	if err := tx.Model(&TronTopupTicket{}).Where("order_id = ? AND reason = ?", order.ID, TronTicketReasonDispute).Count(&disputeCount).Error; err != nil {
		return nil, err
	}
	if disputeCount >= tronMaxClaimsPerOrder {
		return nil, ErrTronClaimLimitReached
	}
	ticket := TronTopupTicket{UserID: userID, OrderID: order.ID, DepositID: &deposit.ID, ClaimKey: claimKey, TxID: deposit.TxID, Reason: TronTicketReasonDispute, Status: TronTicketStatusOpen, UserNote: note, CreatedAtMS: nowMS, UpdatedAtMS: nowMS}
	if err := tx.Create(&ticket).Error; err != nil {
		return nil, err
	}
	return &ticket, nil
}
