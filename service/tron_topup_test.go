package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTronPriceClient struct {
	rate      decimal.Decimal
	updatedAt time.Time
	err       error
	calls     atomic.Int32
}

func (s *stubTronPriceClient) USDTToCNY(_ context.Context) (decimal.Decimal, time.Time, error) {
	s.calls.Add(1)
	return s.rate, s.updatedAt, s.err
}

type stubTronChainClient struct {
	transfers []TronTransfer
	err       error
	fromMS    int64
	toMS      int64
	calls     int
}

func (s *stubTronChainClient) ConfirmedIncoming(_ context.Context, fromMS, toMS int64) ([]TronTransfer, error) {
	s.calls++
	s.fromMS = fromMS
	s.toMS = toMS
	return s.transfers, s.err
}

func validTronTestConfig() TronTopupConfig {
	return TronTopupConfig{
		Enabled:           true,
		Network:           "mainnet",
		ReceiveAddress:    "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f",
		TronGridBaseURL:   "https://api.trongrid.io",
		CoinGeckoBaseURL:  "https://api.coingecko.com",
		OrderTTL:          20 * time.Minute,
		ScanInterval:      30 * time.Second,
		InitialLookback:   10 * time.Minute,
		CheckpointOverlap: 2 * time.Minute,
		IndexSafetyLag:    3 * time.Minute,
		ReconcileLookback: 25 * time.Hour,
		ReconcileInterval: time.Hour,
		MaxCNY:            2_000,
	}
}

func seedTronServiceUser(t *testing.T, id int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: id, Username: "tron-service-user", AffCode: "tron-service-aff", Status: common.UserStatusEnabled}).Error)
}

func TestLoadTronTopupConfig_RequiresEnabledValidMainnetAddress(t *testing.T) {
	t.Setenv("TRON_TOPUP_ENABLED", "true")
	t.Setenv("TRON_NETWORK", "mainnet")
	t.Setenv("TRON_RECEIVE_ADDRESS", "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f")
	t.Setenv("TRONGRID_API_KEY", "tron-test-key")
	t.Setenv("COINGECKO_API_KEY", "coingecko-test-key")

	config, err := loadTronTopupConfig()
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.Equal(t, 20*time.Minute, config.OrderTTL)
	assert.Equal(t, 30*time.Second, config.ScanInterval)
	assert.Equal(t, int64(2_000), config.MaxCNY)

	t.Setenv("TRON_RECEIVE_ADDRESS", "bad")
	_, err = loadTronTopupConfig()
	assert.Error(t, err)

	t.Setenv("TRON_TOPUP_ENABLED", "false")
	config, err = loadTronTopupConfig()
	require.NoError(t, err)
	assert.False(t, config.Enabled)
}

func TestLoadTronTopupConfig_RejectsMissingReadAPIKeys(t *testing.T) {
	for _, missing := range []string{"TRONGRID_API_KEY", "COINGECKO_API_KEY"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("TRON_TOPUP_ENABLED", "true")
			t.Setenv("TRON_NETWORK", "mainnet")
			t.Setenv("TRON_RECEIVE_ADDRESS", "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f")
			t.Setenv("TRONGRID_API_KEY", "tron-test-key")
			t.Setenv("COINGECKO_API_KEY", "coingecko-test-key")
			t.Setenv(missing, "")

			_, err := loadTronTopupConfig()
			assert.Error(t, err)
		})
	}
}

func TestTronTopupService_CreateOrderLocksFreshRateAndReusesActiveOrder(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9101)
	now := time.UnixMilli(1_800_000_000_000)
	price := &stubTronPriceClient{rate: decimal.RequireFromString("7.25"), updatedAt: now.Add(-20 * time.Second)}
	tails := []int64{1234}
	service := newTronTopupService(validTronTestConfig(), price, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) {
		tail := tails[0]
		tails = tails[1:]
		return tail, nil
	})

	first, err := service.CreateOrder(context.Background(), 9101, 100, decimal.NewFromInt(100), 50_000_000)
	require.NoError(t, err)
	assert.Equal(t, int64(13_791_234), first.ExpectedUSDTMicros)
	assert.Equal(t, int64(7_250_000), first.RateCNYMicros)
	assert.Equal(t, now.Add(20*time.Minute).UnixMilli(), first.ExpiresAtMS)

	second, err := service.CreateOrder(context.Background(), 9101, 999, decimal.NewFromInt(999), 1)
	require.NoError(t, err)
	assert.Equal(t, first.TradeNo, second.TradeNo)
	assert.Equal(t, int32(1), price.calls.Load())
	var generic model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", first.TradeNo).First(&generic).Error)
	assert.Equal(t, 100.0, generic.Money)
}

func TestLoadTronTopupConfig_RejectsMalformedNumericSafetyValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "malformed ttl", key: "TRON_TOPUP_ORDER_TTL_MINUTES", value: "twenty"},
		{name: "whitespace ttl", key: "TRON_TOPUP_ORDER_TTL_MINUTES", value: " 20 "},
		{name: "overflow interval", key: "TRON_TOPUP_SCAN_INTERVAL_SECONDS", value: "999999999999999999999"},
		{name: "malformed max", key: "TRON_TOPUP_MAX_CNY", value: "100x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRON_TOPUP_ENABLED", "true")
			t.Setenv("TRON_NETWORK", "mainnet")
			t.Setenv("TRON_RECEIVE_ADDRESS", "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f")
			t.Setenv("TRONGRID_API_KEY", "tron-test-key")
			t.Setenv("COINGECKO_API_KEY", "coingecko-test-key")
			t.Setenv(tc.key, tc.value)
			_, err := loadTronTopupConfig()
			assert.Error(t, err)
		})
	}
}

func TestTronTopupService_ConcurrentSameUserCreateReturnsOneActiveOrder(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9104)
	now := time.UnixMilli(1_800_000_000_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	var nextTail atomic.Int64
	nextTail.Store(100)
	service := newTronTopupService(validTronTestConfig(), price, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) {
		return nextTail.Add(1), nil
	})

	start := make(chan struct{})
	results := make(chan TronOrderView, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			order, err := service.CreateOrder(context.Background(), 9104, 100, decimal.NewFromInt(100), 100)
			results <- order
			errorsFound <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}
	orders := make([]TronOrderView, 0, 2)
	for order := range results {
		orders = append(orders, order)
	}
	require.Len(t, orders, 2)
	assert.Equal(t, orders[0].TradeNo, orders[1].TradeNo)
	var count int64
	require.NoError(t, model.DB.Model(&model.TronTopupOrder{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestTronTopupService_CreateOrderRetriesAmountCollision(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9102)
	now := time.UnixMilli(1_800_000_000_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	service := newTronTopupService(validTronTestConfig(), price, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) { return 100, nil })

	first, err := service.CreateOrder(context.Background(), 9102, 100, decimal.NewFromInt(100), 100)
	require.NoError(t, err)
	assert.Equal(t, int64(10_000_100), first.ExpectedUSDTMicros)

	// Mark the first order expired so a new order is eligible while its globally
	// unique amount remains reserved forever.
	require.NoError(t, model.DB.Model(&model.TronTopupOrder{}).Where("trade_no = ?", first.TradeNo).Update("expires_at_ms", now.Add(-time.Second).UnixMilli()).Error)
	second, err := service.CreateOrder(context.Background(), 9102, 101, decimal.NewFromInt(100), 100)
	require.NoError(t, err)
	assert.Equal(t, int64(10_000_101), second.ExpectedUSDTMicros)
}

func TestTronTopupService_CreateOrderFailsClosedOnPriceAndQuotaBounds(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9103)
	now := time.UnixMilli(1_800_000_000_000)
	price := &stubTronPriceClient{err: errors.New("price unavailable")}
	service := newTronTopupService(validTronTestConfig(), price, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) { return 1, nil })

	_, err := service.CreateOrder(context.Background(), 9103, 100, decimal.NewFromInt(100), 100)
	assert.Error(t, err)
	_, err = service.CreateOrder(context.Background(), 9103, 2_001, decimal.NewFromInt(2_001), 100)
	assert.Error(t, err)
	_, err = service.CreateOrder(context.Background(), 9103, 100, decimal.NewFromInt(100), common.MaxQuota+1)
	assert.Error(t, err)

	var count int64
	require.NoError(t, model.DB.Model(&model.TronTopupOrder{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestTronTopupService_DisabledModeKeepsExistingOrderRecoveryAvailable(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9105)
	now := time.UnixMilli(1_800_000_000_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	enabled := newTronTopupService(validTronTestConfig(), price, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) { return 700, nil })
	order, err := enabled.CreateOrder(context.Background(), 9105, 100, decimal.NewFromInt(100), 100)
	require.NoError(t, err)

	disabledConfig := validTronTestConfig()
	disabledConfig.Enabled = false
	disabled := newTronTopupService(disabledConfig, nil, nil, func() time.Time { return now }, nil)
	view, err := disabled.GetOrder(9105, order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, order.TradeNo, view.TradeNo)
	ticket, err := disabled.SubmitClaim(9105, order.TradeNo, "3"+strings.Repeat("0", 63), "recovery while disabled")
	require.NoError(t, err)
	err = disabled.ResolveTicket(context.Background(), ticket.ID, 1, 1, "must verify chain")
	assert.ErrorIs(t, err, ErrTronChainVerificationUnavailable)
	assert.Equal(t, int64(0), getTronServiceUserQuota(t, 9105))
	status, err := disabled.AdminStatus()
	require.NoError(t, err)
	assert.False(t, status.Enabled)
}
