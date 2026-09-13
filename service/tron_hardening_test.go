package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTronHardening_RejectsNewOrderWithoutScannerCheckpoint(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9401)
	now := time.UnixMilli(1_800_000_000_000)
	s := newTronTopupService(validTronTestConfig(), &stubTronPriceClient{rate: decimal.NewFromInt(7), updatedAt: now}, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) { return 1, nil })
	_, err := s.CreateOrder(context.Background(), 9401, 10, decimal.NewFromInt(10), 1000)
	require.Error(t, err)
}

func TestTronHardening_SecondPaymentDoesNotStopFollowingDeposit(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9402)
	now := time.UnixMilli(1_800_000_000_000)
	require.NoError(t, model.AdvanceTronScanCheckpoint(tronScanCheckpointName, now.Add(-3*time.Minute).UnixMilli(), now.UnixMilli()))
	chain := &stubTronChainClient{}
	s := newTronTopupService(validTronTestConfig(), &stubTronPriceClient{rate: decimal.NewFromInt(7), updatedAt: now}, chain, func() time.Time { return now }, func() (int64, error) { return 1, nil })
	order, err := s.CreateOrder(context.Background(), 9402, 10, decimal.NewFromInt(10), 1000)
	require.NoError(t, err)
	now = now.Add(5 * time.Minute)
	chain.transfers = []TronTransfer{
		{TxID: strings.Repeat("a", 64), BlockTimestampMS: now.Add(-4 * time.Minute).UnixMilli(), From: "sender", To: s.config.ReceiveAddress, AmountMicros: order.ExpectedUSDTMicros},
		{TxID: strings.Repeat("b", 64), BlockTimestampMS: now.Add(-4 * time.Minute).UnixMilli(), From: "sender", To: s.config.ReceiveAddress, AmountMicros: order.ExpectedUSDTMicros},
		{TxID: strings.Repeat("c", 64), BlockTimestampMS: now.Add(-4 * time.Minute).UnixMilli(), From: "sender", To: s.config.ReceiveAddress, AmountMicros: 100},
	}
	_, err = s.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1000), getTronServiceUserQuota(t, 9402))
	var count int64
	require.NoError(t, model.DB.Model(&model.TronDeposit{}).Count(&count).Error)
	assert.Equal(t, int64(3), count)
}

func TestTronHardening_PriceCacheUsesFreshQuoteAndRetriesFailures(t *testing.T) {
	now := time.Unix(1800000000, 0)
	source := &stubTronPriceClient{rate: decimal.NewFromInt(7), updatedAt: now}
	cache := newCachedTronPriceClient(source, func() time.Time { return now })
	_, _, err := cache.USDTToCNY(context.Background())
	require.NoError(t, err)
	_, _, err = cache.USDTToCNY(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), source.calls.Load())
	now = now.Add(61 * time.Second)
	source.err = errors.New("rate limit")
	_, _, err = cache.USDTToCNY(context.Background())
	require.Error(t, err)
	_, _, err = cache.USDTToCNY(context.Background())
	require.Error(t, err)
	assert.Equal(t, int32(2), source.calls.Load())
	now = now.Add(11 * time.Second)
	source.err = nil
	source.updatedAt = now
	_, _, err = cache.USDTToCNY(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(3), source.calls.Load())
	now = now.Add(7 * time.Minute)
	_, _, err = cache.USDTToCNY(context.Background())
	require.Error(t, err)
}

type segmentedTronChain struct {
	windows        [][2]int64
	failWide       bool
	narrowBoundary int64
}

func (c *segmentedTronChain) ConfirmedIncoming(_ context.Context, from, to int64) ([]TronTransfer, error) {
	c.windows = append(c.windows, [2]int64{from, to})
	if c.failWide && from < c.narrowBoundary {
		return nil, errors.New("wide interrupted")
	}
	if to-from > 60_000 {
		return nil, ErrTronPaginationLimit
	}
	return nil, nil
}

func TestTronHardening_WideFailurePreservesWindowAndDoesNotBlockLatestCheckpoint(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1800000000000)
	chain := &segmentedTronChain{failWide: true, narrowBoundary: now.Add(-15 * time.Minute).UnixMilli()}
	s := newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	_, err := s.ScanOnce(context.Background())
	require.ErrorContains(t, err, "wide interrupted")
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-3*time.Minute).UnixMilli(), checkpoint.LastScannedMS)
	wide, err := model.GetTronScanCheckpoint(tronReconcileCheckpointName)
	require.NoError(t, err)
	require.NotNil(t, wide)
	assert.Zero(t, wide.CompletedAtMS)
	originalEnd := wide.WindowEndMS
	now = now.Add(time.Minute)
	chain.failWide = false
	// Recreate the service to model process restart with no in-memory progress.
	s = newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	_, err = s.ScanOnce(context.Background())
	require.NoError(t, err)
	wide, err = model.GetTronScanCheckpoint(tronReconcileCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, originalEnd, wide.WindowEndMS)
	assert.Equal(t, originalEnd, wide.LastScannedMS)
	assert.Equal(t, now.UnixMilli(), wide.CompletedAtMS)
}

type slowWindowTronChain struct{ attempts int }

func (c *slowWindowTronChain) ConfirmedIncoming(_ context.Context, from, to int64) ([]TronTransfer, error) {
	c.attempts++
	if to-from > 60_000 {
		return nil, context.DeadlineExceeded
	}
	return nil, nil
}
func TestTronHardening_WindowDeadlineSplitsWithoutLosingProgress(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1800000000000)
	chain := &slowWindowTronChain{}
	s := newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	_, err := s.ScanOnce(context.Background())
	require.NoError(t, err)
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-3*time.Minute).UnixMilli(), checkpoint.LastScannedMS)
}

type deadlineAcrossRoundsChain struct {
	calls   int
	cancel  context.CancelFunc
	spans   []int64
	succeed bool
}

func (c *deadlineAcrossRoundsChain) ConfirmedIncoming(ctx context.Context, from, to int64) ([]TronTransfer, error) {
	c.calls++
	c.spans = append(c.spans, to-from+1)
	if c.succeed {
		return nil, nil
	}
	<-ctx.Done()
	if c.calls == 2 {
		c.cancel()
	}
	return nil, ctx.Err()
}
func TestTronHardening_SubdivisionSurvivesExpiredRoundAndRestart(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1800000000000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chain := &deadlineAcrossRoundsChain{cancel: cancel}
	s := newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	s.windowTimeout = time.Millisecond
	_, err := s.ScanOnce(ctx)
	require.Error(t, err)
	require.Len(t, chain.spans, 2)
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	require.NotNil(t, checkpoint)
	assert.Zero(t, checkpoint.UpdatedAtMS)
	assert.Less(t, checkpoint.WindowSpanMS, chain.spans[0])
	savedSpan := checkpoint.WindowSpanMS
	chain.succeed = true
	chain.spans = nil
	s = newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	_, err = s.ScanOnce(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, chain.spans)
	assert.Equal(t, savedSpan, chain.spans[0])
	checkpoint, err = model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-3*time.Minute).UnixMilli(), checkpoint.LastScannedMS)
}

type overlapResumeChain struct {
	calls     int
	cancel    context.CancelFunc
	seen      []int64
	interrupt bool
}

func (c *overlapResumeChain) ConfirmedIncoming(_ context.Context, from, to int64) ([]TronTransfer, error) {
	c.calls++
	c.seen = append(c.seen, from)
	if c.interrupt && c.calls == 2 {
		c.cancel()
		return nil, context.Canceled
	}
	return nil, nil
}
func TestTronHardening_CompletedOverlapWindowsSurviveRestart(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1800000000000)
	oldWatermark := now.Add(-4 * time.Minute).UnixMilli()
	require.NoError(t, model.AdvanceTronScanCheckpoint(tronScanCheckpointName, oldWatermark, now.Add(-time.Minute).UnixMilli()))
	require.NoError(t, model.SetTronScanWindowSpan(tronScanCheckpointName, oldWatermark, 30000))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chain := &overlapResumeChain{cancel: cancel, interrupt: true}
	s := newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	_, err := s.ScanOnce(ctx)
	require.Error(t, err)
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, oldWatermark, checkpoint.LastScannedMS)
	assert.Greater(t, checkpoint.CursorMS, checkpoint.WindowStartMS)
	next := checkpoint.CursorMS + 1
	chain.interrupt = false
	chain.seen = nil
	s = newTronTopupService(validTronTestConfig(), nil, chain, func() time.Time { return now }, nil)
	_, err = s.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, next, chain.seen[0])
	checkpoint, err = model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-3*time.Minute).UnixMilli(), checkpoint.LastScannedMS)
}
