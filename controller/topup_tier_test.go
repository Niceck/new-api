package controller

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 累计充值等级（2026-10-30）：累计到账面值达到门槛后每笔充值都享该档折扣，与单笔档位取较优、不叠加；
// 配置了等级却读不出累计时，下单与试算都要明确报错让客户重试，不静默按原价。

// 生产用的两张表：单笔档位 200/500/1000/2000，累计等级 1000/2500/5000/10000。
var (
	testAmountDiscount = map[int]float64{200: 0.98, 500: 0.97, 1000: 0.95, 2000: 0.92}
	testTierDiscount   = map[int]float64{1000: 0.98, 2500: 0.97, 5000: 0.95, 10000: 0.92}
)

func withTopUpPricing(t *testing.T, amountDiscount map[int]float64, tierDiscount map[int]float64) {
	t.Helper()
	ps := operation_setting.GetPaymentSetting()
	originalAmount, originalTier, originalPrice := ps.AmountDiscount, ps.TierDiscount, operation_setting.Price
	ps.AmountDiscount, ps.TierDiscount, operation_setting.Price = amountDiscount, tierDiscount, 1
	t.Cleanup(func() {
		ps.AmountDiscount, ps.TierDiscount, operation_setting.Price = originalAmount, originalTier, originalPrice
	})
}

// withFaceTotal 把「读累计」换成固定答案，并数一数被调了几次。
func withFaceTotal(t *testing.T, total int64, err error) *int {
	t.Helper()
	calls := 0
	original := getUserTopUpFaceTotal
	getUserTopUpFaceTotal = func(_ int) (int64, error) {
		calls++
		return total, err
	}
	t.Cleanup(func() { getUserTopUpFaceTotal = original })
	return &calls
}

func TestQuoteTopUp_TakesTheBetterOfAmountAndTier(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)

	// 还没入级：和以前一样，只有单笔档位
	withFaceTotal(t, 999, nil)
	quote, err := quoteTopUp(500, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 485, quote.Money, 1e-9)
	assert.Nil(t, quote.Tier)

	// VIP1（9.8 折）：自定义金额 600 以前不打折，现在 588
	withFaceTotal(t, 1000, nil)
	quote, err = quoteTopUp(600, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 588, quote.Money, 1e-9)
	require.NotNil(t, quote.Tier)
	assert.Equal(t, 1, quote.Tier.Level)
	assert.Equal(t, int64(1000), quote.Total)

	// VIP1 充 2000：单笔 9.2 折更优，不叠加、不取等级
	quote, err = quoteTopUp(2000, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 1840, quote.Money, 1e-9)
	assert.Nil(t, quote.Tier, "单笔档位更优时不算等级折扣")
	assert.InDelta(t, 0.92, quote.Discount, 1e-9)

	// VIP4（9.2 折）充 500：等级 9.2 比单笔 9.7 更优
	withFaceTotal(t, 10190, nil)
	quote, err = quoteTopUp(500, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 460, quote.Money, 1e-9)
	assert.Equal(t, 4, quote.tierLevel())

	// VIP2 充 500：等级 9.7 与单笔 9.7 相同，按单笔算（不标成等级）
	withFaceTotal(t, 2500, nil)
	quote, err = quoteTopUp(500, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 485, quote.Money, 1e-9)
	assert.Nil(t, quote.Tier)
}

func TestQuoteTopUp_WithoutTiersNeverTouchesTheDatabase(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, map[int]float64{})
	calls := withFaceTotal(t, 0, errors.New("库挂了也不该被问到"))
	quote, err := quoteTopUp(200, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 196, quote.Money, 1e-9)
	assert.Zero(t, *calls)
}

func TestQuoteTopUp_FailsLoudlyWhenTotalCannotBeRead(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	withFaceTotal(t, 0, errors.New("connection refused"))
	_, err := quoteTopUp(200, "default", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "累计充值")
}

// 两位小数的折扣乘任意整数金额恒为整分：易支付入账前严格比对回调金额，应付金额不能带半分钱。
func TestPayMoneyWithDiscount_StaysOnWholeCents(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	for _, tier := range operation_setting.TopUpTiers() {
		for amount := int64(1); amount <= 3000; amount++ {
			money := payMoneyWithDiscount(amount, "default", tier.Discount)
			cents := money * 100
			assert.InDelta(t, math.Round(cents), cents, 1e-6, "amount=%d discount=%v money=%v", amount, tier.Discount, money)
			// 与订单金额的生成函数同一口径
			assert.Equal(t, model.EpayOrderMoney(money), model.EpayOrderMoney(math.Round(cents)/100))
		}
	}
}

func topUpHandlerBody(t *testing.T, recorder interface{ String() string }) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(recorder.String()), &body))
	return body
}

func TestRequestAmount_AppliesTierAndReportsReadFailure(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	originalGroup := getTopUpUserGroup
	getTopUpUserGroup = func(_ int, _ bool) (string, error) { return "default", nil }
	t.Cleanup(func() { getTopUpUserGroup = originalGroup })

	withFaceTotal(t, 5000, nil) // VIP3 9.5 折
	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":50}`, 9301, RequestAmount)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"data":"47.50"`)

	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":2000}`, 9301, RequestAmount)
	assert.Contains(t, recorder.Body.String(), `"data":"1840.00"`, "单笔 9.2 折更优")

	withFaceTotal(t, 0, errors.New("timeout"))
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":50}`, 9301, RequestAmount)
	body := recorder.Body.String()
	assert.Contains(t, body, `"message":"error"`)
	assert.Contains(t, body, topUpTierUnavailableMessage)
}

func TestRequestEpay_StoresTierDiscountedMoney(t *testing.T) {
	db := setupEpayCallbackTest(t)
	require.NoError(t, db.AutoMigrate(&model.TronTopupOrder{}))
	withQuotaPerUnit(t, 500_000)
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	seedWalletUser(t, 9401, 0)
	withFaceTotal(t, 2500, nil) // VIP2 9.7 折

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100,"payment_method":"alipay"}`, 9401, RequestEpay)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"message":"success"`)
	var order model.TopUp
	require.NoError(t, db.Where("user_id = ?", 9401).First(&order).Error)
	assert.Equal(t, int64(100), order.Amount, "到账面值不变")
	assert.InDelta(t, 97, order.Money, 1e-9, "应付按等级 9.7 折")

	// 读累计失败：不建单、不交支付链接
	withFaceTotal(t, 0, errors.New("timeout"))
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100,"payment_method":"alipay"}`, 9401, RequestEpay)
	body := recorder.Body.String()
	assert.Contains(t, body, `"message":"error"`)
	assert.Contains(t, body, topUpTierUnavailableMessage)
	assert.NotContains(t, body, "pay.example.com")
	assert.Equal(t, int64(1), topUpCount(t, 9401))
}

func TestCreateTronTopupOrder_UsesTierQuote(t *testing.T) {
	operations := &stubTronTopupOperations{createView: service.TronOrderView{TradeNo: "TRON-tier", Status: common.TopUpStatusPending}}
	withTronControllerDependencies(t, operations)
	calculateTronPaymentMoney = quoteTronPayMoney // 用真实口径，不用桩
	withQuotaPerUnit(t, 500_000)
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	withFaceTotal(t, 2500, nil)

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.True(t, operations.createPayCNY.Equal(decimal.NewFromInt(97)), "USDT 订单的人民币应付也按等级 9.7 折：%s", operations.createPayCNY)
	assert.Equal(t, 50_000_000, operations.createQuota, "到账额度按面值")

	withFaceTotal(t, 0, errors.New("timeout"))
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	body := recorder.Body.String()
	assert.Contains(t, body, `"success":false`)
	assert.Contains(t, body, topUpTierUnavailableMessage)
}

func TestGetTopUpInfo_ReportsTierAndProgress(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	withFaceTotal(t, 2735, nil)

	recorder := tronHandlerResponse(t, http.MethodGet, "/", "", 121, GetTopUpInfo)
	require.Equal(t, http.StatusOK, recorder.Code)
	data := topUpHandlerBody(t, recorder.Body)["data"].(map[string]any)
	tier := data["tier"].(map[string]any)
	assert.EqualValues(t, 2735, tier["total"])
	assert.EqualValues(t, 2, tier["level"])
	assert.InDelta(t, 0.97, tier["discount"], 1e-9)
	assert.EqualValues(t, 2500, tier["threshold"])
	assert.EqualValues(t, 3, tier["next_level"])
	assert.EqualValues(t, 5000, tier["next_threshold"])
	assert.InDelta(t, 0.95, tier["next_discount"], 1e-9)
	assert.EqualValues(t, 2265, tier["remaining"])
	assert.Len(t, tier["tiers"], 4)

	// 还没入级：level 0、discount 1，但告诉前端下一档
	withFaceTotal(t, 0, nil)
	recorder = tronHandlerResponse(t, http.MethodGet, "/", "", 7, GetTopUpInfo)
	tier = topUpHandlerBody(t, recorder.Body)["data"].(map[string]any)["tier"].(map[string]any)
	assert.EqualValues(t, 0, tier["level"])
	assert.EqualValues(t, 1, tier["next_level"])
	assert.EqualValues(t, 1000, tier["remaining"])

	// 最高档：没有下一档
	withFaceTotal(t, 10190, nil)
	recorder = tronHandlerResponse(t, http.MethodGet, "/", "", 382, GetTopUpInfo)
	tier = topUpHandlerBody(t, recorder.Body)["data"].(map[string]any)["tier"].(map[string]any)
	assert.EqualValues(t, 4, tier["level"])
	_, hasNext := tier["next_level"]
	assert.False(t, hasNext)

	// 读累计失败：钱包页照常打开，只是没有等级
	withFaceTotal(t, 0, errors.New("timeout"))
	recorder = tronHandlerResponse(t, http.MethodGet, "/", "", 382, GetTopUpInfo)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Nil(t, topUpHandlerBody(t, recorder.Body)["data"].(map[string]any)["tier"])
}

func TestGetTopUpInfo_TierIsNullWhenNotConfigured(t *testing.T) {
	withTopUpPricing(t, testAmountDiscount, map[int]float64{})
	calls := withFaceTotal(t, 0, nil)
	recorder := tronHandlerResponse(t, http.MethodGet, "/", "", 1, GetTopUpInfo)
	require.Equal(t, http.StatusOK, recorder.Code)
	data := topUpHandlerBody(t, recorder.Body)["data"].(map[string]any)
	_, present := data["tier"]
	assert.True(t, present, "字段要在，值为 null，前端好区分「没配置」与「老后端」")
	assert.Nil(t, data["tier"])
	assert.Zero(t, *calls)
}

func TestUpdateOption_RejectsBrokenTierDiscount(t *testing.T) {
	assert.Error(t, operation_setting.ValidateTierDiscountJSON(`{"1000":0.975}`))
	assert.NoError(t, operation_setting.ValidateTierDiscountJSON(`{"1000":0.98,"2500":0.97,"5000":0.95,"10000":0.92}`))
}
