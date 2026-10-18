package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// loadTronPaidOffOrders returns, among the plausible orders, the ones settled
// beyond doubt, which no longer block another user's payment: the top-up
// succeeded more than tronTolerantPaidExactBlockMS before this block, the one
// payment of the order's amount on record is the exact payment that credited
// it, and no open ticket names the order or that payment. Anything less
// certain keeps blocking for the full window.
func loadTronPaidOffOrders(tx *gorm.DB, transfer TronTransferRecord, plausible []TronTopupOrder, topUps map[int]TopUp) (map[int64]bool, error) {
	paidOff := make(map[int64]bool)
	orders := tronPaidOrdersPastBlock(transfer, plausible, topUps)
	if len(orders) == 0 {
		return paidOff, nil
	}
	ids := make([]int64, 0, len(orders))
	amounts := make([]int64, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
		amounts = append(amounts, order.ExpectedAmountMicros)
	}
	// A repeated payment of an already paid order is recorded without an order
	// id, so payments are found by amount as well: each order's amount is unique.
	var deposits []TronDeposit
	if err := tx.Where("(order_id IN ? OR amount_micros IN ?)", ids, amounts).Find(&deposits).Error; err != nil {
		return nil, err
	}
	payments := make(map[int64][]TronDeposit, len(orders))
	txIDs := make([]string, 0, len(deposits))
	for _, deposit := range deposits {
		txIDs = append(txIDs, deposit.TxID)
		for _, order := range orders {
			if deposit.AmountMicros == order.ExpectedAmountMicros || (deposit.OrderID != nil && *deposit.OrderID == order.ID) {
				payments[order.ID] = append(payments[order.ID], deposit)
			}
		}
	}
	tickets, err := loadOpenTronTicketsFor(tx, ids, txIDs)
	if err != nil {
		return nil, err
	}
	for _, order := range orders {
		if tronOrderSettledByOneExactPayment(order, payments[order.ID]) && !tronTicketsName(tickets, order.ID, payments[order.ID]) {
			paidOff[order.ID] = true
		}
	}
	return paidOff, nil
}

// tronPaidOrdersPastBlock keeps the successful orders whose credit happened
// more than tronTolerantPaidExactBlockMS before the transfer's block.
func tronPaidOrdersPastBlock(transfer TronTransferRecord, plausible []TronTopupOrder, topUps map[int]TopUp) []*TronTopupOrder {
	var orders []*TronTopupOrder
	for i := range plausible {
		order := &plausible[i]
		topUp, ok := topUps[order.TopUpID]
		if !ok || !tronTopUpMatchesOrder(topUp, order) || topUp.Status != common.TopUpStatusSuccess {
			continue
		}
		if order.CompletedAtMS <= 0 || transfer.BlockTimestampMS <= order.CompletedAtMS+tronTolerantPaidExactBlockMS {
			continue
		}
		orders = append(orders, order)
	}
	return orders
}

func loadOpenTronTicketsFor(tx *gorm.DB, orderIDs []int64, txIDs []string) ([]TronTopupTicket, error) {
	query := tx.Where("status = ?", TronTicketStatusOpen)
	if len(txIDs) > 0 {
		query = query.Where("(order_id IN ? OR tx_id IN ?)", orderIDs, txIDs)
	} else {
		query = query.Where("order_id IN ?", orderIDs)
	}
	var tickets []TronTopupTicket
	if err := query.Find(&tickets).Error; err != nil {
		return nil, err
	}
	return tickets, nil
}

// tronOrderSettledByOneExactPayment reports whether the only payment on record
// for the order is the exact-amount payment the scanner credited it with.
func tronOrderSettledByOneExactPayment(order *TronTopupOrder, payments []TronDeposit) bool {
	if len(payments) != 1 {
		return false
	}
	only := payments[0]
	return only.Status == TronDepositStatusCredited && only.MatchMode == TronMatchModeExact &&
		only.AmountMicros == order.ExpectedAmountMicros && only.OrderID != nil && *only.OrderID == order.ID
}

// tronTicketsName reports whether an open ticket is about the order or about
// one of its payments, such as another user disputing that payment.
func tronTicketsName(tickets []TronTopupTicket, orderID int64, payments []TronDeposit) bool {
	for _, ticket := range tickets {
		if ticket.OrderID == orderID {
			return true
		}
		for _, payment := range payments {
			if payment.TxID == ticket.TxID {
				return true
			}
		}
	}
	return false
}
