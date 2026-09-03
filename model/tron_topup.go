package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TronTopupOrder struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	TopUpID              int    `json:"top_up_id" gorm:"column:top_up_id;uniqueIndex"`
	UserID               int    `json:"user_id" gorm:"column:user_id;index"`
	TradeNo              string `json:"trade_no" gorm:"type:varchar(255);uniqueIndex"`
	ReceiveAddress       string `json:"receive_address" gorm:"type:varchar(64)"`
	TokenContract        string `json:"token_contract" gorm:"type:varchar(64)"`
	ExpectedAmountMicros int64  `json:"expected_amount_micros" gorm:"type:bigint;uniqueIndex"`
	RateCNYMicros        int64  `json:"rate_cny_micros" gorm:"type:bigint"`
	QuoteUpdatedAtMS     int64  `json:"quote_updated_at_ms" gorm:"type:bigint"`
	ExpiresAtMS          int64  `json:"expires_at_ms" gorm:"type:bigint;index"`
	CreditQuota          int    `json:"credit_quota" gorm:"type:bigint"`
	CreatedAtMS          int64  `json:"created_at_ms" gorm:"type:bigint;index"`
	CompletedAtMS        int64  `json:"completed_at_ms" gorm:"type:bigint"`
}

type TronDeposit struct {
	ID               int64  `json:"id" gorm:"primaryKey"`
	TxID             string `json:"tx_id" gorm:"type:char(64);uniqueIndex"`
	BlockTimestampMS int64  `json:"block_timestamp_ms" gorm:"type:bigint;index"`
	FromAddress      string `json:"from_address" gorm:"type:varchar(64)"`
	ToAddress        string `json:"to_address" gorm:"type:varchar(64)"`
	TokenContract    string `json:"token_contract" gorm:"type:varchar(64)"`
	AmountMicros     int64  `json:"amount_micros" gorm:"type:bigint"`
	OrderID          *int64 `json:"order_id,omitempty" gorm:"column:order_id;index"`
	CreditedQuota    int    `json:"credited_quota" gorm:"type:bigint"`
	Status           string `json:"status" gorm:"type:varchar(32);index"`
	ObservedAtMS     int64  `json:"observed_at_ms" gorm:"type:bigint"`
}

type TronTopupTicket struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	UserID          int    `json:"user_id" gorm:"column:user_id;index"`
	OrderID         int64  `json:"order_id" gorm:"column:order_id;index"`
	DepositID       *int64 `json:"deposit_id,omitempty" gorm:"column:deposit_id;index"`
	ClaimKey        string `json:"-" gorm:"type:varchar(160);uniqueIndex"`
	TxID            string `json:"tx_id" gorm:"type:char(64);index"`
	Reason          string `json:"reason" gorm:"type:varchar(32);index"`
	Status          string `json:"status" gorm:"type:varchar(32);index"`
	UserNote        string `json:"user_note" gorm:"type:varchar(500)"`
	AdminNote       string `json:"admin_note" gorm:"type:varchar(500)"`
	ResolverID      int    `json:"resolver_id" gorm:"column:resolver_id;index"`
	ResolvedOrderID int64  `json:"resolved_order_id" gorm:"column:resolved_order_id;index"`
	CreditedUserID  int    `json:"credited_user_id" gorm:"column:credited_user_id;index"`
	CreatedAtMS     int64  `json:"created_at_ms" gorm:"type:bigint;index"`
	UpdatedAtMS     int64  `json:"updated_at_ms" gorm:"type:bigint"`
	ResolvedAtMS    int64  `json:"resolved_at_ms" gorm:"type:bigint"`
}

type TronScanCheckpoint struct {
	Name          string `json:"name" gorm:"type:varchar(64);primaryKey"`
	LastScannedMS int64  `json:"last_scanned_ms" gorm:"type:bigint"`
	UpdatedAtMS   int64  `json:"updated_at_ms" gorm:"type:bigint"`
}

type TronTransferRecord struct {
	TxID             string
	BlockTimestampMS int64
	FromAddress      string
	ToAddress        string
	TokenContract    string
	AmountMicros     int64
	ObservedAtMS     int64
}

type TronSettlementResult struct {
	DepositID        int64
	OrderID          int64
	Credited         bool
	NeedsReview      bool
	AlreadyProcessed bool
}

const (
	TronDepositStatusUnmatched = "unmatched"
	TronDepositStatusReview    = "review"
	TronDepositStatusCredited  = "credited"

	TronTicketReasonUserClaim    = "user_claim"
	TronTicketReasonLatePayment  = "late_payment"
	TronTicketReasonCreditFailed = "credit_failed"

	TronTicketStatusOpen     = "open"
	TronTicketStatusResolved = "resolved"
	TronTicketStatusRejected = "rejected"

	tronClaimMinPercent    int64 = 90
	tronClaimMaxPercent    int64 = 110
	TronClaimGracePeriodMS int64 = 24 * 60 * 60 * 1000
	tronMaxClaimsPerOrder  int64 = 3
)

var (
	ErrTronOrderNotFound         = errors.New("TRON top-up order not found")
	ErrTronTransferMismatch      = errors.New("TRON transfer does not match order")
	ErrTronTransferPredatesOrder = errors.New("TRON transfer predates order")
	ErrTronCreditReviewRequired  = errors.New("TRON credit requires manual review")
	ErrTronTxAlreadyClaimed      = errors.New("TRON transaction is already claimed")
	ErrTronTicketNotOpen         = errors.New("TRON top-up ticket is not open")
	ErrTronClaimWindowClosed     = errors.New("TRON top-up claim window is closed")
	ErrTronClaimLimitReached     = errors.New("TRON top-up claim limit reached")
	tronModelTxIDPattern         = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
)

func CreateTronTopupOrder(topUp *TopUp, order *TronTopupOrder) error {
	if err := validateNewTronTopupOrder(topUp, order); err != nil {
		return err
	}

	topUpID := topUp.Id
	orderID := order.ID
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		order.TopUpID = topUp.Id
		return tx.Create(order).Error
	})
	if err != nil {
		topUp.Id = topUpID
		order.ID = orderID
		order.TopUpID = 0
	}
	return err
}

func CreateOrGetActiveTronTopupOrder(topUp *TopUp, order *TronTopupOrder, nowMS int64) (*TronTopupOrder, bool, error) {
	if err := validateNewTronTopupOrder(topUp, order); err != nil {
		return nil, false, err
	}
	if nowMS <= 0 {
		return nil, false, errors.New("invalid TRON order time")
	}

	originalTopUpID := topUp.Id
	originalOrderID := order.ID
	var result *TronTopupOrder
	created := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		user := User{}
		if err := lockForUpdate(tx).Select("id").Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
			return err
		}
		active := TronTopupOrder{}
		err := tx.Table("tron_topup_orders").
			Select("tron_topup_orders.*").
			Joins("JOIN top_ups ON top_ups.id = tron_topup_orders.top_up_id").
			Where("tron_topup_orders.user_id = ? AND tron_topup_orders.expires_at_ms > ? AND top_ups.status = ?", topUp.UserId, nowMS, common.TopUpStatusPending).
			Order("tron_topup_orders.id DESC").
			First(&active).Error
		if err == nil {
			result = &active
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		order.TopUpID = topUp.Id
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		copyOfOrder := *order
		result = &copyOfOrder
		created = true
		return nil
	})
	if err != nil || !created {
		topUp.Id = originalTopUpID
		order.ID = originalOrderID
		order.TopUpID = 0
	}
	return result, created, err
}

func validateNewTronTopupOrder(topUp *TopUp, order *TronTopupOrder) error {
	if topUp == nil || order == nil || topUp.Id != 0 || order.ID != 0 {
		return errors.New("invalid TRON top-up order")
	}
	if topUp.PaymentProvider != PaymentProviderTron || topUp.PaymentMethod != PaymentMethodTron || topUp.Status != common.TopUpStatusPending {
		return ErrPaymentMethodMismatch
	}
	if topUp.UserId <= 0 || topUp.UserId != order.UserID || topUp.TradeNo == "" || topUp.TradeNo != order.TradeNo {
		return errors.New("inconsistent TRON top-up order")
	}
	if order.ExpectedAmountMicros <= 0 || order.RateCNYMicros <= 0 || order.CreditQuota <= 0 || order.CreditQuota > common.MaxQuota || order.ExpiresAtMS <= order.CreatedAtMS || topUp.CreateTime != order.CreatedAtMS/1000 {
		return errors.New("invalid TRON top-up amounts")
	}
	return nil
}

func GetTronTopupOrderByTradeNo(tradeNo string) (*TronTopupOrder, error) {
	var order TronTopupOrder
	err := DB.Where("trade_no = ?", tradeNo).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &order, err
}

func GetTronTopupOrderForUser(userID int, tradeNo string) (*TronTopupOrder, *TopUp, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if userID <= 0 || tradeNo == "" || len(tradeNo) > 255 {
		return nil, nil, ErrTronOrderNotFound
	}
	var order TronTopupOrder
	if err := DB.Where("user_id = ? AND trade_no = ?", userID, tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrTronOrderNotFound
		}
		return nil, nil, err
	}
	var topUp TopUp
	if err := DB.Where("id = ?", order.TopUpID).First(&topUp).Error; err != nil {
		return nil, nil, err
	}
	if topUp.UserId != userID || topUp.TradeNo != tradeNo || topUp.PaymentProvider != PaymentProviderTron {
		return nil, nil, ErrTronTransferMismatch
	}
	return &order, &topUp, nil
}

func GetTronTopupOrderByID(orderID int64) (*TronTopupOrder, error) {
	var order TronTopupOrder
	err := DB.Where("id = ?", orderID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTronOrderNotFound
	}
	return &order, err
}

func GetTronTopupTicketByID(ticketID int64) (*TronTopupTicket, error) {
	var ticket TronTopupTicket
	err := DB.Where("id = ?", ticketID).First(&ticket).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("TRON top-up ticket not found")
	}
	return &ticket, err
}

func ListTronTopupTickets(pageInfo *common.PageInfo, status string) ([]*TronTopupTicket, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("page info is required")
	}
	page := pageInfo.GetPage()
	pageSize := pageInfo.GetPageSize()
	maxInt := int(^uint(0) >> 1)
	if page < 1 || pageSize < 1 || pageSize > 100 || page-1 > maxInt/pageSize {
		return nil, 0, errors.New("unsafe TRON ticket pagination")
	}
	offset := (page - 1) * pageSize
	query := DB.Model(&TronTopupTicket{})
	if status != "" {
		if status != TronTicketStatusOpen && status != TronTicketStatusResolved && status != TronTicketStatusRejected {
			return nil, 0, errors.New("invalid TRON ticket status")
		}
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tickets []*TronTopupTicket
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset).Find(&tickets).Error; err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

func CountOpenTronTopupTickets() (int64, error) {
	var count int64
	err := DB.Model(&TronTopupTicket{}).Where("status = ?", TronTicketStatusOpen).Count(&count).Error
	return count, err
}

func RejectTronTopupTicket(ticketID int64, resolverID int, note string, nowMS int64) error {
	note = strings.TrimSpace(note)
	if ticketID <= 0 || resolverID <= 0 || note == "" || len([]rune(note)) > 500 || nowMS <= 0 {
		return errors.New("invalid TRON ticket rejection")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		ticket := TronTopupTicket{}
		if err := lockForUpdate(tx).Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			return err
		}
		if ticket.Status == TronTicketStatusRejected {
			return nil
		}
		if ticket.Status != TronTicketStatusOpen {
			return ErrTronTicketNotOpen
		}
		ticket.Status = TronTicketStatusRejected
		ticket.AdminNote = note
		ticket.ResolverID = resolverID
		ticket.ResolvedAtMS = nowMS
		ticket.UpdatedAtMS = nowMS
		return tx.Save(&ticket).Error
	})
}

func GetActiveTronTopupOrderForUser(userID int, nowMS int64) (*TronTopupOrder, error) {
	var order TronTopupOrder
	err := DB.Table("tron_topup_orders").
		Select("tron_topup_orders.*").
		Joins("JOIN top_ups ON top_ups.id = tron_topup_orders.top_up_id").
		Where("tron_topup_orders.user_id = ? AND tron_topup_orders.expires_at_ms > ? AND top_ups.status = ?", userID, nowMS, common.TopUpStatusPending).
		Order("tron_topup_orders.id DESC").
		First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &order, err
}

func TronExpectedAmountExists(amountMicros int64) (bool, error) {
	var count int64
	err := DB.Model(&TronTopupOrder{}).Where("expected_amount_micros = ?", amountMicros).Limit(1).Count(&count).Error
	return count > 0, err
}

func GetTronScanCheckpoint(name string) (*TronScanCheckpoint, error) {
	var checkpoint TronScanCheckpoint
	err := DB.Where("name = ?", name).First(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &checkpoint, err
}

func AdvanceTronScanCheckpoint(name string, lastScannedMS int64, updatedAtMS int64) error {
	if name == "" || lastScannedMS <= 0 || updatedAtMS <= 0 {
		return errors.New("invalid TRON scan checkpoint")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		checkpoint := TronScanCheckpoint{}
		err := lockForUpdate(tx).Where("name = ?", name).First(&checkpoint).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&TronScanCheckpoint{Name: name, LastScannedMS: lastScannedMS, UpdatedAtMS: updatedAtMS}).Error
		}
		if err != nil {
			return err
		}
		if checkpoint.LastScannedMS >= lastScannedMS {
			return nil
		}
		checkpoint.LastScannedMS = lastScannedMS
		checkpoint.UpdatedAtMS = updatedAtMS
		return tx.Save(&checkpoint).Error
	})
}

func RecordFailedTronSettlement(transfer TronTransferRecord) (bool, error) {
	normalized, err := normalizeTronTransferRecord(transfer)
	if err != nil {
		return false, err
	}
	transfer = normalized
	needsReview := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		order := TronTopupOrder{}
		if err := lockForUpdate(tx).Where("expected_amount_micros = ?", transfer.AmountMicros).First(&order).Error; err != nil {
			return err
		}
		if transfer.ToAddress != order.ReceiveAddress || transfer.TokenContract != order.TokenContract {
			return ErrTronTransferMismatch
		}
		topUp := TopUp{}
		if err := lockForUpdate(tx).Where("id = ?", order.TopUpID).First(&topUp).Error; err != nil {
			return err
		}
		validPendingOrder := topUp.PaymentProvider == PaymentProviderTron && topUp.PaymentMethod == PaymentMethodTron && topUp.Status == common.TopUpStatusPending && topUp.UserId == order.UserID && topUp.TradeNo == order.TradeNo && transfer.BlockTimestampMS >= order.CreatedAtMS

		deposit := TronDeposit{}
		err := tx.Where("tx_id = ?", transfer.TxID).First(&deposit).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			deposit = TronDeposit{TxID: transfer.TxID, BlockTimestampMS: transfer.BlockTimestampMS, FromAddress: transfer.FromAddress, ToAddress: transfer.ToAddress, TokenContract: transfer.TokenContract, AmountMicros: transfer.AmountMicros, Status: TronDepositStatusUnmatched, ObservedAtMS: transfer.ObservedAtMS}
			if validPendingOrder {
				orderID := order.ID
				deposit.OrderID = &orderID
				deposit.Status = TronDepositStatusReview
			}
			if err := tx.Create(&deposit).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !sameTronTransfer(&deposit, transfer) {
			return errors.New("stored TRON deposit conflicts with transfer")
		}
		if !validPendingOrder {
			return nil
		}

		claimKey := "auto:credit_failed:" + transfer.TxID
		ticket := TronTopupTicket{}
		err = tx.Where("claim_key = ?", claimKey).First(&ticket).Error
		if err == nil {
			needsReview = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&TronTopupTicket{UserID: order.UserID, OrderID: order.ID, DepositID: &deposit.ID, ClaimKey: claimKey, TxID: transfer.TxID, Reason: TronTicketReasonCreditFailed, Status: TronTicketStatusOpen, CreatedAtMS: transfer.ObservedAtMS, UpdatedAtMS: transfer.ObservedAtMS}).Error; err != nil {
			return err
		}
		needsReview = true
		return nil
	})
	return needsReview, err
}

func SettleTronDeposit(transfer TronTransferRecord, callerIP string) (TronSettlementResult, error) {
	normalized, err := normalizeTronTransferRecord(transfer)
	if err != nil {
		return TronSettlementResult{}, err
	}
	transfer = normalized
	result := TronSettlementResult{}
	var userID int
	var creditedQuota int
	var tradeNo string

	err = DB.Transaction(func(tx *gorm.DB) error {
		existing := TronDeposit{}
		err := tx.Where("tx_id = ?", transfer.TxID).First(&existing).Error
		if err == nil {
			if !sameTronTransfer(&existing, transfer) {
				return ErrTronTransferMismatch
			}
			if existing.Status != TronDepositStatusCredited && existing.Status != TronDepositStatusReview && existing.Status != TronDepositStatusUnmatched {
				return errors.New("stored TRON deposit has invalid status")
			}
			result.DepositID = existing.ID
			if existing.OrderID != nil {
				result.OrderID = *existing.OrderID
			}
			result.Credited = existing.Status == TronDepositStatusCredited
			result.NeedsReview = existing.Status == TronDepositStatusReview
			result.AlreadyProcessed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		order := TronTopupOrder{}
		if err := lockForUpdate(tx).Where("expected_amount_micros = ?", transfer.AmountMicros).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTronOrderNotFound
			}
			return err
		}
		topUp := TopUp{}
		if err := lockForUpdate(tx).Where("id = ?", order.TopUpID).First(&topUp).Error; err != nil {
			return err
		}
		if err := validateTronOrderTransfer(&topUp, &order, transfer); err != nil {
			return err
		}

		orderID := order.ID
		depositStatus := TronDepositStatusCredited
		if transfer.BlockTimestampMS > order.ExpiresAtMS {
			depositStatus = TronDepositStatusReview
		}
		deposit := TronDeposit{
			TxID:             transfer.TxID,
			BlockTimestampMS: transfer.BlockTimestampMS,
			FromAddress:      transfer.FromAddress,
			ToAddress:        transfer.ToAddress,
			TokenContract:    transfer.TokenContract,
			AmountMicros:     transfer.AmountMicros,
			OrderID:          &orderID,
			Status:           depositStatus,
			ObservedAtMS:     transfer.ObservedAtMS,
		}
		if err := tx.Create(&deposit).Error; err != nil {
			return err
		}
		result.DepositID = deposit.ID
		result.OrderID = order.ID

		if depositStatus == TronDepositStatusReview {
			claimKey := "auto:late:" + transfer.TxID
			ticket := TronTopupTicket{
				UserID:      order.UserID,
				OrderID:     order.ID,
				DepositID:   &deposit.ID,
				ClaimKey:    claimKey,
				TxID:        transfer.TxID,
				Reason:      TronTicketReasonLatePayment,
				Status:      TronTicketStatusOpen,
				CreatedAtMS: transfer.ObservedAtMS,
				UpdatedAtMS: transfer.ObservedAtMS,
			}
			if err := tx.Create(&ticket).Error; err != nil {
				return err
			}
			result.NeedsReview = true
			return nil
		}

		credited, _, err := creditTronOrderTx(tx, &topUp, &order, &deposit, order.CreditQuota, transfer.ObservedAtMS)
		if err != nil {
			return err
		}
		result.Credited = true
		userID = order.UserID
		creditedQuota = credited
		tradeNo = order.TradeNo
		deposit.CreditedQuota = creditedQuota
		return nil
	})
	if err != nil {
		return TronSettlementResult{}, err
	}
	if result.Credited && !result.AlreadyProcessed {
		refreshTronQuotaCache(userID, creditedQuota)
		RecordTopupLog(userID, fmt.Sprintf("TRON USDT 充值成功，订单: %s，txid: %s，入账额度: %v", tradeNo, transfer.TxID, logger.FormatQuota(creditedQuota)), callerIP, PaymentMethodTron, PaymentProviderTron)
	}
	return result, nil
}

func RecordUnmatchedTronDeposit(transfer TronTransferRecord) (*TronDeposit, error) {
	normalized, err := normalizeTronTransferRecord(transfer)
	if err != nil {
		return nil, err
	}
	transfer = normalized
	deposit := TronDeposit{
		TxID:             transfer.TxID,
		BlockTimestampMS: transfer.BlockTimestampMS,
		FromAddress:      transfer.FromAddress,
		ToAddress:        transfer.ToAddress,
		TokenContract:    transfer.TokenContract,
		AmountMicros:     transfer.AmountMicros,
		Status:           TronDepositStatusUnmatched,
		ObservedAtMS:     transfer.ObservedAtMS,
	}
	createErr := DB.Create(&deposit).Error
	if createErr == nil {
		return &deposit, nil
	}
	var existing TronDeposit
	if err := DB.Where("tx_id = ?", transfer.TxID).First(&existing).Error; err != nil {
		return nil, createErr
	}
	if !sameTronTransfer(&existing, transfer) {
		return nil, errors.New("stored TRON deposit conflicts with transfer")
	}
	return &existing, nil
}

func SubmitTronTopupClaim(userID int, tradeNo string, txID string, note string) (*TronTopupTicket, error) {
	return submitTronTopupClaimAt(userID, tradeNo, txID, note, common.GetTimestamp()*1000)
}

func submitTronTopupClaimAt(userID int, tradeNo string, txID string, note string, nowMS int64) (*TronTopupTicket, error) {
	txID = strings.ToLower(strings.TrimSpace(txID))
	note = strings.TrimSpace(note)
	if userID <= 0 || tradeNo == "" || !tronModelTxIDPattern.MatchString(txID) || len([]rune(note)) > 500 || nowMS <= 0 {
		return nil, errors.New("invalid TRON top-up claim")
	}

	var result TronTopupTicket
	var claimKey string
	err := DB.Transaction(func(tx *gorm.DB) error {
		order := TronTopupOrder{}
		err := lockForUpdate(tx).Where("trade_no = ? AND user_id = ?", tradeNo, userID).First(&order).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTronOrderNotFound
		}
		if err != nil {
			return err
		}
		topUp := TopUp{}
		if err := tx.Where("id = ?", order.TopUpID).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderTron || topUp.PaymentMethod != PaymentMethodTron || topUp.Status != common.TopUpStatusPending || topUp.UserId != order.UserID || topUp.TradeNo != order.TradeNo {
			return ErrTopUpStatusInvalid
		}

		claimKey = fmt.Sprintf("claim:%d:%d:%s", userID, order.ID, txID)
		existingTicket := TronTopupTicket{}
		err = tx.Where("claim_key = ?", claimKey).First(&existingTicket).Error
		if err == nil {
			result = existingTicket
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if nowMS > order.ExpiresAtMS && nowMS-order.ExpiresAtMS > TronClaimGracePeriodMS {
			return ErrTronClaimWindowClosed
		}

		var depositID *int64
		deposit := TronDeposit{}
		err = tx.Where("tx_id = ?", txID).First(&deposit).Error
		if err == nil {
			if deposit.Status == TronDepositStatusCredited {
				return ErrTronTxAlreadyClaimed
			}
			depositID = &deposit.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var claimCount int64
		if err := tx.Model(&TronTopupTicket{}).
			Where("order_id = ? AND reason = ?", order.ID, TronTicketReasonUserClaim).
			Count(&claimCount).Error; err != nil {
			return err
		}
		if claimCount >= tronMaxClaimsPerOrder {
			return ErrTronClaimLimitReached
		}

		openTicket := TronTopupTicket{}
		err = lockForUpdate(tx).
			Where("order_id = ? AND reason = ? AND status = ?", order.ID, TronTicketReasonUserClaim, TronTicketStatusOpen).
			Order("id ASC").
			First(&openTicket).Error
		if err == nil {
			openTicket.Status = TronTicketStatusRejected
			openTicket.AdminNote = "superseded by corrected claim"
			openTicket.ResolvedAtMS = nowMS
			openTicket.UpdatedAtMS = nowMS
			if err := tx.Save(&openTicket).Error; err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		result = TronTopupTicket{
			UserID:      userID,
			OrderID:     order.ID,
			DepositID:   depositID,
			ClaimKey:    claimKey,
			TxID:        txID,
			Reason:      TronTicketReasonUserClaim,
			Status:      TronTicketStatusOpen,
			UserNote:    note,
			CreatedAtMS: nowMS,
			UpdatedAtMS: nowMS,
		}
		return tx.Create(&result).Error
	})
	if err != nil {
		if claimKey != "" {
			var existing TronTopupTicket
			if reloadErr := DB.Where("claim_key = ?", claimKey).First(&existing).Error; reloadErr == nil {
				return &existing, nil
			}
		}
		return nil, err
	}
	return &result, nil
}

func ResolveTronTopupTicket(ticketID int64, selectedOrderID int64, transfer TronTransferRecord, resolverID int, note string) error {
	note = strings.TrimSpace(note)
	if ticketID <= 0 || selectedOrderID <= 0 || resolverID <= 0 || note == "" || len([]rune(note)) > 500 {
		return errors.New("invalid TRON ticket resolution")
	}
	normalized, err := normalizeTronTransferRecord(transfer)
	if err != nil {
		return err
	}
	transfer = normalized
	var userID int
	var creditedQuota int
	var tradeNo string
	err = DB.Transaction(func(tx *gorm.DB) error {
		order := TronTopupOrder{}
		if err := lockForUpdate(tx).Where("id = ?", selectedOrderID).First(&order).Error; err != nil {
			return err
		}
		ticket := TronTopupTicket{}
		if err := lockForUpdate(tx).Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			return err
		}
		if ticket.Status == TronTicketStatusResolved {
			if ticket.TxID != transfer.TxID || ticket.ResolvedOrderID != selectedOrderID {
				return ErrTronTransferMismatch
			}
			return nil
		}
		if ticket.Status != TronTicketStatusOpen {
			return ErrTronTicketNotOpen
		}
		if ticket.TxID != transfer.TxID {
			return ErrTronTransferMismatch
		}

		topUp := TopUp{}
		if err := lockForUpdate(tx).Where("id = ?", order.TopUpID).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderTron || topUp.Status != common.TopUpStatusPending || transfer.ToAddress != order.ReceiveAddress || transfer.TokenContract != order.TokenContract || transfer.BlockTimestampMS < order.CreatedAtMS || transfer.BlockTimestampMS > order.ExpiresAtMS+TronClaimGracePeriodMS {
			return ErrTronTransferMismatch
		}
		approvedQuota, err := calculateTronClaimQuota(order.ExpectedAmountMicros, transfer.AmountMicros, order.CreditQuota)
		if err != nil {
			return err
		}

		deposit := TronDeposit{}
		err = lockForUpdate(tx).Where("tx_id = ?", transfer.TxID).First(&deposit).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			orderID := order.ID
			deposit = TronDeposit{TxID: transfer.TxID, BlockTimestampMS: transfer.BlockTimestampMS, FromAddress: transfer.FromAddress, ToAddress: transfer.ToAddress, TokenContract: transfer.TokenContract, AmountMicros: transfer.AmountMicros, OrderID: &orderID, Status: TronDepositStatusReview, ObservedAtMS: transfer.ObservedAtMS}
			if err := tx.Create(&deposit).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if !sameTronTransfer(&deposit, transfer) {
				return ErrTronTransferMismatch
			}
			if deposit.Status == TronDepositStatusCredited {
				return ErrTronTxAlreadyClaimed
			}
			orderID := order.ID
			deposit.OrderID = &orderID
		}

		credited, _, err := creditTronOrderTx(tx, &topUp, &order, &deposit, approvedQuota, transfer.ObservedAtMS)
		if err != nil {
			return err
		}
		ticket.DepositID = &deposit.ID
		ticket.Status = TronTicketStatusResolved
		ticket.AdminNote = note
		ticket.ResolverID = resolverID
		ticket.ResolvedOrderID = order.ID
		ticket.CreditedUserID = order.UserID
		ticket.ResolvedAtMS = transfer.ObservedAtMS
		ticket.UpdatedAtMS = transfer.ObservedAtMS
		if err := tx.Save(&ticket).Error; err != nil {
			return err
		}
		if err := tx.Model(&TronTopupTicket{}).
			Where("tx_id = ? AND id <> ? AND status = ?", transfer.TxID, ticket.ID, TronTicketStatusOpen).
			Updates(map[string]any{"status": TronTicketStatusRejected, "admin_note": "resolved by competing claim", "resolver_id": resolverID, "resolved_at_ms": transfer.ObservedAtMS, "updated_at_ms": transfer.ObservedAtMS}).Error; err != nil {
			return err
		}
		userID = order.UserID
		creditedQuota = credited
		tradeNo = order.TradeNo
		return nil
	})
	if err != nil {
		return err
	}
	if userID != 0 {
		refreshTronQuotaCache(userID, creditedQuota)
		RecordTopupLog(userID, fmt.Sprintf("TRON USDT 工单入账成功，订单: %s，txid: %s，处理人: %d，入账额度: %v", tradeNo, transfer.TxID, resolverID, logger.FormatQuota(creditedQuota)), "tron-ticket", PaymentMethodTron, PaymentProviderTron)
	}
	return nil
}

func creditTronOrderTx(tx *gorm.DB, topUp *TopUp, order *TronTopupOrder, deposit *TronDeposit, creditQuota int, completedAtMS int64) (int, int, error) {
	if creditQuota <= 0 || creditQuota > common.MaxQuota {
		return 0, 0, errors.New("TRON credit quota is out of range")
	}
	user := User{}
	if err := lockForUpdate(tx).Select("id", "quota").Where("id = ?", order.UserID).First(&user).Error; err != nil {
		return 0, 0, err
	}
	if int64(user.Quota) > int64(common.MaxQuota)-int64(creditQuota) {
		return 0, 0, fmt.Errorf("%w: quota safety limit", ErrTronCreditReviewRequired)
	}
	newQuota := int(int64(user.Quota) + int64(creditQuota))
	if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", newQuota).Error; err != nil {
		return 0, 0, err
	}

	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = completedAtMS / 1000
	if err := tx.Save(topUp).Error; err != nil {
		return 0, 0, err
	}
	order.CompletedAtMS = completedAtMS
	if err := tx.Save(order).Error; err != nil {
		return 0, 0, err
	}
	deposit.Status = TronDepositStatusCredited
	deposit.CreditedQuota = creditQuota
	if err := tx.Save(deposit).Error; err != nil {
		return 0, 0, err
	}
	return creditQuota, newQuota, nil
}

func validateTronOrderTransfer(topUp *TopUp, order *TronTopupOrder, transfer TronTransferRecord) error {
	if topUp.PaymentProvider != PaymentProviderTron || topUp.PaymentMethod != PaymentMethodTron {
		return ErrPaymentMethodMismatch
	}
	if topUp.Status != common.TopUpStatusPending || topUp.UserId != order.UserID || topUp.TradeNo != order.TradeNo {
		return ErrTopUpStatusInvalid
	}
	if transfer.BlockTimestampMS < order.CreatedAtMS {
		return ErrTronTransferPredatesOrder
	}
	if transfer.AmountMicros != order.ExpectedAmountMicros || transfer.ToAddress != order.ReceiveAddress || transfer.TokenContract != order.TokenContract {
		return ErrTronTransferMismatch
	}
	return nil
}

func normalizeTronTransferRecord(transfer TronTransferRecord) (TronTransferRecord, error) {
	transfer.TxID = strings.ToLower(strings.TrimSpace(transfer.TxID))
	if !tronModelTxIDPattern.MatchString(transfer.TxID) || transfer.BlockTimestampMS <= 0 || transfer.ObservedAtMS < transfer.BlockTimestampMS || transfer.AmountMicros <= 0 || transfer.ToAddress == "" || transfer.TokenContract == "" {
		return TronTransferRecord{}, errors.New("invalid TRON transfer")
	}
	return transfer, nil
}

func sameTronTransfer(deposit *TronDeposit, transfer TronTransferRecord) bool {
	return deposit.TxID == transfer.TxID && deposit.BlockTimestampMS == transfer.BlockTimestampMS && deposit.FromAddress == transfer.FromAddress && deposit.ToAddress == transfer.ToAddress && deposit.TokenContract == transfer.TokenContract && deposit.AmountMicros == transfer.AmountMicros
}

func calculateTronClaimQuota(expectedMicros int64, actualMicros int64, creditQuota int) (int, error) {
	if expectedMicros <= 0 || actualMicros <= 0 || creditQuota <= 0 || creditQuota > common.MaxQuota {
		return 0, errors.New("TRON claim amounts are out of range")
	}
	expected := decimal.NewFromInt(expectedMicros)
	actual := decimal.NewFromInt(actualMicros)
	percent := actual.Mul(decimal.NewFromInt(100)).Div(expected)
	if percent.LessThan(decimal.NewFromInt(tronClaimMinPercent)) || percent.GreaterThan(decimal.NewFromInt(tronClaimMaxPercent)) {
		return 0, errors.New("TRON claim amount is outside review policy")
	}
	quotaDecimal := actual.Mul(decimal.NewFromInt(int64(creditQuota))).Div(expected).Floor()
	quota, clamp := common.QuotaFromDecimalChecked(quotaDecimal)
	if clamp != nil || quota <= 0 {
		return 0, errors.New("TRON claim quota is out of range")
	}
	return quota, nil
}

func refreshTronQuotaCache(userID int, quotaDelta int) {
	if !common.RedisEnabled {
		return
	}
	gopool.Go(func() {
		if err := cacheIncrUserQuota(userID, int64(quotaDelta)); err != nil {
			common.SysLog("failed to refresh user quota cache after TRON top-up: " + err.Error())
		}
	})
}
