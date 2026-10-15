package model

import "gorm.io/gorm"

// Every claim or dispute a user opens raises an operator alert, and orders
// cost nothing to create, so the per-order limits alone do not bound them.
const (
	tronUserTicketBudget         int64 = 3
	tronUserTicketBudgetWindowMS int64 = 24 * 60 * 60 * 1000
)

// checkTronUserTicketBudgetTx refuses a new user-opened ticket once the user
// has opened the budget within the rolling window, across all their orders.
// Tickets the system opens (late payments, failed credits) are not the user's
// doing and do not count; returning an existing ticket opens nothing. Callers
// hold the ledger serial lock, so concurrent submissions see each other.
func checkTronUserTicketBudgetTx(tx *gorm.DB, userID int, nowMS int64) error {
	var opened int64
	if err := tx.Model(&TronTopupTicket{}).
		Where("user_id = ? AND reason IN ? AND created_at_ms > ?", userID, []string{TronTicketReasonUserClaim, TronTicketReasonDispute}, nowMS-tronUserTicketBudgetWindowMS).
		Count(&opened).Error; err != nil {
		return err
	}
	if opened >= tronUserTicketBudget {
		return ErrTronClaimUserBudgetExceeded
	}
	return nil
}
