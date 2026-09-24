package common

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

// CNYCentRounding is captured once per request and persisted with async tasks.
// Disabled snapshots are also retained so toggling the setting cannot reprice work.
type CNYCentRounding struct {
	Enabled      bool    `json:"enabled"`
	USDToCNY     float64 `json:"usd_to_cny"`
	QuotaPerUnit float64 `json:"quota_per_unit"`
}

// BillingCharge separates the original log amount from the actual ledger debit.
type BillingCharge struct {
	SettlementError string          `json:"settlement_error,omitempty"`
	Refunded        bool            `json:"refunded,omitempty"`
	RawQuota        int             `json:"raw_quota"`
	ChargedQuota    int             `json:"charged_quota"`
	Policy          CNYCentRounding `json:"policy"`
}

// Charge rounds a nonnegative total, never a delta or an already rounded debit.
func (p CNYCentRounding) Charge(raw int) (int, error) {
	if raw < 0 || raw > MaxQuota {
		return 0, fmt.Errorf("invalid billing quota: %d", raw)
	}
	if !p.Enabled {
		return raw, nil
	}
	if p.USDToCNY <= 0 || math.IsNaN(p.USDToCNY) || math.IsInf(p.USDToCNY, 0) || p.QuotaPerUnit <= 0 || math.IsNaN(p.QuotaPerUnit) || math.IsInf(p.QuotaPerUnit, 0) {
		return 0, fmt.Errorf("invalid CNY billing exchange rate or quota unit")
	}
	if raw == 0 {
		return 0, nil
	}
	rate := decimal.NewFromFloat(p.USDToCNY).Mul(decimal.NewFromInt(100))
	unit := decimal.NewFromFloat(p.QuotaPerUnit)
	// QuoRem preserves the exact remainder; Div followed by Ceil could lose
	// a very small positive remainder at decimal.DivisionPrecision.
	cents, rem := decimal.NewFromInt(int64(raw)).Mul(rate).QuoRem(unit, 0)
	if rem.IsPositive() {
		cents = cents.Add(decimal.NewFromInt(1))
	}
	quota, rem := cents.Mul(unit).QuoRem(rate, 0)
	if rem.IsPositive() {
		quota = quota.Add(decimal.NewFromInt(1))
	}
	charged, clamp := QuotaFromDecimalChecked(quota)
	if clamp != nil {
		return 0, clamp
	}
	return charged, nil
}

func (b *BillingCharge) AuditMap() map[string]interface{} {
	if b == nil {
		return nil
	}
	return map[string]interface{}{"raw_quota": b.RawQuota, "charged_quota": b.ChargedQuota, "rounding_delta": b.ChargedQuota - b.RawQuota, "currency": "CNY", "unit": "0.01", "usd_to_cny": b.Policy.USDToCNY, "quota_per_unit": b.Policy.QuotaPerUnit, "settlement_error": b.SettlementError}
}
