package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 钱包上限的下单前检查在控制器这一层要做到两件事：超限的订单不创建；
// 给客户的那句话说得清「最多还能充多少」。

func walletFull(balanceUnits int64, reservedUnits int64, pending int) *model.TopUpWalletFullError {
	unit := int64(common.QuotaPerUnit)
	return &model.TopUpWalletFullError{
		Capacity: model.TopUpCapacity{
			Balance: balanceUnits * unit, Reserved: reservedUnits * unit, Pending: pending, Limit: int64(common.MaxQuota),
		},
		CreditQuota: 2000 * unit,
	}
}

func withQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	original := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() { common.QuotaPerUnit = original })
}

func TestTopUpWalletFullMessage_TellsHowMuchStillFits(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	// 上限 2147483647 / 500000 ≈ 4294.97；余额 2522 时还装得下 1772（向下取整）
	message := topUpWalletFullMessage(walletFull(2522, 0, 0), 1)
	assert.Contains(t, message, "钱包上限")
	assert.Contains(t, message, "最多可充 1772")
	assert.NotContains(t, message, "未完成")
}

func TestTopUpWalletFullMessage_MentionsPendingOrdersHoldingRoom(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	message := topUpWalletFullMessage(walletFull(522, 2000, 1), 1)
	assert.Contains(t, message, "最多可充 1772")
	assert.Contains(t, message, "1 笔未完成的充值订单")
}

func TestTopUpWalletFullMessage_WhenNothingFitsSaysSpendFirst(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	// 余额 4294：剩不到 1 个单位
	message := topUpWalletFullMessage(walletFull(4294, 0, 0), 1)
	assert.Contains(t, message, "暂时不能再充值")
	assert.NotContains(t, message, "最多可充")

	// 剩 94 个单位，但这个渠道最低要充 100：同样是「装不下一笔」
	message = topUpWalletFullMessage(walletFull(4200, 0, 0), 100)
	assert.Contains(t, message, "暂时不能再充值")
}

func TestTopUpWalletFullMessage_UsesRequestUnitsInTokensDisplay(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	setting := operation_setting.GetGeneralSetting()
	original := setting.QuotaDisplayType
	setting.QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	t.Cleanup(func() { setting.QuotaDisplayType = original })

	// 按 tokens 展示时，下单数量的单位就是额度：1772 个充值单位 = 886000000
	message := topUpWalletFullMessage(walletFull(2522, 0, 0), 1)
	assert.Contains(t, message, "最多可充 886000000")
}

func seedWalletUser(t *testing.T, userID int, balanceUnits int) {
	t.Helper()
	originalGroup := getTopUpUserGroup
	getTopUpUserGroup = func(_ int, _ bool) (string, error) { return "default", nil }
	t.Cleanup(func() { getTopUpUserGroup = originalGroup })
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "wallet-user", AffCode: "wallet-aff", Status: common.UserStatusEnabled,
		Quota: balanceUnits * int(common.QuotaPerUnit), Group: "default",
	}).Error)
}

func topUpCount(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func TestRequestEpay_RefusesOrderThatWouldOverflowWallet(t *testing.T) {
	db := setupEpayCallbackTest(t)
	require.NoError(t, db.AutoMigrate(&model.TronTopupOrder{}))
	withQuotaPerUnit(t, 500_000)
	seedWalletUser(t, 7101, 2522)

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":2000,"payment_method":"alipay"}`, 7101, RequestEpay)
	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"message":"error"`)
	assert.Contains(t, body, "钱包上限")
	assert.Contains(t, body, "最多可充 1772")
	assert.NotContains(t, body, "pay.example.com", "被拒的订单不能把支付链接交出去")
	assert.Zero(t, topUpCount(t, 7101))

	// 降到装得下的金额，照常下单
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":1000,"payment_method":"alipay"}`, 7101, RequestEpay)
	assert.Contains(t, recorder.Body.String(), `"message":"success"`)
	assert.Equal(t, int64(1), topUpCount(t, 7101))

	// 刚下的那张 1000 还没付：再来 1000 就是 2522 + 1000 + 1000，装不下
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":1000,"payment_method":"alipay"}`, 7101, RequestEpay)
	body = recorder.Body.String()
	assert.Contains(t, body, `"message":"error"`)
	assert.Contains(t, body, "最多可充 772")
	assert.Contains(t, body, "1 笔未完成的充值订单")
	assert.Equal(t, int64(1), topUpCount(t, 7101))
}

// 单笔金额本身超过钱包上限：以前能下单、能付款，付完卡在待处理。
func TestRequestEpay_RefusesSingleOrderLargerThanTheWallet(t *testing.T) {
	db := setupEpayCallbackTest(t)
	require.NoError(t, db.AutoMigrate(&model.TronTopupOrder{}))
	withQuotaPerUnit(t, 500_000)
	seedWalletUser(t, 7102, 0)

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":5000,"payment_method":"alipay"}`, 7102, RequestEpay)
	body := recorder.Body.String()
	assert.Contains(t, body, `"message":"error"`)
	assert.Contains(t, body, "最多可充 4294")
	assert.Zero(t, topUpCount(t, 7102))
}

func TestCreateTronTopupOrder_ExplainsWalletLimitInsteadOfGenericFailure(t *testing.T) {
	withQuotaPerUnit(t, 500_000)
	operations := &stubTronTopupOperations{createErr: walletFull(2522, 0, 0)}
	withTronControllerDependencies(t, operations)
	getTronPublicConfig = func() (service.TronTopupConfig, bool) {
		return service.TronTopupConfig{Enabled: true, Network: "mainnet", ReceiveAddress: "TQ2FF8nGsASkSJq6xW8MXhgbdAH6MDd83f", MinAmount: 100}, true
	}

	recorder := tronHandlerResponse(t, http.MethodPost, "/", `{"amount":2000}`, 42, CreateTronTopupOrder)
	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"success":false`)
	assert.Contains(t, body, "最多可充 1772")
	assert.NotContains(t, body, "订单创建失败")

	// 剩余容量不够这个渠道的最低充值额
	operations.createErr = walletFull(4250, 0, 0)
	recorder = tronHandlerResponse(t, http.MethodPost, "/", `{"amount":100}`, 42, CreateTronTopupOrder)
	assert.Contains(t, recorder.Body.String(), "暂时不能再充值")
}
