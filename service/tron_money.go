package service

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const (
	tronUSDTScale             int64 = 1_000_000
	tronUniqueCandidateCount  int64 = 9_999
	tronMaxUniqueOffsetMicros int64 = (tronUniqueCandidateCount - 1) / 2
	tronProbeSeedMin          int64 = 1
	tronProbeSeedMax          int64 = tronUniqueCandidateCount
)

type TronPaymentQuote struct {
	ExpectedUSDTMicros int64
	RateCNYMicros      int64
	CreditQuota        int
}

func calculateTronPayment(payCNY decimal.Decimal, rateCNY decimal.Decimal, creditQuota int, offsetMicros int64) (TronPaymentQuote, error) {
	if !payCNY.IsPositive() || !rateCNY.IsPositive() {
		return TronPaymentQuote{}, errors.New("payment and rate must be positive")
	}
	if creditQuota <= 0 || creditQuota > common.MaxQuota {
		return TronPaymentQuote{}, errors.New("credit quota is out of range")
	}
	if offsetMicros < -tronMaxUniqueOffsetMicros || offsetMicros > tronMaxUniqueOffsetMicros {
		return TronPaymentQuote{}, errors.New("TRON payment uniqueness offset is out of range")
	}

	scale := decimal.NewFromInt(tronUSDTScale)
	targetMicros := payCNY.Div(rateCNY).Mul(scale).Round(0)
	minimumTarget := decimal.NewFromInt(tronMaxUniqueOffsetMicros + 1)
	maximumTarget := decimal.NewFromInt(math.MaxInt64 - tronMaxUniqueOffsetMicros)
	if targetMicros.LessThan(minimumTarget) || targetMicros.GreaterThan(maximumTarget) {
		return TronPaymentQuote{}, errors.New("expected USDT amount cannot reserve uniqueness window")
	}
	expected := targetMicros.Add(decimal.NewFromInt(offsetMicros))
	if !expected.IsPositive() || expected.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return TronPaymentQuote{}, errors.New("expected USDT amount is out of range")
	}

	rateMicros := rateCNY.Mul(scale).Round(0)
	if !rateMicros.IsPositive() || rateMicros.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return TronPaymentQuote{}, errors.New("USDT/CNY rate is out of range")
	}

	return TronPaymentQuote{
		ExpectedUSDTMicros: expected.IntPart(),
		RateCNYMicros:      rateMicros.IntPart(),
		CreditQuota:        creditQuota,
	}, nil
}
