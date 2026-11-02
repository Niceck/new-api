package operation_setting

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 充值赠送（2026-11-02）：实付等于面值，多送一定比例的额度。单笔按「满额」取档，累计等级按累计面值取档。

func withBonusTables(t *testing.T, amount, tier map[int]float64) {
	t.Helper()
	ps := GetPaymentSetting()
	originalAmount, originalTier := ps.AmountBonus, ps.TierBonus
	ps.AmountBonus, ps.TierBonus = amount, tier
	t.Cleanup(func() { ps.AmountBonus, ps.TierBonus = originalAmount, originalTier })
}

func withQuotaDisplay(t *testing.T, display string) {
	t.Helper()
	general := GetGeneralSetting()
	original := general.QuotaDisplayType
	general.QuotaDisplayType = display
	t.Cleanup(func() { general.QuotaDisplayType = original })
}

var (
	productionAmountBonus = map[int]float64{200: 0.025, 500: 0.035, 1000: 0.055, 2000: 0.09}
	productionTierBonus   = map[int]float64{1000: 0.025, 2500: 0.035, 5000: 0.055, 10000: 0.09}
)

func TestAmountBonusRate_IsReachedByThresholdNotExactMatch(t *testing.T) {
	withBonusTables(t, productionAmountBonus, nil)
	cases := map[int64]float64{
		1: 0, 199: 0,
		200: 0.025, 201: 0.025, 499: 0.025,
		500: 0.035, 600: 0.035, 999: 0.035,
		1000: 0.055, 1999: 0.055,
		2000: 0.09, 4000: 0.09,
	}
	for amount, want := range cases {
		assert.Equal(t, want, AmountBonusRate(amount), "面值 %d", amount)
	}
}

func TestAmountBonusRate_ZeroWhenNotConfigured(t *testing.T) {
	withBonusTables(t, map[int]float64{}, nil)
	assert.Zero(t, AmountBonusRate(2000))
}

func TestTopUpBonusTiers_AreOrderedAndCarryTheRate(t *testing.T) {
	withBonusTables(t, nil, productionTierBonus)
	tiers := TopUpBonusTiers()
	require.Len(t, tiers, 4)
	assert.Equal(t, TopUpTier{Level: 1, Threshold: 1000, Discount: 1, Bonus: 0.025}, tiers[0])
	assert.Equal(t, TopUpTier{Level: 4, Threshold: 10000, Discount: 1, Bonus: 0.09}, tiers[3])

	current, next := TierForTotal(tiers, 2500)
	require.NotNil(t, current)
	require.NotNil(t, next)
	assert.Equal(t, 2, current.Level)
	assert.Equal(t, 3, next.Level)
}

func TestTopUpBonusConfigured_FollowsEitherTable(t *testing.T) {
	withBonusTables(t, nil, nil)
	assert.False(t, TopUpBonusConfigured())
	withBonusTables(t, map[int]float64{200: 0.02}, nil)
	assert.True(t, TopUpBonusConfigured())
	withBonusTables(t, nil, map[int]float64{1000: 0.02})
	assert.True(t, TopUpBonusConfigured())
}

func TestTopUpBonusEnabled_OffUnderTokensDisplay(t *testing.T) {
	withBonusTables(t, productionAmountBonus, productionTierBonus)
	withQuotaDisplay(t, QuotaDisplayTypeCNY)
	assert.True(t, TopUpBonusEnabled())
	withQuotaDisplay(t, QuotaDisplayTypeTokens)
	assert.False(t, TopUpBonusEnabled(), "按 tokens 展示时各渠道面值单位不一致，赠送不生效")
}

func TestTopUpBonusEnabled_AmountTableIsTheOnlySwitch(t *testing.T) {
	withQuotaDisplay(t, QuotaDisplayTypeCNY)
	// 切换时等级表先写：这时还不算赠送模式，折扣继续生效，才没有「不打折也不送」的窗口
	withBonusTables(t, map[int]float64{}, productionTierBonus)
	assert.False(t, TopUpBonusEnabled())
	assert.True(t, TopUpBonusConfigured(), "切 tokens 展示前的检查仍然看得见它")
	withBonusTables(t, productionAmountBonus, map[int]float64{})
	assert.True(t, TopUpBonusEnabled())
}

func TestTopUpBonusOffer_IsASnapshotThatDoesNotFollowLaterChanges(t *testing.T) {
	withQuotaDisplay(t, QuotaDisplayTypeCNY)
	withBonusTables(t, productionAmountBonus, productionTierBonus)
	offer := CurrentTopUpBonusOffer()
	require.True(t, offer.Enabled())

	// 报价途中配置被清空：手里这份快照还是原来的，这一笔不会算成两样都没有
	GetPaymentSetting().AmountBonus = map[int]float64{}
	GetPaymentSetting().TierBonus = map[int]float64{}
	rate, tier := offer.RateFor(600, 0)
	assert.Equal(t, 0.035, rate)
	assert.Nil(t, tier)
	rate, tier = offer.RateFor(100, 3000)
	assert.Equal(t, 0.035, rate)
	require.NotNil(t, tier)
	assert.Equal(t, 2, tier.Level)
	assert.False(t, CurrentTopUpBonusOffer().Enabled())
}

func TestCurrentTopUpBonusOffer_TierTableAloneIsNotAnOffer(t *testing.T) {
	withQuotaDisplay(t, QuotaDisplayTypeCNY)
	withBonusTables(t, map[int]float64{}, productionTierBonus)
	assert.False(t, CurrentTopUpBonusOffer().Enabled())
	withBonusTables(t, productionAmountBonus, productionTierBonus)
	withQuotaDisplay(t, QuotaDisplayTypeTokens)
	assert.False(t, CurrentTopUpBonusOffer().Enabled())
}

func TestBadRuntimeTableMeansNoBonus(t *testing.T) {
	// 绕过校验进库的坏表：按没有赠送处理（客户付面值、到账面值），不会多送也不会 panic
	withBonusTables(t, map[int]float64{200: 5}, map[int]float64{1000: -1})
	assert.Zero(t, AmountBonusRate(2000))
	assert.Nil(t, TopUpBonusTiers())
	assert.False(t, TopUpBonusConfigured())

	// 不是递增的也算坏表：高档送得比低档少，客户多充反而少送
	withBonusTables(t, map[int]float64{200: 0.05, 500: 0.03}, nil)
	assert.Zero(t, AmountBonusRate(500))
}

func TestValidateTopUpBonusJSON(t *testing.T) {
	withQuotaDisplay(t, QuotaDisplayTypeCNY)
	good := []string{
		`{}`,
		`{"200":0.025}`,
		`{"200":0.025,"500":0.035,"1000":0.055,"2000":0.09}`,
		`{"200":0.0001}`,
		`{"1000":0.5}`,
	}
	for _, value := range good {
		assert.NoError(t, ValidateTopUpBonusJSON(value), value)
	}
	bad := map[string]string{
		"空串":      ``,
		"null":    `null`,
		"数组":      `[1,2]`,
		"折扣写法":    `{"200":0.98}`,
		"大于上限":    `{"200":0.51}`,
		"零":       `{"200":0}`,
		"负数":      `{"200":-0.01}`,
		"五位小数":    `{"200":0.02501}`,
		"前导零":     `{"0200":0.02}`,
		"带符号":     `{"+200":0.02}`,
		"指数门槛":    `{"2e2":0.02}`,
		"小数门槛":    `{"200.5":0.02}`,
		"零门槛":     `{"0":0.02}`,
		"门槛太大":    `{"1000000001":0.02}`,
		"重复键":     `{"200":0.02,"200":0.03}`,
		"高档不高于低档": `{"200":0.03,"500":0.03}`,
		"高档少送":    `{"200":0.05,"500":0.03}`,
		"比例写成字符串": `{"200":"0.02"}`,
	}
	for name, value := range bad {
		err := ValidateTopUpBonusJSON(value)
		require.Error(t, err, name)
		assert.True(t, errors.Is(err, ErrTopUpBonusInvalid), name)
	}
}

func TestValidateTopUpBonusJSON_RefusesTokensDisplay(t *testing.T) {
	withQuotaDisplay(t, QuotaDisplayTypeTokens)
	assert.Error(t, ValidateTopUpBonusJSON(`{"200":0.02}`))
	assert.NoError(t, ValidateTopUpBonusJSON(`{}`), "关闭赠送任何时候都可以")
}
