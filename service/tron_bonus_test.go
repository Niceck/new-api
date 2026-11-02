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

// USDT 充值的赠送：订单的 CreditQuota 含赠送，链上到账后整笔入账；赠送额快照落在 top_ups 上供展示与对账。
func TestTronTopupScanner_CreditsFacePlusBonus(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9301)
	now := time.UnixMilli(1_800_000_600_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	chain := &stubTronChainClient{}
	service := newTronTestService(t, validTronTestConfig(), price, chain, func() time.Time { return now }, func() (int64, error) { return 321, nil })

	order, err := service.CreateOrderWithBonus(context.Background(), 9301, 100, decimal.NewFromInt(100), 51_750_000, 1_750_000)
	require.NoError(t, err)
	assert.Equal(t, 51_750_000, order.CreditQuota)

	topUp := model.GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, topUp)
	assert.Equal(t, int64(100), topUp.Amount, "面值不含赠送，累计充值只算它")
	assert.Equal(t, int64(1_750_000), topUp.BonusQuota)

	chain.transfers = []TronTransfer{
		{TxID: strings.Repeat("c", 64), BlockTimestampMS: now.Add(time.Minute).UnixMilli(), From: "from-c", To: service.config.ReceiveAddress, AmountMicros: order.ExpectedUSDTMicros},
	}
	now = now.Add(5 * time.Minute)
	summary, err := service.ScanOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Credited)
	assert.Equal(t, int64(51_750_000), getTronServiceUserQuota(t, 9301))
}

func TestCreateOrder_WithoutBonusStaysZero(t *testing.T) {
	truncate(t)
	seedTronServiceUser(t, 9302)
	now := time.UnixMilli(1_800_000_600_000)
	price := &stubTronPriceClient{rate: decimal.NewFromInt(10), updatedAt: now}
	service := newTronTestService(t, validTronTestConfig(), price, &stubTronChainClient{}, func() time.Time { return now }, func() (int64, error) { return 322, nil })

	order, err := service.CreateOrder(context.Background(), 9302, 100, decimal.NewFromInt(100), 50_000_000)
	require.NoError(t, err)
	topUp := model.GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, topUp)
	assert.Zero(t, topUp.BonusQuota)
}
