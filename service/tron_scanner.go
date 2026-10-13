package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"
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

const tronReconcileCheckpointName = "tron_usdt_mainnet_reconcile"

func (s *tronTopupService) ScanOnce(ctx context.Context) (TronScanSummary, error) {
	if !s.config.Enabled || s.chainClient == nil || s.now == nil {
		return TronScanSummary{}, errors.New("TRON top-up scanner is unavailable")
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	nowMS := s.now().UnixMilli()
	toMS := nowMS - s.config.IndexSafetyLag.Milliseconds()
	if toMS <= 0 {
		return TronScanSummary{}, errors.New("invalid TRON scan watermark")
	}
	normal, err := model.BeginTronNormalScan(tronScanCheckpointName, toMS-s.config.InitialLookback.Milliseconds(), toMS, s.config.CheckpointOverlap.Milliseconds())
	if err != nil {
		return TronScanSummary{}, err
	}
	summary := TronScanSummary{}
	err = s.scanTronWindows(ctx, tronScanCheckpointName, normal.CursorMS+1, normal.WindowEndMS, &summary, func(throughMS int64) error {
		return model.AdvanceTronNormalScan(tronScanCheckpointName, throughMS, s.now().UnixMilli())
	})
	if err != nil {
		return summary, err
	}
	wideFrom := toMS - s.config.ReconcileLookback.Milliseconds()
	if wideFrom < 0 {
		wideFrom = 0
	}
	wide, err := model.BeginTronReconciliation(tronReconcileCheckpointName, wideFrom, toMS, nowMS, s.config.ReconcileInterval.Milliseconds())
	if err != nil {
		return summary, err
	}
	if wide != nil {
		err = s.scanTronWindows(ctx, tronReconcileCheckpointName, wide.LastScannedMS+1, wide.WindowEndMS, &summary, func(throughMS int64) error {
			return model.AdvanceTronReconciliation(tronReconcileCheckpointName, throughMS, s.now().UnixMilli())
		})
	}
	return summary, err
}

// Windows are inclusive; only completed windows can advance durable progress.
func (s *tronTopupService) scanTronWindows(ctx context.Context, progressName string, fromMS, toMS int64, summary *TronScanSummary, advance func(int64) error) error {
	if fromMS > toMS {
		return errors.New("TRON scan checkpoint is ahead of safe watermark")
	}
	progress, err := model.GetTronScanCheckpoint(progressName)
	if err != nil {
		return err
	}
	spanMS := int64(900000)
	if progress != nil && progress.WindowSpanMS > 0 && progress.WindowSpanMS < spanMS {
		spanMS = progress.WindowSpanMS
	}
	for fromMS <= toMS {
		if err := ctx.Err(); err != nil {
			return err
		}
		endMS := fromMS + spanMS - 1
		if endMS > toMS {
			endMS = toMS
		}
		var transfers []TronTransfer
		for {
			var err error
			windowCtx, cancelWindow := context.WithTimeout(ctx, s.windowTimeout)
			transfers, err = s.chainClient.ConfirmedIncoming(windowCtx, fromMS, endMS)
			cancelWindow()
			if (errors.Is(err, ErrTronPaginationLimit) || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() == nil && endMS > fromMS {
				endMS = fromMS + (endMS-fromMS)/2
				spanMS = endMS - fromMS + 1
				if err := model.SetTronScanWindowSpan(progressName, fromMS, spanMS); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			break
		}
		if err := s.settleTronWindow(ctx, transfers, fromMS, endMS, summary); err != nil {
			return err
		}
		if err := advance(endMS); err != nil {
			return err
		}
		if spanMS < 900000 {
			spanMS *= 2
			if spanMS > 900000 {
				spanMS = 900000
			}
			if err := model.SetTronScanWindowSpan(progressName, fromMS, spanMS); err != nil {
				return err
			}
		}
		fromMS = endMS + 1
	}
	return nil
}

// settleTronWindow classifies the transfers of one completed window. Exact
// matches are settled first and in a stable order, so an inexact payment is
// never judged against an order that an exact payment of the same window is
// about to complete, and the order the chain API returned rows in decides
// nothing.
func (s *tronTopupService) settleTronWindow(ctx context.Context, transfers []TronTransfer, fromMS, endMS int64, summary *TronScanSummary) error {
	ordered := append([]TronTransfer(nil), transfers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].BlockTimestampMS != ordered[j].BlockTimestampMS {
			return ordered[i].BlockTimestampMS < ordered[j].BlockTimestampMS
		}
		return ordered[i].TxID < ordered[j].TxID
	})
	var inexact []model.TronTransferRecord
	for _, transfer := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if transfer.BlockTimestampMS < fromMS || transfer.BlockTimestampMS > endMS {
			return errors.New("TRON transfer outside completed window")
		}
		summary.Seen++
		record := model.TronTransferRecord{TxID: transfer.TxID, BlockTimestampMS: transfer.BlockTimestampMS, FromAddress: transfer.From, ToAddress: transfer.To, TokenContract: tronUSDTContract, AmountMicros: transfer.AmountMicros, ObservedAtMS: s.now().UnixMilli(), SourceAddresses: transfer.SourceAddresses}
		deferred, err := s.settleTronExact(record, summary)
		if err != nil {
			return err
		}
		if deferred {
			inexact = append(inexact, record)
		}
	}
	for i, record := range inexact {
		if err := ctx.Err(); err != nil {
			return err
		}
		peers := make([]model.TronTransferRecord, 0, len(inexact)-1)
		peers = append(peers, inexact[:i]...)
		peers = append(peers, inexact[i+1:]...)
		result, err := model.SettleTronDepositTolerant(record, peers, "tron-scanner")
		if err != nil {
			return err
		}
		if result.AlreadyProcessed {
			continue
		}
		switch {
		case result.Credited:
			summary.Credited++
		case result.NeedsReview:
			summary.Review++
		case result.Unmatched:
			summary.Unmatched++
		}
	}
	return nil
}

// settleTronExact runs the exact-amount path for one transfer. It reports
// deferred when no order has that exact amount and tolerant matching is on, so
// the caller judges the transfer after every exact match of the window.
func (s *tronTopupService) settleTronExact(record model.TronTransferRecord, summary *TronScanSummary) (bool, error) {
	result, err := model.SettleTronDeposit(record, "tron-scanner")
	if err == nil {
		if result.Credited && !result.AlreadyProcessed {
			summary.Credited++
		}
		if result.NeedsReview && !result.AlreadyProcessed {
			summary.Review++
		}
		return false, nil
	}
	if errors.Is(err, model.ErrTronOrderNotFound) && s.config.TolerantMatchEnabled {
		return true, nil
	}
	if errors.Is(err, model.ErrTronOrderNotFound) || errors.Is(err, model.ErrTronTransferPredatesOrder) {
		deposit, err := model.RecordUnmatchedTronDeposit(record)
		if err != nil {
			return false, err
		}
		if deposit.Status == model.TronDepositStatusUnmatched {
			summary.Unmatched++
		}
		return false, nil
	}
	if !errors.Is(err, model.ErrTronCreditReviewRequired) {
		return false, err
	}
	review, err := model.RecordFailedTronSettlement(record)
	if err != nil {
		return false, fmt.Errorf("record TRON review: %w", err)
	}
	if review {
		summary.Review++
	}
	return false, nil
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
