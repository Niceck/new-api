package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 汇率 10、应付 ¥100 → 订单应付恰为 10 USDT，面值额度 1000。
const tolScanExpectedMicros int64 = 10_000_000

func newTolerantScanFixture(t *testing.T, tolerant bool, userID int) (*tronTopupService, *stubTronChainClient, *time.Time, TronOrderView) {
	t.Helper()
	truncate(t)
	seedTronServiceUser(t, userID)
	now := time.UnixMilli(1_800_000_600_000)
	clock := &now
	config := validTronTestConfig()
	config.TolerantMatchEnabled = tolerant
	chain := &stubTronChainClient{}
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	service := newTronTestService(t, config, price, chain, func() time.Time { return *clock }, func() (int64, error) { return 321, nil })
	order, err := service.CreateOrder(context.Background(), userID, 100, decimal.NewFromInt(100), 1_000)
	require.NoError(t, err)
	require.Equal(t, tolScanExpectedMicros, order.ExpectedUSDTMicros)
	return service, chain, clock, order
}

func tolScanTransfer(service *tronTopupService, txChar string, blockTime time.Time, amountMicros int64) TronTransfer {
	return TronTransfer{TxID: strings.Repeat(txChar, 64), BlockTimestampMS: blockTime.UnixMilli(), From: "from-" + txChar, To: service.config.ReceiveAddress, AmountMicros: amountMicros}
}

func tolScanDeposit(t *testing.T, txChar string) model.TronDeposit {
	t.Helper()
	var deposit model.TronDeposit
	require.NoError(t, model.DB.Where("tx_id = ?", strings.Repeat(txChar, 64)).First(&deposit).Error)
	return deposit
}

func TestTronTolerantScan_SwitchOffKeepsInexactPaymentUnmatched(t *testing.T) {
	service, chain, clock, _ := newTolerantScanFixture(t, false, 9301)
	created := *clock
	chain.transfers = []TronTransfer{tolScanTransfer(service, "a", created.Add(113*time.Second), 10_044_000)}
	*clock = created.Add(6 * time.Minute)

	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Credited)
	assert.Equal(t, 1, summary.Unmatched)
	assert.Equal(t, int64(0), getTronServiceUserQuota(t, 9301))
	deposit := tolScanDeposit(t, "a")
	assert.Equal(t, model.TronDepositStatusUnmatched, deposit.Status)
	assert.Empty(t, deposit.MatchNote, "开关关闭时不做容差判定，也不留判定说明")
}

func TestTronTolerantScan_SwitchOnCreditsFaceValueOnceAcrossRescans(t *testing.T) {
	service, chain, clock, _ := newTolerantScanFixture(t, true, 9302)
	created := *clock
	chain.transfers = []TronTransfer{tolScanTransfer(service, "b", created.Add(113*time.Second), 10_044_000)}
	*clock = created.Add(6 * time.Minute)

	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Credited)
	assert.Equal(t, 0, summary.Unmatched)
	assert.Equal(t, int64(1_000), getTronServiceUserQuota(t, 9302), "多付只入账面值")
	deposit := tolScanDeposit(t, "b")
	assert.Equal(t, model.TronDepositStatusCredited, deposit.Status)
	assert.Equal(t, model.TronMatchModeTolerant, deposit.MatchMode)

	// 回叠窗口再次扫到同一笔：不重复入账，也不重复计数。
	*clock = created.Add(7 * time.Minute)
	summary, err = service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Credited)
	assert.Equal(t, int64(1_000), getTronServiceUserQuota(t, 9302))
}

func TestTronTolerantScan_ExactPaymentInTheSameWindowIsSettledFirst(t *testing.T) {
	service, chain, clock, _ := newTolerantScanFixture(t, true, 9303)
	created := *clock
	// 同一张单先到一笔金额不精确的、后到一笔精确的：入账的必须是精确那笔，
	// 不精确那笔留给人工，不能因为遍历顺序在前就抢走订单。
	chain.transfers = []TronTransfer{
		tolScanTransfer(service, "c", created.Add(70*time.Second), 10_050_000),
		tolScanTransfer(service, "d", created.Add(90*time.Second), tolScanExpectedMicros),
	}
	*clock = created.Add(6 * time.Minute)

	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Credited)
	assert.Equal(t, 1, summary.Unmatched)
	assert.Equal(t, int64(1_000), getTronServiceUserQuota(t, 9303))
	exact := tolScanDeposit(t, "d")
	assert.Equal(t, model.TronDepositStatusCredited, exact.Status)
	assert.Equal(t, model.TronMatchModeExact, exact.MatchMode)
	inexact := tolScanDeposit(t, "c")
	assert.Equal(t, model.TronDepositStatusUnmatched, inexact.Status)
	assert.Zero(t, inexact.CreditedQuota)
}

func TestTronTolerantScan_TwoInexactPaymentsForOneOrderBothGoManual(t *testing.T) {
	service, chain, clock, _ := newTolerantScanFixture(t, true, 9304)
	created := *clock
	// 链上接口返回的顺序与区块时间相反，也不影响结论。
	chain.transfers = []TronTransfer{
		tolScanTransfer(service, "f", created.Add(100*time.Second), 9_800_000),
		tolScanTransfer(service, "e", created.Add(70*time.Second), 10_050_000),
	}
	*clock = created.Add(6 * time.Minute)

	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Credited)
	assert.Equal(t, 2, summary.Unmatched)
	assert.Equal(t, int64(0), getTronServiceUserQuota(t, 9304))
	for _, txChar := range []string{"e", "f"} {
		deposit := tolScanDeposit(t, txChar)
		assert.Equal(t, model.TronDepositStatusUnmatched, deposit.Status)
		assert.Equal(t, "tolerant:conflict_deposit", deposit.MatchNote)
	}
}

func TestLoadTronTopupConfig_TolerantMatchSwitchIsStrictAndOffByDefault(t *testing.T) {
	t.Setenv("TRON_TOPUP_ENABLED", "true")
	t.Setenv("TRON_NETWORK", "mainnet")
	t.Setenv("TRON_RECEIVE_ADDRESS", "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f")
	t.Setenv("TRONGRID_API_KEY", "test-trongrid-key")
	t.Setenv("COINGECKO_API_KEY", "test-coingecko-key")

	config, err := loadTronTopupConfig()
	require.NoError(t, err)
	assert.False(t, config.TolerantMatchEnabled, "不配置就是关")

	t.Setenv("TRON_TOPUP_TOLERANT_MATCH_ENABLED", "true")
	config, err = loadTronTopupConfig()
	require.NoError(t, err)
	assert.True(t, config.TolerantMatchEnabled)

	t.Setenv("TRON_TOPUP_TOLERANT_MATCH_ENABLED", "false")
	config, err = loadTronTopupConfig()
	require.NoError(t, err)
	assert.False(t, config.TolerantMatchEnabled)

	// 动钱的开关写错了要启动失败，不能悄悄当成关或开。
	t.Setenv("TRON_TOPUP_TOLERANT_MATCH_ENABLED", "yes please")
	_, err = loadTronTopupConfig()
	require.Error(t, err)
}

func TestTronOrderView_ShowsToleranceRangeAndActualSettlement(t *testing.T) {
	service, chain, clock, order := newTolerantScanFixture(t, true, 9305)
	created := *clock
	assert.True(t, order.TolerantMatch)
	assert.Equal(t, int64(9_000_000), order.TolerantMinUSDTMicros)
	assert.Equal(t, int64(11_000_000), order.TolerantMaxUSDTMicros)
	assert.Zero(t, order.ReceivedUSDTMicros)

	// 少付 5%：按比例入账 950，订单结清；面值与应付保持原值。
	chain.transfers = []TronTransfer{tolScanTransfer(service, "a", created.Add(113*time.Second), 9_500_000)}
	*clock = created.Add(6 * time.Minute)
	_, err := service.ScanOnce(context.Background())
	require.NoError(t, err)

	view, err := service.GetOrder(9305, order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "success", view.Status)
	assert.Equal(t, 1_000, view.CreditQuota)
	assert.Equal(t, tolScanExpectedMicros, view.ExpectedUSDTMicros)
	assert.Equal(t, int64(9_500_000), view.ReceivedUSDTMicros)
	assert.Equal(t, 950, view.CreditedQuota)
	assert.Equal(t, model.TronMatchModeTolerant, view.MatchMode)

	status, err := service.AdminStatus()
	require.NoError(t, err)
	assert.True(t, status.TolerantMatchEnabled)
}

func TestTronOrderView_SwitchOffAdvertisesNoTolerance(t *testing.T) {
	service, _, _, order := newTolerantScanFixture(t, false, 9306)
	assert.False(t, order.TolerantMatch)
	assert.Zero(t, order.TolerantMinUSDTMicros)
	assert.Zero(t, order.TolerantMaxUSDTMicros)
	view, err := service.GetOrder(9306, order.TradeNo)
	require.NoError(t, err)
	assert.False(t, view.TolerantMatch)
	status, err := service.AdminStatus()
	require.NoError(t, err)
	assert.False(t, status.TolerantMatchEnabled)
}

func TestTronTolerantScan_TurningTheSwitchOnNeverRevisitsRecordedDeposits(t *testing.T) {
	service, chain, clock, _ := newTolerantScanFixture(t, false, 9307)
	created := *clock
	chain.transfers = []TronTransfer{tolScanTransfer(service, "a", created.Add(113*time.Second), 10_044_000)}
	*clock = created.Add(6 * time.Minute)
	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, summary.Unmatched)

	// 开关打开后重扫到同一笔：它已经有了分类，不回头自动入账。
	enabled := validTronTestConfig()
	enabled.TolerantMatchEnabled = true
	reopened := newTronTopupService(enabled, &stubTronPriceClient{}, chain, func() time.Time { return *clock }, func() (int64, error) { return 321, nil })
	*clock = created.Add(7 * time.Minute)
	summary, err = reopened.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Credited)
	assert.Equal(t, int64(0), getTronServiceUserQuota(t, 9307))
	assert.Equal(t, model.TronDepositStatusUnmatched, tolScanDeposit(t, "a").Status)
}
