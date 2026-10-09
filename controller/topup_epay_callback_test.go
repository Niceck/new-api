package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 易支付充值回调的端到端契约：验签 → 结算 → 应答。
//
// 应答决定平台重不重投：success = 已受理、不再投；fail = 请重投。所以
//   - 已入账、重投的幂等跳过、订单确实不存在 → success；
//   - 本次没能入账但重投可能成功（数据库故障）→ fail。
// 回错应答的代价不对称：该回 fail 时回了 success，客户付了钱却永远入不了账。

const epayCallbackTestKey = "test-key"

func setupEpayCallbackTest(t *testing.T) *gorm.DB {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousPayAddress, previousEpayID := operation_setting.PayAddress, operation_setting.EpayId
	previousEpayKey, previousPayMethods := operation_setting.EpayKey, operation_setting.PayMethods

	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = epayCallbackTestKey
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.True(t, isEpayWebhookEnabled())

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainType, previousLogType)
		operation_setting.PayAddress, operation_setting.EpayId = previousPayAddress, previousEpayID
		operation_setting.EpayKey, operation_setting.PayMethods = previousEpayKey, previousPayMethods
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedPendingEpayOrder(t *testing.T, db *gorm.DB, tradeNo string, userID int, startQuota int) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{
		Id: userID, Username: tradeNo + "-user", AffCode: "aff-" + tradeNo,
		Status: common.UserStatusEnabled, Quota: startQuota,
	}).Error)
	require.NoError(t, db.Create(&model.TopUp{
		UserId: userID, Amount: 10, Money: 10, TradeNo: tradeNo, PaymentMethod: "alipay",
		PaymentProvider: model.PaymentProviderEpay, CreateTime: common.GetTimestamp(),
		Status: common.TopUpStatusPending,
	}).Error)
}

// postEpayNotify 用商户密钥签一条平台形态的成功回调并投给 EpayNotify，返回应答正文。
// 金额与 seedPendingEpayOrder 建的订单一致。
func postEpayNotify(t *testing.T, tradeNo string) string {
	t.Helper()
	return postEpayNotifyPaying(t, tradeNo, "10.00")
}

// postEpayNotifyPaying 同上，但回调里的金额由调用方给：签名照样有效，只是金额可以
// 与订单不同——这正是金额比对要拦的那种通知。
func postEpayNotifyPaying(t *testing.T, tradeNo string, money string) string {
	t.Helper()
	signed, err := epay.GenerateParamsWithError(map[string]string{
		"pid": "epay_id", "type": "alipay", "money": money, "name": "TUC10",
		"out_trade_no": tradeNo, "trade_no": "2026100922001400000000000001",
		"trade_status": epay.StatusTradeSuccess,
	}, epayCallbackTestKey)
	require.NoError(t, err)
	query := url.Values{}
	for key, value := range signed {
		query.Set(key, value)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/user/epay/notify", EpayNotify)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/user/epay/notify?"+query.Encode(), nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder.Body.String()
}

func epayOrderState(t *testing.T, db *gorm.DB, tradeNo string, userID int) (status string, quota int) {
	t.Helper()
	topUp := &model.TopUp{}
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(topUp).Error)
	user := &model.User{}
	require.NoError(t, db.Select("id", "quota").Where("id = ?", userID).First(user).Error)
	return topUp.Status, user.Quota
}

func TestEpayNotifyCreditsOnceAndAcknowledges(t *testing.T) {
	db := setupEpayCallbackTest(t)
	seedPendingEpayOrder(t, db, "USR7NOcredit", 7, 100)

	require.Equal(t, "success", postEpayNotify(t, "USR7NOcredit"))
	status, credited := epayOrderState(t, db, "USR7NOcredit", 7)
	assert.Equal(t, common.TopUpStatusSuccess, status)
	assert.Greater(t, credited, 100)

	// 平台重投：照样回 success，额度不再加
	require.Equal(t, "success", postEpayNotify(t, "USR7NOcredit"))
	_, afterReplay := epayOrderState(t, db, "USR7NOcredit", 7)
	assert.Equal(t, credited, afterReplay)
}

func TestEpayNotifyAsksForRedeliveryWhenOrderLookupFails(t *testing.T) {
	db := setupEpayCallbackTest(t)
	seedPendingEpayOrder(t, db, "USR8NOdbdown", 8, 100)
	const hook = "test:fail_topup_lookup"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "top_ups" {
			_ = tx.AddError(errors.New("simulated lock wait timeout"))
		}
	}))

	// 查不了单：必须回 fail 让平台重投，不能当成「订单不存在」回 success
	body := postEpayNotify(t, "USR8NOdbdown")
	require.NoError(t, db.Callback().Query().Remove(hook))
	assert.Equal(t, "fail", body)
	status, quota := epayOrderState(t, db, "USR8NOdbdown", 8)
	assert.Equal(t, common.TopUpStatusPending, status)
	assert.Equal(t, 100, quota)

	// 故障消失后的重投：入账
	require.Equal(t, "success", postEpayNotify(t, "USR8NOdbdown"))
	status, quota = epayOrderState(t, db, "USR8NOdbdown", 8)
	assert.Equal(t, common.TopUpStatusSuccess, status)
	assert.Greater(t, quota, 100)
}

func TestEpayNotifyStopsRedeliveryForUnknownOrder(t *testing.T) {
	db := setupEpayCallbackTest(t)
	seedPendingEpayOrder(t, db, "USR9NOother", 9, 100)

	// 订单确实不存在：重投也不会变好，回 success 让平台停手；别人的订单不受影响
	require.Equal(t, "success", postEpayNotify(t, "USR9NOghost"))
	status, quota := epayOrderState(t, db, "USR9NOother", 9)
	assert.Equal(t, common.TopUpStatusPending, status)
	assert.Equal(t, 100, quota)
}

// 验签通过但金额与订单不符：不入账、不确认。回 fail 而不是 success——我们没有受理
// 这笔钱，不能告诉平台「已处理」；订单留在 pending，随后金额相符的通知照常入账。
func TestEpayNotifyRefusesToCreditWhenAmountDiffersFromOrder(t *testing.T) {
	db := setupEpayCallbackTest(t)
	seedPendingEpayOrder(t, db, "USR10NOamount", 10, 100)

	assert.Equal(t, "fail", postEpayNotifyPaying(t, "USR10NOamount", "0.01"))
	status, quota := epayOrderState(t, db, "USR10NOamount", 10)
	assert.Equal(t, common.TopUpStatusPending, status)
	assert.Equal(t, 100, quota, "签名有效也不能按订单额度入账")

	require.Equal(t, "success", postEpayNotifyPaying(t, "USR10NOamount", "10.00"))
	status, quota = epayOrderState(t, db, "USR10NOamount", 10)
	assert.Equal(t, common.TopUpStatusSuccess, status)
	assert.Greater(t, quota, 100)
}

// 已结清订单的重投即使金额对不上也回 success：订单早已入账，没有可重投出来的结果。
func TestEpayNotifyAcknowledgesReplayOfSettledOrderWhateverTheAmount(t *testing.T) {
	db := setupEpayCallbackTest(t)
	seedPendingEpayOrder(t, db, "USR11NOreplay", 11, 100)
	require.Equal(t, "success", postEpayNotify(t, "USR11NOreplay"))
	_, credited := epayOrderState(t, db, "USR11NOreplay", 11)

	assert.Equal(t, "success", postEpayNotifyPaying(t, "USR11NOreplay", "0.01"))
	_, afterReplay := epayOrderState(t, db, "USR11NOreplay", 11)
	assert.Equal(t, credited, afterReplay)
}
