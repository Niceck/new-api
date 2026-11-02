package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 充值赠送（2026-11-02）：实付等于面值，多送的额度在建单时算好落进订单。
// 单笔满额与累计等级取高、不叠加；赠送表非空时折扣表不生效；累计读不出来时拒绝下单，不静默少送。

var (
	testAmountBonus = map[int]float64{200: 0.025, 500: 0.035, 1000: 0.055, 2000: 0.09}
	testTierBonus   = map[int]float64{1000: 0.025, 2500: 0.035, 5000: 0.055, 10000: 0.09}
)

func withTopUpBonus(t *testing.T, amountBonus map[int]float64, tierBonus map[int]float64) {
	t.Helper()
	withQuotaPerUnit(t, 500_000)
	withTopUpPricing(t, map[int]float64{}, map[int]float64{})
	ps := operation_setting.GetPaymentSetting()
	originalAmount, originalTier := ps.AmountBonus, ps.TierBonus
	ps.AmountBonus, ps.TierBonus = amountBonus, tierBonus
	t.Cleanup(func() { ps.AmountBonus, ps.TierBonus = originalAmount, originalTier })
}

func TestQuoteTopUp_PaysFaceAndGiftsTheHigherOfAmountAndTier(t *testing.T) {
	withTopUpBonus(t, testAmountBonus, testTierBonus)

	cases := []struct {
		name       string
		total      int64
		amount     int64
		wantRate   float64
		wantBonus  int64
		wantLevel  int
		wantReason string
	}{
		{"没入级的小额没有赠送", 0, 100, 0, 0, 0, "门槛以下"},
		{"恰好满额", 0, 200, 0.025, 2_500_000, 0, "200 × 2.5% = ¥5"},
		{"不在档位上也按满额", 0, 600, 0.035, 10_500_000, 0, "600 × 3.5% = ¥21"},
		{"单笔最高档", 0, 2000, 0.09, 90_000_000, 0, "2000 × 9% = ¥180"},
		{"VIP2 小额按等级", 2500, 10, 0.035, 175_000, 2, "10 × 3.5% = ¥0.35"},
		{"等级高过已到的单笔档：取高不是相加", 2500, 200, 0.035, 3_500_000, 2, "200 × 3.5% = ¥7，不是 6%"},
		{"VIP2 单笔更高按单笔", 2500, 2000, 0.09, 90_000_000, 0, "取高"},
		{"VIP4 任意金额 9%", 10000, 33, 0.09, 1_485_000, 4, "33 × 9% = ¥2.97"},
		{"相等时算单笔", 2500, 500, 0.035, 8_750_000, 0, "同为 3.5%，不算等级"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFaceTotal(t, tc.total, nil)
			quote, err := quoteTopUp(tc.amount, "default", 1)
			require.NoError(t, err)
			assert.InDelta(t, float64(tc.amount), quote.Money, 1e-9, "实付恒等于面值")
			assert.Equal(t, 1.0, quote.Discount)
			assert.Equal(t, tc.wantRate, quote.BonusRate, tc.wantReason)
			assert.Equal(t, tc.wantBonus, quote.BonusQuota, tc.wantReason)
			assert.Equal(t, tc.wantLevel, quote.tierLevel(), tc.wantReason)
			assert.Equal(t, tc.total, quote.Total)
		})
	}
}

func TestTopUpBonusQuota_IsWholeQuotaForEveryAmountAndRate(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	for _, rate := range []float64{0.0001, 0.025, 0.035, 0.055, 0.09, 0.0333, 0.5} {
		for amount := int64(1); amount <= 3000; amount++ {
			want := decimal.NewFromInt(amount).Mul(decimal.NewFromInt(500_000)).Mul(decimal.RequireFromString(decimal.NewFromFloat(rate).String()))
			require.True(t, want.Equal(want.Floor()), "%d × %v 应为整数额度", amount, rate)
			require.Equal(t, want.IntPart(), topUpBonusQuota(amount, rate), "%d × %v", amount, rate)
		}
	}
	assert.Zero(t, topUpBonusQuota(0, 0.09))
	assert.Zero(t, topUpBonusQuota(100, 0))
	assert.Equal(t, int64(common.MaxQuota), topUpBonusQuota(1<<40, 0.5), "面值离谱时饱和，不能绕回成小数字")
}

func TestQuoteTopUp_BonusModeIgnoresDiscountTables(t *testing.T) {
	// 切换窗口：赠送表已写、折扣表还没清。不能既打折又赠送，也不能两头都没有。
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	withFaceTotal(t, 10000, nil)

	quote, err := quoteTopUp(2000, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 2000, quote.Money, 1e-9, "不打折")
	assert.Equal(t, int64(90_000_000), quote.BonusQuota, "按赠送")

	// 反过来：赠送表清空，折扣表恢复生效（回到折扣模式不用换镜像）
	ps := operation_setting.GetPaymentSetting()
	ps.AmountBonus, ps.TierBonus = map[int]float64{}, map[int]float64{}
	quote, err = quoteTopUp(2000, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 1840, quote.Money, 1e-9)
	assert.Zero(t, quote.BonusQuota)
}

func TestQuoteTopUp_TierTableAloneDoesNotSwitchModes(t *testing.T) {
	// 切换的第一步：等级赠送表写好了、单笔表还没到。此时仍是折扣模式，VIP4 充 100 还是付 92、不送
	withTopUpBonus(t, map[int]float64{}, testTierBonus)
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	withFaceTotal(t, 10000, nil)
	quote, err := quoteTopUp(100, "default", 1)
	require.NoError(t, err)
	assert.InDelta(t, 92, quote.Money, 1e-9)
	assert.Zero(t, quote.BonusQuota)
}

func TestQuoteTopUp_OnlyAmountBonusNeverTouchesTheDatabase(t *testing.T) {
	withTopUpBonus(t, testAmountBonus, map[int]float64{})
	calls := withFaceTotal(t, 0, errors.New("不该读"))
	quote, err := quoteTopUp(500, "default", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(8_750_000), quote.BonusQuota)
	assert.Zero(t, *calls)
}

func TestQuoteTopUp_FailsLoudlyWhenTotalCannotBeReadInBonusMode(t *testing.T) {
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	withFaceTotal(t, 0, errors.New("timeout"))
	_, err := quoteTopUp(500, "default", 1)
	assert.Error(t, err)
}

func TestRequestAmount_ReportsBonusWhenThereIsOne(t *testing.T) {
	setupEpayCallbackTest(t)
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	withFaceTotal(t, 2500, nil)
	original := getTopUpUserGroup
	getTopUpUserGroup = func(_ int, _ bool) (string, error) { return "default", nil }
	t.Cleanup(func() { getTopUpUserGroup = original })

	body := topUpHandlerBody(t, tronHandlerResponse(t, http.MethodPost, "/", `{"amount":500}`, 9411, RequestAmount).Body)
	assert.Equal(t, "success", body["message"])
	assert.Equal(t, "500.00", body["data"], "应付仍在 data，老调用方不受影响")
	assert.InDelta(t, 0.035, body["bonus_rate"], 1e-9)
	assert.InDelta(t, 17.5, body["bonus_amount"], 1e-9)
	assert.InDelta(t, 517.5, body["credit_amount"], 1e-9)

	// 没有赠送的金额：响应与以前一样，没有多余字段
	withFaceTotal(t, 0, nil)
	body = topUpHandlerBody(t, tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 9411, RequestAmount).Body)
	assert.Equal(t, "100.00", body["data"])
	assert.NotContains(t, body, "bonus_rate")
	assert.NotContains(t, body, "bonus_amount")
}

func TestRequestEpay_StoresBonusSnapshotAndFaceMoney(t *testing.T) {
	db := setupEpayCallbackTest(t)
	require.NoError(t, db.AutoMigrate(&model.TronTopupOrder{}))
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	seedWalletUser(t, 9412, 0)
	withFaceTotal(t, 0, nil)

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":600,"payment_method":"alipay"}`, 9412, RequestEpay)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"message":"success"`)
	var order model.TopUp
	require.NoError(t, db.Where("user_id = ?", 9412).First(&order).Error)
	assert.Equal(t, int64(600), order.Amount, "面值")
	assert.InDelta(t, 600, order.Money, 1e-9, "实付 = 面值")
	assert.Equal(t, int64(10_500_000), order.BonusQuota, "600 × 3.5% = ¥21")

	// 读累计失败：不建单、不交支付链接
	withFaceTotal(t, 0, errors.New("timeout"))
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100,"payment_method":"alipay"}`, 9412, RequestEpay)
	body := recorder.Body.String()
	assert.Contains(t, body, topUpTierUnavailableMessage)
	assert.NotContains(t, body, "pay.example.com")
	assert.Equal(t, int64(1), topUpCount(t, 9412))
}

func TestRequestEpay_WalletCapacityCountsTheBonus(t *testing.T) {
	db := setupEpayCallbackTest(t)
	require.NoError(t, db.AutoMigrate(&model.TronTopupOrder{}))
	withTopUpBonus(t, testAmountBonus, map[int]float64{})

	// 钱包上限 ¥4294.967294；余额 ¥2200，面值 2000 本身装得下（4200），加 9% 赠送（2180 → 4380）装不下
	seedWalletUser(t, 9413, 2200)
	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":2000,"payment_method":"alipay"}`, 9413, RequestEpay)
	body := recorder.Body.String()
	assert.Contains(t, body, "最多可充")
	assert.Contains(t, body, "1985", "1985 × 1.055 = 2094.17 装得下（1986 × 1.055 = 2095.23 超了），不能报 2094")
	assert.Equal(t, int64(0), topUpCount(t, 9413), "被拒的订单不落库")
}

func TestTopUpMaxAmountForRoom(t *testing.T) {
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	unit := int64(500_000)
	cases := []struct {
		name      string
		roomUnits int64
		total     int64
		want      int64
	}{
		{"门槛以下等于额度", 100, 0, 100},
		{"跨过 200 档回落：199 全到账，200 要 205", 204, 0, 199},
		{"205 正好放下 200", 205, 0, 200},
		{"2180 正好放下 2000（9%）", 2180, 0, 2000},
		{"2179 放不下 2000，回到 5.5% 档", 2179, 0, 1999},
		{"VIP4 任意金额都按 9%", 109, 10000, 100},
		{"没有空间", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := topUpMaxAmountForRoom(tc.roomUnits*unit, tc.total)
			assert.Equal(t, tc.want, got)
			if got > 0 {
				rate, _ := topUpBonusRateForTotal(got, tc.total)
				assert.LessOrEqual(t, model.TopUpFaceQuota(got)+topUpBonusQuota(got, rate), tc.roomUnits*unit, "报出来的数字必须真能下单")
			}
		})
	}
}

func TestCreateTronTopupOrder_CreditQuotaIncludesBonus(t *testing.T) {
	operations := &stubTronTopupOperations{createView: service.TronOrderView{TradeNo: "TRON-bonus", Status: common.TopUpStatusPending}}
	withTronControllerDependencies(t, operations)
	calculateTronQuote = quoteTopUp
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	withFaceTotal(t, 2500, nil)

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.True(t, operations.createPayCNY.Equal(decimal.NewFromInt(100)), "USDT 应付按面值：%s", operations.createPayCNY)
	assert.Equal(t, int64(100), operations.createAmount)
	assert.Equal(t, 51_750_000, operations.createQuota, "全额到账 = 面值 + 3.5%")
	assert.Equal(t, int64(1_750_000), operations.createBonus)

	withFaceTotal(t, 0, errors.New("timeout"))
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	assert.Contains(t, recorder.Body.String(), topUpTierUnavailableMessage)
}

func TestGetTopUpInfo_ReportsBonusAndHidesDiscount(t *testing.T) {
	setupEpayCallbackTest(t)
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	withTopUpPricing(t, testAmountDiscount, testTierDiscount) // 切换窗口：折扣表还没清
	withFaceTotal(t, 3000, nil)

	recorder := tronHandlerResponse(t, http.MethodGet, "/", "", 9414, GetTopUpInfo)
	var response struct {
		Data struct {
			Discount map[string]float64 `json:"discount"`
			Bonus    map[string]float64 `json:"bonus"`
			Tier     map[string]any     `json:"tier"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Empty(t, response.Data.Discount, "赠送模式下钱包页不再显示折扣")
	assert.Equal(t, map[string]float64{"200": 0.025, "500": 0.035, "1000": 0.055, "2000": 0.09}, response.Data.Bonus)
	require.NotNil(t, response.Data.Tier)
	assert.EqualValues(t, 2, response.Data.Tier["level"])
	assert.EqualValues(t, 1, response.Data.Tier["discount"])
	assert.InDelta(t, 0.035, response.Data.Tier["bonus"], 1e-9)
	assert.InDelta(t, 0.055, response.Data.Tier["next_bonus"], 1e-9)
	assert.EqualValues(t, 2000, response.Data.Tier["remaining"])
	tiers, ok := response.Data.Tier["tiers"].([]any)
	require.True(t, ok)
	require.Len(t, tiers, 4)
	assert.InDelta(t, 0.09, tiers[3].(map[string]any)["bonus"], 1e-9)
}

func TestGetTopUpInfo_DiscountModeIsUnchanged(t *testing.T) {
	setupEpayCallbackTest(t)
	withQuotaPerUnit(t, 500_000)
	withTopUpPricing(t, testAmountDiscount, testTierDiscount)
	withFaceTotal(t, 3000, nil)

	recorder := tronHandlerResponse(t, http.MethodGet, "/", "", 9415, GetTopUpInfo)
	var response struct {
		Data struct {
			Discount map[string]float64 `json:"discount"`
			Bonus    map[string]float64 `json:"bonus"`
			Tier     map[string]any     `json:"tier"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.InDelta(t, 0.97, response.Data.Discount["500"], 1e-9)
	assert.Empty(t, response.Data.Bonus)
	assert.InDelta(t, 0.97, response.Data.Tier["discount"], 1e-9)
	assert.NotContains(t, response.Data.Tier, "bonus")
}

func TestUpdateOption_RejectsBrokenBonusTable(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	assert.Error(t, model.UpdateOption(operation_setting.AmountBonusOptionKey, `{"200":0.98}`), "把折扣写进赠送表")
	assert.Error(t, model.UpdateOption(operation_setting.TierBonusOptionKey, `{"1000":0.05,"2500":0.03}`), "高档少送")
}

func bonusGatewayContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return ctx, recorder
}

func TestUnsupportedGatewaysRefuseOrdersThatWouldHaveABonus(t *testing.T) {
	setupEpayCallbackTest(t)
	withTopUpBonus(t, testAmountBonus, testTierBonus)
	withFaceTotal(t, 0, nil)

	ctx, recorder := bonusGatewayContext()
	assert.True(t, topUpBonusRefused(ctx, 500, 9416, "Waffo"), "500 有赠送，未接入的渠道要拒绝")
	assert.Contains(t, recorder.Body.String(), topUpBonusUnsupportedMessage)

	ctx, recorder = bonusGatewayContext()
	assert.False(t, topUpBonusRefused(ctx, 100, 9416, "Waffo"), "100 没有赠送，照常")
	assert.Empty(t, recorder.Body.String())

	withFaceTotal(t, 0, errors.New("timeout"))
	ctx, recorder = bonusGatewayContext()
	assert.True(t, topUpBonusRefused(ctx, 100, 9416, "Waffo"), "读累计失败同样拒绝")
	assert.Contains(t, recorder.Body.String(), topUpTierUnavailableMessage)
}
