package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 累计充值等级的配置校验与查表：两位小数、越高越优惠、按门槛排序、找当前档与下一档。

func TestValidateTierDiscountJSON_AcceptsTheProductionLadder(t *testing.T) {
	require.NoError(t, ValidateTierDiscountJSON(`{"1000":0.98,"2500":0.97,"5000":0.95,"10000":0.92}`))
	require.NoError(t, ValidateTierDiscountJSON(`{}`), "空表 = 关闭等级")
	require.NoError(t, ValidateTierDiscountJSON(`{"500":1}`), "折扣 1 = 这一档只是称号不打折，允许")
}

func TestValidateTierDiscountJSON_RejectsBadShapes(t *testing.T) {
	cases := map[string]string{
		"空串":             ``,
		"不是对象":           `[1000]`,
		"门槛带小数":          `{"1000.5":0.98}`,
		"门槛为零":           `{"0":0.98}`,
		"门槛为负":           `{"-5":0.98}`,
		"折扣为零":           `{"1000":0}`,
		"折扣大于 1":         `{"1000":1.2}`,
		"折扣三位小数":         `{"1000":0.975}`,
		"高档反而更差":         `{"1000":0.95,"2500":0.97}`,
		"折扣不是数字":         `{"1000":"九折"}`,
		"写成 92 而不是 0.92": `{"1000":92}`,
		"重复的门槛键":         `{"1000":0.98,"1000":0.97}`,
		"前导零（解析后撞键）":     `{"1000":0.98,"01000":0.97}`,
		"指数写法的门槛":        `{"1e3":0.98}`,
		"带正号的门槛":         `{"+1000":0.98}`,
		"浮点尾数":           `{"1000":0.9700000000000001}`,
		"门槛超过上限":         `{"10000000000":0.98}`,
	}
	for name, value := range cases {
		err := ValidateTierDiscountJSON(value)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrTierDiscountInvalid, name)
	}
}

func TestValidateTierDiscountJSON_RefusesTiersInTokensDisplay(t *testing.T) {
	general := GetGeneralSetting()
	original := general.QuotaDisplayType
	general.QuotaDisplayType = QuotaDisplayTypeTokens
	t.Cleanup(func() { general.QuotaDisplayType = original })
	// 按 tokens 展示时 Stripe 等渠道的订单面值是原始 token 数，与易支付的单位不一致，累计会算错
	assert.ErrorIs(t, ValidateTierDiscountJSON(`{"1000":0.98}`), ErrTierDiscountInvalid)
	assert.NoError(t, ValidateTierDiscountJSON(`{}`), "关闭等级随时可以")
}

func TestValidateTierDiscountJSON_AcceptsEquivalentDecimalSpellings(t *testing.T) {
	require.NoError(t, ValidateTierDiscountJSON(`{"1000":0.980,"2500":0.97}`), "0.980 的值仍是两位小数")
	require.NoError(t, ValidateTierDiscountJSON(`{ "1000" : 0.98 , "2500":0.97 }`), "空白不影响")
}

func TestValidateTierDiscountJSON_LimitsTierCount(t *testing.T) {
	many := "{"
	for i := 1; i <= maxTopUpTiers+1; i++ {
		if i > 1 {
			many += ","
		}
		many += `"` + itoa(i*100) + `":0.99`
	}
	many += "}"
	assert.ErrorIs(t, ValidateTierDiscountJSON(many), ErrTierDiscountInvalid)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestTierForTotal_FindsCurrentAndNext(t *testing.T) {
	tiers, err := tiersFromMap(map[int]float64{10000: 0.92, 1000: 0.98, 5000: 0.95, 2500: 0.97})
	require.NoError(t, err)
	// 按门槛升序并编号
	require.Len(t, tiers, 4)
	assert.Equal(t, []int64{1000, 2500, 5000, 10000}, []int64{tiers[0].Threshold, tiers[1].Threshold, tiers[2].Threshold, tiers[3].Threshold})
	assert.Equal(t, 4, tiers[3].Level)

	current, next := TierForTotal(tiers, 0)
	assert.Nil(t, current)
	require.NotNil(t, next)
	assert.Equal(t, int64(1000), next.Threshold)

	current, next = TierForTotal(tiers, 999)
	assert.Nil(t, current)
	assert.Equal(t, 1, next.Level)

	current, next = TierForTotal(tiers, 1000)
	require.NotNil(t, current)
	assert.Equal(t, 1, current.Level)
	assert.Equal(t, 0.98, current.Discount)
	assert.Equal(t, int64(2500), next.Threshold)

	current, next = TierForTotal(tiers, 7300)
	assert.Equal(t, 3, current.Level)
	assert.Equal(t, 4, next.Level)

	current, next = TierForTotal(tiers, 10190)
	assert.Equal(t, 4, current.Level)
	assert.Equal(t, 0.92, current.Discount)
	assert.Nil(t, next, "已是最高档")
}

func TestTopUpTiers_ReadsPaymentSettingAndIgnoresBrokenConfig(t *testing.T) {
	original := GetPaymentSetting().TierDiscount
	t.Cleanup(func() { GetPaymentSetting().TierDiscount = original })

	GetPaymentSetting().TierDiscount = map[int]float64{}
	assert.Nil(t, TopUpTiers())

	GetPaymentSetting().TierDiscount = map[int]float64{1000: 0.98, 2500: 0.97}
	tiers := TopUpTiers()
	require.Len(t, tiers, 2)
	assert.Equal(t, int64(1000), tiers[0].Threshold)

	// 不合法的表（本不该写得进来）按没有等级处理，不让下单路径崩掉
	GetPaymentSetting().TierDiscount = map[int]float64{1000: 0.975}
	assert.Nil(t, TopUpTiers())
}
