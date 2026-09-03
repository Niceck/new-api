package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

const tronScanCheckpointName = "tron_usdt_mainnet"

var tronScannerStartOnce sync.Once

type TronScanSummary struct {
	Seen      int `json:"seen"`
	Credited  int `json:"credited"`
	Unmatched int `json:"unmatched"`
	Review    int `json:"review"`
}

func (s *tronTopupService) ScanOnce(ctx context.Context) (TronScanSummary, error) {
	if !s.config.Enabled || s.chainClient == nil || s.now == nil {
		return TronScanSummary{}, errors.New("TRON top-up scanner is unavailable")
	}
	nowMS := s.now().UnixMilli()
	toMS := nowMS - s.config.IndexSafetyLag.Milliseconds()
	if toMS <= 0 {
		return TronScanSummary{}, errors.New("invalid TRON scan watermark")
	}
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	if err != nil {
		return TronScanSummary{}, err
	}
	fromMS := toMS - s.config.InitialLookback.Milliseconds()
	if checkpoint != nil {
		fromMS = checkpoint.LastScannedMS - s.config.CheckpointOverlap.Milliseconds()
		if fromMS < 0 {
			fromMS = 0
		}
	}
	runWideReconciliation := s.shouldRunWideReconciliation(nowMS)
	if runWideReconciliation {
		reconcileFromMS := toMS - s.config.ReconcileLookback.Milliseconds()
		if reconcileFromMS < fromMS {
			fromMS = reconcileFromMS
			if fromMS < 0 {
				fromMS = 0
			}
		}
	}
	transfers, err := s.chainClient.ConfirmedIncoming(ctx, fromMS, toMS)
	if err != nil {
		return TronScanSummary{}, err
	}

	summary := TronScanSummary{Seen: len(transfers)}
	for _, transfer := range transfers {
		record := model.TronTransferRecord{TxID: transfer.TxID, BlockTimestampMS: transfer.BlockTimestampMS, FromAddress: transfer.From, ToAddress: transfer.To, TokenContract: tronUSDTContract, AmountMicros: transfer.AmountMicros, ObservedAtMS: nowMS}
		result, settleErr := model.SettleTronDeposit(record, "tron-scanner")
		if settleErr == nil {
			if result.Credited && !result.AlreadyProcessed {
				summary.Credited++
			} else if result.NeedsReview && !result.AlreadyProcessed {
				summary.Review++
			}
			continue
		}
		if errors.Is(settleErr, model.ErrTronOrderNotFound) || errors.Is(settleErr, model.ErrTronTransferPredatesOrder) {
			deposit, err := model.RecordUnmatchedTronDeposit(record)
			if err != nil {
				return TronScanSummary{}, err
			}
			if deposit.Status == model.TronDepositStatusUnmatched {
				summary.Unmatched++
			}
			continue
		}
		if !errors.Is(settleErr, model.ErrTronCreditReviewRequired) {
			return TronScanSummary{}, settleErr
		}
		needsReview, err := model.RecordFailedTronSettlement(record)
		if err != nil {
			return TronScanSummary{}, fmt.Errorf("record failed TRON settlement: %w", err)
		}
		if needsReview {
			summary.Review++
		} else {
			summary.Unmatched++
		}
	}
	if err := model.AdvanceTronScanCheckpoint(tronScanCheckpointName, toMS, toMS); err != nil {
		return TronScanSummary{}, err
	}
	if runWideReconciliation {
		s.markWideReconciliationComplete(nowMS)
	}
	return summary, nil
}

func (s *tronTopupService) shouldRunWideReconciliation(nowMS int64) bool {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	return s.lastReconcileMS == 0 || nowMS-s.lastReconcileMS >= s.config.ReconcileInterval.Milliseconds()
}

func (s *tronTopupService) markWideReconciliationComplete(nowMS int64) {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	if nowMS > s.lastReconcileMS {
		s.lastReconcileMS = nowMS
	}
}

func shouldStartTronScanner(isMaster bool, config TronTopupConfig) bool {
	return isMaster && config.Enabled
}

func StartTronTopupScanner(ctx context.Context) {
	service, err := GetDefaultTronTopupService()
	if err != nil || !shouldStartTronScanner(common.IsMasterNode, service.config) {
		return
	}
	tronScannerStartOnce.Do(func() {
		gopool.Go(func() {
			interval := service.config.ScanInterval
			for {
				_, scanErr := service.ScanOnce(ctx)
				if scanErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("TRON top-up scan failed: %v", scanErr))
					interval *= 2
					if interval > 5*time.Minute {
						interval = 5 * time.Minute
					}
				} else {
					interval = service.config.ScanInterval
				}
				timer := time.NewTimer(interval + tronBackoffJitter())
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					return
				case <-timer.C:
				}
			}
		})
	})
}

func tronBackoffJitter() time.Duration {
	value, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64()) * time.Millisecond
}
