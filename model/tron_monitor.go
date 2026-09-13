package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// RecordReviewTronDeposit isolates verified funds without asserting ownership.
func RecordReviewTronDeposit(transfer TronTransferRecord, reason string) (TronSettlementResult, error) {
	normalized, err := normalizeTronTransferRecord(transfer)
	if err != nil {
		return TronSettlementResult{}, err
	}
	if reason != "multiple_senders" {
		return TronSettlementResult{}, errors.New("invalid TRON review reason")
	}
	transfer = normalized
	result := TronSettlementResult{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing TronDeposit
		err := lockForUpdate(tx).Where("tx_id = ?", transfer.TxID).First(&existing).Error
		if err == nil {
			if !sameTronTransfer(&existing, transfer) {
				return ErrTronTransferMismatch
			}
			if existing.SourceAddresses == "" && transfer.SourceAddresses != "" {
				if err := tx.Model(&existing).Update("source_addresses", transfer.SourceAddresses).Error; err != nil {
					return err
				}
			}
			result = TronSettlementResult{DepositID: existing.ID, NeedsReview: existing.Status == TronDepositStatusReview, AlreadyProcessed: true}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		deposit := TronDeposit{TxID: transfer.TxID, BlockTimestampMS: transfer.BlockTimestampMS, FromAddress: transfer.FromAddress, ToAddress: transfer.ToAddress, TokenContract: transfer.TokenContract, AmountMicros: transfer.AmountMicros, ObservedAtMS: transfer.ObservedAtMS, Status: TronDepositStatusReview, ReviewReason: reason, SourceAddresses: transfer.SourceAddresses}
		if err := tx.Create(&deposit).Error; err != nil {
			return err
		}
		result = TronSettlementResult{DepositID: deposit.ID, NeedsReview: true}
		return nil
	})
	return result, err
}

type TronDepositSummary struct {
	UnmatchedCount        int64 `json:"unmatched_count"`
	UnmatchedAmountMicros int64 `json:"unmatched_amount_micros"`
	OldestUnmatchedMS     int64 `json:"oldest_unmatched_ms"`
	ReviewCount           int64 `json:"review_count"`
}

func GetTronDepositSummary() (TronDepositSummary, error) {
	var result TronDepositSummary
	err := DB.Model(&TronDeposit{}).Where("status = ?", TronDepositStatusUnmatched).
		Select("COUNT(*) AS unmatched_count, COALESCE(SUM(amount_micros),0) AS unmatched_amount_micros, COALESCE(MIN(observed_at_ms),0) AS oldest_unmatched_ms").Scan(&result).Error
	if err != nil {
		return result, err
	}
	err = DB.Model(&TronDeposit{}).Where("status = ?", TronDepositStatusReview).Count(&result.ReviewCount).Error
	return result, err
}

func ListTronDeposits(page *common.PageInfo, status string) ([]*TronDeposit, int64, error) {
	if page == nil || page.GetPage() < 1 || page.GetPageSize() < 1 || page.GetPageSize() > 100 || page.GetPage() > 1000000 {
		return nil, 0, errors.New("unsafe TRON deposit pagination")
	}
	query := DB.Model(&TronDeposit{})
	switch status {
	case "":
	case TronDepositStatusUnmatched, TronDepositStatusReview, TronDepositStatusCredited:
		query = query.Where("status = ?", status)
	default:
		return nil, 0, errors.New("invalid TRON deposit status")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var deposits []*TronDeposit
	err := query.Order("id DESC").Limit(page.GetPageSize()).Offset((page.GetPage() - 1) * page.GetPageSize()).Find(&deposits).Error
	return deposits, count, err
}

// BeginTronReconciliation resumes an unfinished fixed window, never a moving one.
func BeginTronReconciliation(name string, fromMS, toMS, nowMS, intervalMS int64) (*TronScanCheckpoint, error) {
	var result *TronScanCheckpoint
	err := DB.Transaction(func(tx *gorm.DB) error {
		var row TronScanCheckpoint
		err := lockForUpdate(tx).Where("name = ?", name).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && row.WindowEndMS > 0 && row.LastScannedMS < row.WindowEndMS {
			result = &row
			return nil
		}
		if err == nil && nowMS-row.CompletedAtMS < intervalMS {
			return nil
		}
		previousSpan := row.WindowSpanMS
		previousCompleted := row.CompletedAtMS
		row = TronScanCheckpoint{WindowSpanMS: previousSpan, CompletedAtMS: previousCompleted, Name: name, WindowStartMS: fromMS, WindowEndMS: toMS, LastScannedMS: fromMS - 1, UpdatedAtMS: nowMS}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		result = &row
		return nil
	})
	return result, err
}

func AdvanceTronReconciliation(name string, throughMS, nowMS int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var row TronScanCheckpoint
		if err := lockForUpdate(tx).Where("name = ?", name).First(&row).Error; err != nil {
			return err
		}
		if throughMS < row.LastScannedMS || throughMS > row.WindowEndMS {
			return errors.New("invalid reconciliation progress")
		}
		row.LastScannedMS = throughMS
		row.UpdatedAtMS = nowMS
		if throughMS == row.WindowEndMS {
			row.CompletedAtMS = nowMS
		}
		return tx.Save(&row).Error
	})
}

// Persist subdivision even if a round completes no full window. A fresh
// checkpoint has no successful-update timestamp and cannot enable new orders.
func SetTronScanWindowSpan(name string, fromMS, spanMS int64) error {
	if spanMS < 1 || spanMS > 900000 || fromMS < 0 {
		return errors.New("invalid TRON scan span")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var row TronScanCheckpoint
		err := lockForUpdate(tx).Where("name = ?", name).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = TronScanCheckpoint{Name: name, LastScannedMS: fromMS - 1}
		} else if err != nil {
			return err
		}
		row.WindowSpanMS = spanMS
		return tx.Save(&row).Error
	})
}

// The overlap belongs to a scan cycle, not each retry. Persist its cursor even
// while it is behind the settlement watermark, so busy overlap can finish.
func BeginTronNormalScan(name string, initialFromMS, toMS, overlapMS int64) (*TronScanCheckpoint, error) {
	var result *TronScanCheckpoint
	err := DB.Transaction(func(tx *gorm.DB) error {
		var row TronScanCheckpoint
		err := lockForUpdate(tx).Where("name = ?", name).First(&row).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		if !missing && row.WindowEndMS > 0 && row.CursorMS < row.WindowEndMS {
			result = &row
			return nil
		}
		fromMS := initialFromMS
		if !missing {
			fromMS = row.LastScannedMS - overlapMS
		}
		if fromMS < 0 {
			fromMS = 0
		}
		if fromMS > toMS {
			return errors.New("TRON checkpoint is ahead of safe watermark")
		}
		if missing {
			row.Name = name
			row.LastScannedMS = fromMS - 1
		}
		row.WindowStartMS = fromMS
		row.WindowEndMS = toMS
		row.CursorMS = fromMS - 1
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		result = &row
		return nil
	})
	return result, err
}

func AdvanceTronNormalScan(name string, throughMS, nowMS int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var row TronScanCheckpoint
		if err := lockForUpdate(tx).Where("name = ?", name).First(&row).Error; err != nil {
			return err
		}
		if throughMS < row.CursorMS || throughMS > row.WindowEndMS {
			return errors.New("invalid TRON scan progress")
		}
		row.CursorMS = throughMS
		if throughMS > row.LastScannedMS {
			row.LastScannedMS = throughMS
		}
		row.UpdatedAtMS = nowMS
		return tx.Save(&row).Error
	})
}
