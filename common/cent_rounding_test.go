package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCNYCentRounding(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  int
		rate float64
		want int
	}{
		{"free", 0, 1, 0}, {"tiny", 1, 1, 5000},
		{"below cent", 4999, 1, 5000}, {"exact cent", 5000, 1, 5000},
		{"above cent", 5001, 1, 10000}, {"multiple cents", 12345, 1, 15000},
		{"fractional quota per cent", 1, 7.3, 685},
		{"exact cent at fractional rate", 50000, 7.3, 50000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := CNYCentRounding{Enabled: true, USDToCNY: tc.rate, QuotaPerUnit: 500000}
			got, err := p.Charge(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	p := CNYCentRounding{}
	got, err := p.Charge(123)
	require.NoError(t, err)
	assert.Equal(t, 123, got)
}

func TestCNYCentRoundingRejectsInvalidInput(t *testing.T) {
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		_, err := (CNYCentRounding{Enabled: true, USDToCNY: rate, QuotaPerUnit: 500000}).Charge(1)
		require.Error(t, err)
	}
	for _, raw := range []int{-1, MaxQuota} {
		_, err := (CNYCentRounding{Enabled: true, USDToCNY: 1, QuotaPerUnit: 500000}).Charge(raw)
		require.Error(t, err)
	}
	_, err := (CNYCentRounding{Enabled: true, USDToCNY: 1, QuotaPerUnit: 0}).Charge(1)
	require.Error(t, err)
}
