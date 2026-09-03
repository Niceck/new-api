package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateTronPayment_UsesFixedPointAndBoundsTail(t *testing.T) {
	quote, err := calculateTronPayment(
		decimal.RequireFromString("100"),
		decimal.RequireFromString("7.25"),
		50_000_000,
		1234,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(13_791_234), quote.ExpectedUSDTMicros)
	assert.Equal(t, int64(7_250_000), quote.RateCNYMicros)
	assert.Equal(t, 50_000_000, quote.CreditQuota)

	for _, tail := range []int64{0, 10_000} {
		_, err := calculateTronPayment(decimal.NewFromInt(100), decimal.NewFromInt(7), 1, tail)
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
		{name: "zero rate", payCNY: decimal.NewFromInt(1), rateCNY: decimal.Zero, creditQuota: 1},
		{name: "zero quota", payCNY: decimal.NewFromInt(1), rateCNY: decimal.NewFromInt(7), creditQuota: 0},
		{name: "quota overflow", payCNY: decimal.NewFromInt(1), rateCNY: decimal.NewFromInt(7), creditQuota: common.MaxQuota + 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := calculateTronPayment(tc.payCNY, tc.rateCNY, tc.creditQuota, 1)
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
