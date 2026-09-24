package service

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enableCentRounding(t *testing.T) {
	t.Helper()
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	old := *operation_setting.GetQuotaSetting()
	rate := operation_setting.USDExchangeRate
	operation_setting.GetQuotaSetting().RoundChargeToCNYCent = true
	operation_setting.USDExchangeRate = 1
	t.Cleanup(func() { *operation_setting.GetQuotaSetting() = old; operation_setting.USDExchangeRate = rate })
}

func TestCentRoundingWalletReserveSettleAndLog(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 701, 100000)
	seedToken(t, 701, 701, "cent-wallet", 100000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 701, TokenId: 701, TokenKey: "cent-wallet", ForcePreConsume: true}
	info.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, PreConsumeBilling(c, 1, info))
	assert.Equal(t, 5000, info.Billing.GetPreConsumedQuota())
	require.NoError(t, info.Billing.Reserve(5001))
	assert.Equal(t, 90000, getUserQuota(t, 701))
	// Mid-flight config changes do not change the request snapshot.
	operation_setting.GetQuotaSetting().RoundChargeToCNYCent = false
	operation_setting.USDExchangeRate = 7.3
	require.NoError(t, SettleBilling(c, info, 99))
	require.NoError(t, SettleBilling(c, info, 99))
	assert.Equal(t, 95000, getUserQuota(t, 701))
	assert.Equal(t, 95000, getTokenRemainQuota(t, 701))
	other := map[string]interface{}{}
	AttachBillingCharge(info, other)
	assert.Equal(t, 99, info.BillingCharge.RawQuota)
	assert.Equal(t, 5000, info.BillingCharge.ChargedQuota)
	require.Contains(t, other, "charge_rounding")
}

func TestCentRoundingSubscriptionSettle(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 702, 100000)
	seedToken(t, 702, 702, "cent-sub", 100000)
	seedSubscription(t, 702, 702, 100000, 5000)
	info := &relaycommon.RelayInfo{UserId: 702, TokenId: 702, TokenKey: "cent-sub", SubscriptionId: 702, BillingSource: BillingSourceSubscription}
	session := &BillingSession{relayInfo: info, funding: &SubscriptionFunding{subscriptionId: 702}, preConsumedQuota: 5000}
	require.NoError(t, session.Settle(5001))
	require.NoError(t, session.Settle(5001))
	assert.EqualValues(t, 10000, getSubscriptionUsed(t, 702))
	assert.Equal(t, 95000, getTokenRemainQuota(t, 702))
	assert.Equal(t, 10000, info.BillingCharge.ChargedQuota)
}

func TestCentRoundingTaskSameCentAndRefund(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 703, 95000)
	seedToken(t, 703, 703, "cent-task", 95000)
	task := makeTask(703, 0, 5000, 703, BillingSourceWallet, 0)
	task.PrivateData.BillingCharge = &common.BillingCharge{RawQuota: 1, ChargedQuota: 5000, Policy: common.CNYCentRounding{Enabled: true, USDToCNY: 1, QuotaPerUnit: 500000}}
	require.NoError(t, model.DB.Create(task).Error)
	RecalculateTaskQuota(context.Background(), task, 4999, "same cent")
	assert.Equal(t, 95000, getUserQuota(t, 703))
	assert.Equal(t, 4999, task.PrivateData.BillingCharge.RawQuota)
	operation_setting.USDExchangeRate = 7.3
	RecalculateTaskQuota(context.Background(), task, 5001, "next cent")
	RecalculateTaskQuota(context.Background(), task, 5001, "duplicate")
	assert.Equal(t, 90000, getUserQuota(t, 703))
	assert.Equal(t, 10000, task.Quota)
	require.True(t, RefundTaskQuota(context.Background(), task, "failure"))
	require.True(t, RefundTaskQuota(context.Background(), task, "duplicate failure"))
	assert.Equal(t, 100000, getUserQuota(t, 703))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 703))
	assert.Equal(t, 5001, getLastLog(t).Quota)
}

func TestCentRoundingMidjourneyRefund(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	seedUser(t, 704, 95000)
	seedToken(t, 704, 704, "cent-mj", 95000)
	charge := &common.BillingCharge{RawQuota: 12, ChargedQuota: 5000, Policy: common.CNYCentRounding{Enabled: true, USDToCNY: 1, QuotaPerUnit: 500000}}
	task := &model.Midjourney{UserId: 704, TokenId: 704, Quota: 5000, BillingCharge: charge}
	require.NoError(t, model.DB.Create(task).Error)
	t.Cleanup(func() { model.DB.Delete(task) })
	stale := *task
	old, changed, err := model.RefundRoundedMidjourney(task)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 12, old.RawQuota)
	_, changed, err = model.RefundRoundedMidjourney(&stale)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, 100000, getUserQuota(t, 704))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 704))
}

func TestCentRoundingTextLogPreservesRawQuota(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 705, 100000)
	seedToken(t, 705, 705, "cent-text", 100000)
	seedChannel(t, 705)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{UserId: 705, TokenId: 705, TokenKey: "cent-text", OriginModelName: "test-model", StartTime: time.Now(), PriceData: hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: 705}
	info.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, PreConsumeBilling(c, 5, info))
	PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 7, CompletionTokens: 5, TotalTokens: 12}, nil)
	assert.Equal(t, 95000, getUserQuota(t, 705))
	assert.Equal(t, 95000, getTokenRemainQuota(t, 705))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 12, log.Quota)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	rounding, ok := other["charge_rounding"].(map[string]interface{})
	require.True(t, ok)
	assert.EqualValues(t, 5000, rounding["charged_quota"])
	var user model.User
	require.NoError(t, model.DB.First(&user, 705).Error)
	assert.Equal(t, 5000, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, 705).Error)
	assert.EqualValues(t, 12, channel.UsedQuota)
}

func TestCentRoundingFreeSettlementAndFailedPreconsume(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 706, 4999)
	seedToken(t, 706, 706, "cent-poor", 10000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 706, TokenId: 706, TokenKey: "cent-poor"}
	info.UserSetting.BillingPreference = "wallet_only"
	require.NotNil(t, PreConsumeBilling(c, 1, info))
	assert.Equal(t, 4999, getUserQuota(t, 706))
	assert.Equal(t, 10000, getTokenRemainQuota(t, 706))
	require.NoError(t, SettleBilling(c, info, 0))
	assert.Zero(t, info.BillingCharge.ChargedQuota)
	assert.Equal(t, 4999, getUserQuota(t, 706))
}

func TestCentRoundingTaskStaleRetryAndRollback(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 707, 100000)
	seedToken(t, 707, 707, "cent-task-sub", 95000)
	seedSubscription(t, 707, 707, 10000, 5000)
	task := makeTask(707, 0, 5000, 707, BillingSourceSubscription, 707)
	task.PrivateData.BillingCharge = &common.BillingCharge{RawQuota: 1, ChargedQuota: 5000, Policy: common.CNYCentRounding{Enabled: true, USDToCNY: 1, QuotaPerUnit: 500000}}
	require.NoError(t, model.DB.Create(task).Error)
	stale := *task
	RecalculateTaskQuota(context.Background(), task, 5001, "next cent")
	RecalculateTaskQuota(context.Background(), &stale, 5001, "stale retry")
	assert.EqualValues(t, 10000, getSubscriptionUsed(t, 707))
	assert.Equal(t, 90000, getTokenRemainQuota(t, 707))
	RecalculateTaskQuota(context.Background(), task, 10001, "insufficient subscription")
	assert.Equal(t, 10000, task.Quota)
	assert.EqualValues(t, 10000, getSubscriptionUsed(t, 707))
	assert.Equal(t, 90000, getTokenRemainQuota(t, 707))
	require.True(t, RefundTaskQuota(context.Background(), task, "failed"))
	require.True(t, RefundTaskQuota(context.Background(), &stale, "stale refund"))
	assert.EqualValues(t, 0, getSubscriptionUsed(t, 707))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 707))
}

func TestCentRoundingRealtimeReservesCumulativeTotal(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 708, 100000)
	seedToken(t, 708, 708, "cent-realtime", 100000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 708, TokenId: 708, TokenKey: "cent-realtime", OriginModelName: "gpt-4o-realtime-preview", UsingGroup: "default"}
	info.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, PreConsumeBilling(c, 1, info))
	usage := &dto.RealtimeUsage{}
	usage.InputTokenDetails.TextTokens = 1
	require.NoError(t, PreWssConsumeQuota(c, info, usage))
	require.NoError(t, PreWssConsumeQuota(c, info, usage))
	assert.Equal(t, 95000, getUserQuota(t, 708))
	require.NoError(t, SettleBilling(c, info, info.RealtimeRawQuota))
	assert.Equal(t, 95000, getUserQuota(t, 708))
	assert.Equal(t, 95000, getTokenRemainQuota(t, 708))
}

func TestCentRoundingRealtimeRejectsUnfundedNextCent(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 709, 5000)
	seedToken(t, 709, 709, "cent-realtime-poor", 100000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 709, TokenId: 709, TokenKey: "cent-realtime-poor", TokenUnlimited: true, OriginModelName: "gpt-4o-realtime-preview", UsingGroup: "default"}
	info.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, PreConsumeBilling(c, 1, info))
	info.RealtimeRawQuota = 5000
	usage := &dto.RealtimeUsage{}
	usage.InputTokenDetails.TextTokens = 1
	require.Error(t, PreWssConsumeQuota(c, info, usage))
	assert.Equal(t, 0, getUserQuota(t, 709))
	assert.Equal(t, 5000, info.Billing.GetPreConsumedQuota())
}

func TestCentRoundingRealtimeSubscriptionDoesNotRequireWallet(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 710, 0)
	seedToken(t, 710, 710, "cent-realtime-sub", 100000)
	seedSubscription(t, 710, 710, 10000, 5000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 710, TokenId: 710, TokenKey: "cent-realtime-sub", SubscriptionId: 710, BillingSource: BillingSourceSubscription, OriginModelName: "gpt-4o-realtime-preview", UsingGroup: "default", RealtimeRawQuota: 5000}
	session := &BillingSession{relayInfo: info, funding: &SubscriptionFunding{subscriptionId: 710}, preConsumedQuota: 5000}
	info.Billing = session
	usage := &dto.RealtimeUsage{}
	usage.InputTokenDetails.TextTokens = 1
	require.NoError(t, PreWssConsumeQuota(c, info, usage))
	assert.EqualValues(t, 10000, getSubscriptionUsed(t, 710))
	assert.Zero(t, getUserQuota(t, 710))
	info.RealtimeRawQuota = 10000
	require.Error(t, PreWssConsumeQuota(c, info, usage))
	assert.EqualValues(t, 10000, getSubscriptionUsed(t, 710))
}

func TestCentRoundingPreconsumeRefundsActualReservation(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 711, 100000)
	seedToken(t, 711, 711, "cent-refund", 100000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 711, TokenId: 711, TokenKey: "cent-refund"}
	info.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, PreConsumeBilling(c, 1, info))
	require.NoError(t, info.Billing.Reserve(5001))
	info.Billing.Refund(c)
	info.Billing.Refund(c)
	require.Eventually(t, func() bool { return getUserQuota(t, 711) == 100000 && getTokenRemainQuota(t, 711) == 100000 }, time.Second, time.Millisecond)
	require.Error(t, info.Billing.Settle(1))
}

func TestCentRoundingFailedSettlementRecordsOnlyActualReservation(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 712, 100000)
	seedToken(t, 712, 712, "cent-sub-full", 95000)
	seedSubscription(t, 712, 712, 5000, 5000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 712, TokenId: 712, TokenKey: "cent-sub-full", SubscriptionId: 712, BillingSource: BillingSourceSubscription}
	info.Billing = &BillingSession{relayInfo: info, funding: &SubscriptionFunding{subscriptionId: 712}, preConsumedQuota: 5000}
	require.Error(t, SettleBilling(c, info, 5001))
	require.NotNil(t, info.BillingCharge)
	assert.Equal(t, 5000, info.BillingCharge.ChargedQuota)
	assert.Equal(t, 5001, info.BillingCharge.RawQuota)
}

func TestCentRoundingMJChargeFailureRollsBackWallet(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	seedUser(t, 713, 100000)
	seedToken(t, 713, 713, "cent-mj-fail", 100000)
	task := &model.Midjourney{UserId: 713, TokenId: 713, Quota: 5000, BillingCharge: &common.BillingCharge{RawQuota: 1, ChargedQuota: 5000, Policy: common.CNYCentRounding{Enabled: true, USDToCNY: 1, QuotaPerUnit: 500000}}}
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("test:fail_cent_token", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(fmt.Errorf("injected token write failure"))
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove("test:fail_cent_token") })
	require.Error(t, model.InsertChargedMidjourney(task))
	assert.Equal(t, 100000, getUserQuota(t, 713))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 713))
	var count int64
	require.NoError(t, model.DB.Model(&model.Midjourney{}).Where("user_id = ?", 713).Count(&count).Error)
	assert.Zero(t, count)
}

func TestCentRoundingMJAcceptedTaskSurvivesBillingFailure(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	seedUser(t, 714, 100000)
	seedToken(t, 714, 714, "cent-mj-track", 100000)
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("test:fail_cent_track_token", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(fmt.Errorf("injected token write failure"))
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove("test:fail_cent_track_token") })
	info := &relaycommon.RelayInfo{UserId: 714, TokenId: 714, TokenKey: "cent-mj-track"}
	task := &model.Midjourney{UserId: 714, MjId: "accepted-upstream-task"}
	require.NoError(t, InsertMidjourneyTask(info, task, 1, true))
	t.Cleanup(func() { model.DB.Delete(task) })
	var saved model.Midjourney
	require.NoError(t, model.DB.First(&saved, task.Id).Error)
	assert.Equal(t, "accepted-upstream-task", saved.MjId)
	assert.Zero(t, saved.Quota)
	require.NotNil(t, saved.BillingCharge)
	assert.NotEmpty(t, saved.BillingCharge.SettlementError)
	assert.Equal(t, 100000, getUserQuota(t, 714))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 714))
}

func TestCentRoundingMJInsertFailureRollsBackDebit(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	seedUser(t, 715, 100000)
	seedToken(t, 715, 715, "cent-mj-insert", 100000)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test:fail_cent_mj_insert", func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			tx.AddError(fmt.Errorf("injected task insert failure"))
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Create().Remove("test:fail_cent_mj_insert") })
	task := &model.Midjourney{UserId: 715, TokenId: 715, Quota: 5000, BillingCharge: &common.BillingCharge{RawQuota: 1, ChargedQuota: 5000}}
	require.Error(t, model.InsertChargedMidjourney(task))
	assert.Equal(t, 100000, getUserQuota(t, 715))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 715))
}

func TestCentRoundingMJSuccessAndFree(t *testing.T) {
	for _, raw := range []int{1, 0} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			truncate(t)
			enableCentRounding(t)
			require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
			seedUser(t, 716, 100000)
			seedToken(t, 716, 716, "cent-mj-ok", 100000)
			info := &relaycommon.RelayInfo{UserId: 716, TokenId: 716, TokenKey: "cent-mj-ok"}
			task := &model.Midjourney{UserId: 716, MjId: "accepted"}
			require.NoError(t, InsertMidjourneyTask(info, task, raw, true))
			t.Cleanup(func() { model.DB.Delete(task) })
			want := 0
			if raw > 0 {
				want = 5000
			}
			assert.Equal(t, 100000-want, getUserQuota(t, 716))
			assert.Equal(t, 100000-want, getTokenRemainQuota(t, 716))
			var saved model.Midjourney
			require.NoError(t, model.DB.First(&saved, task.Id).Error)
			assert.Equal(t, want, saved.Quota)
			assert.Equal(t, raw, saved.BillingCharge.RawQuota)
			_, _, err := model.RefundRoundedMidjourney(&saved)
			require.NoError(t, err)
			assert.Equal(t, 100000, getUserQuota(t, 716))
			assert.Equal(t, 100000, getTokenRemainQuota(t, 716))
		})
	}
}

func TestCentRoundingOverflowSettlementKeepsRefundableReservation(t *testing.T) {
	truncate(t)
	enableCentRounding(t)
	seedUser(t, 717, 100000)
	seedToken(t, 717, 717, "cent-overflow", 100000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 717, TokenId: 717, TokenKey: "cent-overflow"}
	info.UserSetting.BillingPreference = "wallet_only"
	require.Nil(t, PreConsumeBilling(c, 1, info))
	require.Error(t, SettleBilling(c, info, common.MaxQuota))
	require.NotNil(t, info.BillingCharge)
	assert.Equal(t, 5000, ChargedQuota(info, common.MaxQuota))
	task := makeTask(717, 0, ChargedQuota(info, common.MaxQuota), 717, BillingSourceWallet, 0)
	task.PrivateData.BillingCharge = info.BillingCharge
	require.NoError(t, model.DB.Create(task).Error)
	require.True(t, RefundTaskQuota(context.Background(), task, "overflow task failed"))
	assert.Equal(t, 100000, getUserQuota(t, 717))
	assert.Equal(t, 100000, getTokenRemainQuota(t, 717))
}
