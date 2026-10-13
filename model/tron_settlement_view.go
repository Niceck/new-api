package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TronOrderSettlement is what a TRON order was actually settled with. The
// order keeps its quoted face value and expected amount; these are the facts
// of the payment, which differ whenever the amount was not exact.
type TronOrderSettlement struct {
	ReceivedMicros int64
	CreditedQuota  int
	MatchMode      string
}

// GetTronOrderSettlement returns the credited deposit of an order, or nil
// while nothing has been credited. Deposits held for review are not settlement.
func GetTronOrderSettlement(orderID int64) (*TronOrderSettlement, error) {
	deposit := TronDeposit{}
	err := DB.Where("order_id = ? AND status = ?", orderID, TronDepositStatusCredited).Order("id ASC").First(&deposit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &TronOrderSettlement{ReceivedMicros: deposit.AmountMicros, CreditedQuota: deposit.CreditedQuota, MatchMode: deposit.MatchMode}, nil
}

// TronTolerantAmountRange returns the smallest and largest amounts, inclusive,
// that are within policy for an order expecting expectedMicros.
func TronTolerantAmountRange(expectedMicros int64) (int64, int64) {
	if expectedMicros <= 0 || expectedMicros > (1<<62)/tronClaimMaxPercent {
		return 0, 0
	}
	minMicros := (expectedMicros*tronClaimMinPercent + 99) / 100
	maxMicros := expectedMicros * tronClaimMaxPercent / 100
	return minMicros, maxMicros
}

// attachTronSettlements fills, for settled TRON top-ups only, what arrived
// and what was credited next to the order's face value, so a part-paid order
// is not shown as if the full amount had been paid and credited. It returns
// copies; the rows that were passed in are left as they were.
func attachTronSettlements(topUps []*TopUp) ([]*TopUp, error) {
	topUpIDs := make([]int, 0, len(topUps))
	for _, topUp := range topUps {
		if topUp != nil && topUp.PaymentProvider == PaymentProviderTron && topUp.Status == common.TopUpStatusSuccess {
			topUpIDs = append(topUpIDs, topUp.Id)
		}
	}
	if len(topUpIDs) == 0 {
		return topUps, nil
	}
	var orders []TronTopupOrder
	if err := DB.Where("top_up_id IN ?", topUpIDs).Find(&orders).Error; err != nil {
		return nil, err
	}
	orderIDs := make([]int64, 0, len(orders))
	orderByTopUp := make(map[int]TronTopupOrder, len(orders))
	for _, order := range orders {
		orderIDs = append(orderIDs, order.ID)
		orderByTopUp[order.TopUpID] = order
	}
	depositByOrder := make(map[int64]TronDeposit, len(orders))
	if len(orderIDs) > 0 {
		var deposits []TronDeposit
		if err := DB.Where("order_id IN ? AND status = ?", orderIDs, TronDepositStatusCredited).Order("id DESC").Find(&deposits).Error; err != nil {
			return nil, err
		}
		for _, deposit := range deposits {
			depositByOrder[*deposit.OrderID] = deposit
		}
	}
	enriched := make([]*TopUp, 0, len(topUps))
	for _, topUp := range topUps {
		order, hasOrder := orderByTopUp[topUpID(topUp)]
		deposit, hasDeposit := depositByOrder[order.ID]
		if topUp == nil || !hasOrder || !hasDeposit {
			enriched = append(enriched, topUp)
			continue
		}
		withSettlement := *topUp
		withSettlement.TronReceivedMicros = deposit.AmountMicros
		withSettlement.TronCreditedQuota = deposit.CreditedQuota
		withSettlement.TronFaceQuota = order.CreditQuota
		enriched = append(enriched, &withSettlement)
	}
	return enriched, nil
}

func topUpID(topUp *TopUp) int {
	if topUp == nil {
		return 0
	}
	return topUp.Id
}
