package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateTronPayment_UsesFixedPointAndKeepsValueNearTarget(t *testing.T) {
	quote, err := calculateTronPayment(
		decimal.RequireFromString("100"),
		decimal.RequireFromString("7.25"),
		50_000_000,
		0,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(13_793_103), quote.ExpectedUSDTMicros)
	assert.Equal(t, int64(7_250_000), quote.RateCNYMicros)
	assert.Equal(t, 50_000_000, quote.CreditQuota)

	smallQuote, err := calculateTronPayment(
		decimal.NewFromInt(1),
		decimal.RequireFromString("6.716106"),
		500_000,
		0,
	)
	require.NoError(t, err)
	actualCNY := decimal.NewFromInt(smallQuote.ExpectedUSDTMicros).
		Div(decimal.NewFromInt(1_000_000)).
		Mul(decimal.RequireFromString("6.716106"))
	assert.True(t, actualCNY.Sub(decimal.NewFromInt(1)).Abs().LessThan(decimal.RequireFromString("0.00001")))

	for _, offset := range []int64{-5_000, 5_000} {
		_, err := calculateTronPayment(decimal.NewFromInt(100), decimal.NewFromInt(7), 1, offset)
		assert.Error(t, err)
	}
}

func TestCalculateTronPayment_RejectsZeroNegativeAndQuotaOverflow(t *testing.T) {
	testCases := []struct {
		name        string
		payCNY      decimal.Decimal
		rateCNY     decimal.Decimal
		creditQuota int
	}{
		{name: "zero payment", payCNY: decimal.Zero, rateCNY: decimal.NewFromInt(7), creditQuota: 1},
		{name: "negative payment", payCNY: decimal.NewFromInt(-1), rateCNY: decimal.NewFromInt(7), creditQuota: 1},
		{name: "payment cannot reserve uniqueness window", payCNY: decimal.RequireFromString("0.000001"), rateCNY: decimal.NewFromInt(1), creditQuota: 1},
		{name: "payment cannot reserve upper uniqueness window", payCNY: decimal.NewFromInt(math.MaxInt64).Div(decimal.NewFromInt(tronUSDTScale)), rateCNY: decimal.NewFromInt(1), creditQuota: 1},
		{name: "zero rate", payCNY: decimal.NewFromInt(1), rateCNY: decimal.Zero, creditQuota: 1},
		{name: "zero quota", payCNY: decimal.NewFromInt(1), rateCNY: decimal.NewFromInt(7), creditQuota: 0},
		{name: "quota overflow", payCNY: decimal.NewFromInt(1), rateCNY: decimal.NewFromInt(7), creditQuota: common.MaxQuota + 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := calculateTronPayment(tc.payCNY, tc.rateCNY, tc.creditQuota, 0)
			assert.Error(t, err)
		})
	}
}

func TestCalculateClaimQuota_IsProportionalAndNeverOverflows(t *testing.T) {
	quota, err := calculateClaimQuota(10_000_000, 15_000_000, 100)
	require.NoError(t, err)
	assert.Equal(t, 150, quota)

	quota, err = calculateClaimQuota(3, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, 33, quota)

	for _, tc := range []struct {
		expected int64
		actual   int64
		quota    int
	}{
		{expected: 0, actual: 1, quota: 1},
		{expected: 1, actual: 0, quota: 1},
		{expected: 1, actual: 1, quota: 0},
		{expected: 1, actual: int64(common.MaxQuota) + 1, quota: common.MaxQuota},
	} {
		_, err := calculateClaimQuota(tc.expected, tc.actual, tc.quota)
		assert.Error(t, err)
	}
}

func TestCalculateClaimQuota_UsesCentralQuotaConversionWithoutClamp(t *testing.T) {
	quota, err := calculateClaimQuota(7_000_000, 7_000_000, common.MaxQuota-1)
	require.NoError(t, err)
	assert.Equal(t, common.MaxQuota-1, quota)
}
