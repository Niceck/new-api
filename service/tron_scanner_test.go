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
	"gorm.io/gorm"
)

func TestTronTopupScanner_CreditsMatchRecordsUnmatchedAndAdvancesCheckpoint(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9201)
	now := time.UnixMilli(1_800_000_600_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	chain := &stubTronChainClient{}
	service := newTronTopupService(validTronTestConfig(), price, chain, func() time.Time { return now }, func() (int64, error) { return 321, nil })
	order, err := service.CreateOrder(context.Background(), 9201, 100, decimal.NewFromInt(100), 1_000)
	require.NoError(t, err)
	chain.transfers = []TronTransfer{
		{TxID: strings.Repeat("a", 64), BlockTimestampMS: now.Add(time.Minute).UnixMilli(), From: "from-a", To: service.config.ReceiveAddress, AmountMicros: order.ExpectedUSDTMicros},
		{TxID: strings.Repeat("b", 64), BlockTimestampMS: now.Add(time.Minute).UnixMilli(), From: "from-b", To: service.config.ReceiveAddress, AmountMicros: 99_999_999},
	}
	now = now.Add(5 * time.Minute)

	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Credited)
	assert.Equal(t, 1, summary.Unmatched)
	assert.Equal(t, int64(1_000), getTronServiceUserQuota(t, 9201))

	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	require.NotNil(t, checkpoint)
	watermark := now.Add(-service.config.IndexSafetyLag)
	assert.Equal(t, watermark.UnixMilli(), checkpoint.LastScannedMS)
	assert.Equal(t, watermark.Add(-service.config.ReconcileLookback).UnixMilli(), chain.fromMS)
}

func TestTronTopupScanner_DoesNotAdvanceCheckpointWhenChainReadFails(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1_800_000_600_000)
	require.NoError(t, model.AdvanceTronScanCheckpoint(tronScanCheckpointName, now.Add(-time.Minute).UnixMilli(), now.Add(-time.Minute).UnixMilli()))
	chain := &stubTronChainClient{err: errors.New("temporary failure")}
	service := newTronTopupService(validTronTestConfig(), &stubTronPriceClient{}, chain, func() time.Time { return now }, func() (int64, error) { return 1, nil })

	_, err := service.ScanOnce(context.Background())
	assert.Error(t, err)
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-time.Minute).UnixMilli(), checkpoint.LastScannedMS)
	assert.Equal(t, now.Add(-service.config.IndexSafetyLag-service.config.ReconcileLookback).UnixMilli(), chain.fromMS)
}

func TestShouldStartTronScanner_RequiresMasterAndEnabledConfig(t *testing.T) {
	config := validTronTestConfig()
	assert.True(t, shouldStartTronScanner(true, config))
	assert.False(t, shouldStartTronScanner(false, config))
	config.Enabled = false
	assert.False(t, shouldStartTronScanner(true, config))
}

func getTronServiceUserQuota(t *testing.T, userID int) int64 {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return int64(user.Quota)
}

func TestTronTopupScanner_ConfirmedAtExpiryBoundaryCredits(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9202)
	now := time.UnixMilli(1_800_001_000_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	chain := &stubTronChainClient{}
	service := newTronTopupService(validTronTestConfig(), price, chain, func() time.Time { return now }, func() (int64, error) { return 400, nil })
	order, err := service.CreateOrder(context.Background(), 9202, 100, decimal.NewFromInt(100), 1_000)
	require.NoError(t, err)
	chain.transfers = []TronTransfer{{TxID: strings.Repeat("c", 64), BlockTimestampMS: order.ExpiresAtMS, From: "from-c", To: service.config.ReceiveAddress, AmountMicros: order.ExpectedUSDTMicros}}
	now = now.Add(21 * time.Minute)

	_, err = service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1_000), getTronServiceUserQuota(t, 9202))
}

func TestTronTopupScanner_WideReconciliationCatchesDelayedIndexRecord(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1_800_010_000_000)
	require.NoError(t, model.AdvanceTronScanCheckpoint(tronScanCheckpointName, now.Add(-4*time.Minute).UnixMilli(), now.UnixMilli()))
	chain := &stubTronChainClient{}
	service := newTronTopupService(validTronTestConfig(), &stubTronPriceClient{}, chain, func() time.Time { return now }, func() (int64, error) { return 1, nil })

	_, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.LessOrEqual(t, chain.fromMS, now.Add(-10*time.Minute).UnixMilli())
	assert.Equal(t, now.Add(-service.config.IndexSafetyLag).UnixMilli(), chain.toMS)
}

func TestTronTopupScanner_UsesNarrowCheckpointWindowBetweenHourlyReconciliations(t *testing.T) {
	truncate(t)
	now := time.UnixMilli(1_800_015_000_000)
	chain := &stubTronChainClient{}
	service := newTronTopupService(validTronTestConfig(), &stubTronPriceClient{}, chain, func() time.Time { return now }, func() (int64, error) { return 1, nil })

	_, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	firstWatermark := now.Add(-service.config.IndexSafetyLag)
	assert.Equal(t, firstWatermark.Add(-service.config.ReconcileLookback).UnixMilli(), chain.fromMS)

	now = now.Add(30 * time.Second)
	_, err = service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, firstWatermark.Add(-service.config.CheckpointOverlap).UnixMilli(), chain.fromMS)
}

func TestTronTopupScanner_UnknownDatabaseFailureKeepsCheckpointAndDoesNotCreateReview(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9203)
	now := time.UnixMilli(1_800_020_000_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	chain := &stubTronChainClient{}
	service := newTronTopupService(validTronTestConfig(), price, chain, func() time.Time { return now }, func() (int64, error) { return 500, nil })
	order, err := service.CreateOrder(context.Background(), 9203, 100, decimal.NewFromInt(100), 1_000)
	require.NoError(t, err)
	chain.transfers = []TronTransfer{{TxID: strings.Repeat("d", 64), BlockTimestampMS: order.ExpiresAtMS, From: "from-d", To: service.config.ReceiveAddress, AmountMicros: order.ExpectedUSDTMicros}}
	now = now.Add(24 * time.Minute)

	callbackName := "tron-test-user-update-failure"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("injected database failure"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })

	_, err = service.ScanOnce(context.Background())
	assert.ErrorContains(t, err, "injected database failure")
	checkpoint, err := model.GetTronScanCheckpoint(tronScanCheckpointName)
	require.NoError(t, err)
	assert.Nil(t, checkpoint)
	var depositCount int64
	var ticketCount int64
	require.NoError(t, model.DB.Model(&model.TronDeposit{}).Count(&depositCount).Error)
	require.NoError(t, model.DB.Model(&model.TronTopupTicket{}).Count(&ticketCount).Error)
	assert.Zero(t, depositCount)
	assert.Zero(t, ticketCount)
}
