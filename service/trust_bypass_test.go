package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 信任额度是 10 个单位（¥10）：余额与令牌都高于它，才轮得到信任旁路。
var richQuota = common.GetTrustQuota() + 1000000

func setQuotaPolicy(t *testing.T, rounding bool, trust bool) {
	t.Helper()
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	old := *operation_setting.GetQuotaSetting()
	rate := operation_setting.USDExchangeRate
	operation_setting.GetQuotaSetting().RoundChargeToCNYCent = rounding
	operation_setting.GetQuotaSetting().TrustBypassEnabled = trust
	operation_setting.USDExchangeRate = 1
	t.Cleanup(func() { *operation_setting.GetQuotaSetting() = old; operation_setting.USDExchangeRate = rate })
}

func richWalletRequest(t *testing.T, id int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	seedUser(t, id, richQuota)
	seedToken(t, id, id, "trust-bypass", richQuota)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("token_quota", richQuota)
	info := &relaycommon.RelayInfo{UserId: id, TokenId: id, TokenKey: "trust-bypass"}
	info.UserSetting.BillingPreference = "wallet_only"
	return c, info
}

// 取整关掉以后，默认仍然一律预扣：不因为余额充足就跳过（2026-10-19 起取整与信任旁路解耦）。
func TestTrustBypassOffByDefaultStillPreConsumesRichWallet(t *testing.T) {
	truncate(t)
	setQuotaPolicy(t, false, false)
	c, info := richWalletRequest(t, 901)
	require.Nil(t, PreConsumeBilling(c, 1234, info))
	assert.Equal(t, 1234, info.Billing.GetPreConsumedQuota())
	assert.Equal(t, richQuota-1234, getUserQuota(t, 901))
	assert.Equal(t, richQuota-1234, getTokenRemainQuota(t, 901))
	require.NoError(t, SettleBilling(c, info, 1500))
	assert.Equal(t, richQuota-1500, getUserQuota(t, 901))
	assert.Equal(t, richQuota-1500, getTokenRemainQuota(t, 901))
}

// 显式打开才恢复上游行为：不预扣，结束时一次扣全额。
func TestTrustBypassOnSkipsPreConsumeAndSettlesFullAmount(t *testing.T) {
	truncate(t)
	setQuotaPolicy(t, false, true)
	c, info := richWalletRequest(t, 902)
	require.Nil(t, PreConsumeBilling(c, 1234, info))
	assert.Equal(t, 0, info.Billing.GetPreConsumedQuota())
	assert.Equal(t, richQuota, getUserQuota(t, 902))
	require.NoError(t, SettleBilling(c, info, 1500))
	assert.Equal(t, richQuota-1500, getUserQuota(t, 902))
	assert.Equal(t, richQuota-1500, getTokenRemainQuota(t, 902))
}

// 按分取整开着时，无论信任开关怎么设都预扣（与 r10 以来的行为一致）。
func TestTrustBypassNeverAppliesWhileCentRounding(t *testing.T) {
	truncate(t)
	setQuotaPolicy(t, true, true)
	c, info := richWalletRequest(t, 903)
	require.Nil(t, PreConsumeBilling(c, 1234, info))
	assert.Equal(t, 5000, info.Billing.GetPreConsumedQuota())
	assert.Equal(t, richQuota-5000, getUserQuota(t, 903))
}

// 开关在请求中途改了，不影响这一笔已经做出的预扣决定。
func TestTrustBypassDecisionIsFixedAtPreConsume(t *testing.T) {
	truncate(t)
	setQuotaPolicy(t, false, false)
	c, info := richWalletRequest(t, 904)
	require.Nil(t, PreConsumeBilling(c, 1234, info))
	operation_setting.GetQuotaSetting().TrustBypassEnabled = true
	require.NoError(t, info.Billing.Reserve(3000))
	assert.Equal(t, 3000, info.Billing.GetPreConsumedQuota())
	require.NoError(t, SettleBilling(c, info, 2000))
	assert.Equal(t, richQuota-2000, getUserQuota(t, 904))
	assert.Equal(t, richQuota-2000, getTokenRemainQuota(t, 904))
}
