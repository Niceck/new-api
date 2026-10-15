package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func withGroupRatiosForEntryTest(t *testing.T, ratios string) {
	t.Helper()
	original := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(ratios))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(original))
	})
}

// 管理接口 / 启动加载 / 定时同步都走 handleConfigUpdate：它必须在写完配置后发布快照，
// 请求路径才看得到新登记的入口限制。
func TestHandleConfigUpdate_PublishesGroupEntryHosts(t *testing.T) {
	const key = "general_setting.group_entry_hosts"
	t.Cleanup(func() { handleConfigUpdate(key, "{}") })

	require.True(t, handleConfigUpdate(key, `{"claude kiro":["kiro.yzzt.top"]}`))
	require.False(t, operation_setting.GroupAllowedOnHost("claude kiro", "cc.yzzt.top:2087"))
	require.True(t, operation_setting.GroupAllowedOnHost("claude kiro", "kiro.yzzt.top"))
	require.True(t, operation_setting.GroupAllowedOnHost("满血稳定claude", "cc.yzzt.top"))

	require.True(t, handleConfigUpdate(key, "{}"))
	require.True(t, operation_setting.GroupAllowedOnHost("claude kiro", "cc.yzzt.top:2087"))
}

func TestHandleConfigUpdate_PublishesGroupEntryRejectMessage(t *testing.T) {
	const hostsKey = "general_setting.group_entry_hosts"
	const messageKey = "general_setting.group_entry_reject_message"
	t.Cleanup(func() {
		handleConfigUpdate(hostsKey, "{}")
		handleConfigUpdate(messageKey, "")
	})

	require.True(t, handleConfigUpdate(hostsKey, `{"claude kiro":["kiro.yzzt.top"]}`))
	require.True(t, handleConfigUpdate(messageKey, "{group} 请改用 https://{entry}"))
	require.Equal(t, "claude kiro 请改用 https://kiro.yzzt.top", operation_setting.GroupEntryRejectMessage("claude kiro"))
}

// 库里解析不了的值（绕过校验直接改库）不会让进程出错，也不会冲掉已生效的限制。
func TestHandleConfigUpdate_UnparseableGroupEntryHostsKeepsCurrentRestriction(t *testing.T) {
	const key = "general_setting.group_entry_hosts"
	t.Cleanup(func() { handleConfigUpdate(key, "{}") })

	require.True(t, handleConfigUpdate(key, `{"claude kiro":["kiro.yzzt.top"]}`))
	require.True(t, handleConfigUpdate(key, `{"claude kiro":"kiro.yzzt.top"}`))
	require.False(t, operation_setting.GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))
}

// validateOptionValue 是单项写入（UpdateOption）与批量写入（UpdateOptionsBulk）的共同入口。
func TestValidateOptionValue_GroupEntryHosts(t *testing.T) {
	withGroupRatiosForEntryTest(t, `{"claude kiro":0.4,"满血稳定claude":1.75}`)
	const key = "general_setting.group_entry_hosts"

	require.NoError(t, validateOptionValue(key, `{}`))
	require.NoError(t, validateOptionValue(key, `{"claude kiro":["kiro.yzzt.top"]}`))

	require.ErrorContains(t, validateOptionValue(key, `null`), "清空请写 {}")
	require.ErrorContains(t, validateOptionValue(key, `{"claude kiro":"kiro.yzzt.top"}`), "JSON 对象")
	require.ErrorContains(t, validateOptionValue(key, `{"claude kiro":["https://kiro.yzzt.top"]}`), "主机名")
}

// 分组名写错的条目限制不了任何请求；写入时就要拦住，不能报成功。
func TestValidateOptionValue_GroupEntryHostsRejectsUnknownGroup(t *testing.T) {
	withGroupRatiosForEntryTest(t, `{"claude kiro":0.4,"满血稳定claude":1.75}`)
	const key = "general_setting.group_entry_hosts"

	err := validateOptionValue(key, `{"claude kiro":["kiro.yzzt.top"],"claude  kiro":["kiro.yzzt.top"]}`)
	require.ErrorContains(t, err, "claude  kiro")
	require.ErrorContains(t, err, "不存在")
	require.ErrorContains(t, validateOptionValue(key, `{"Claude Kiro":["kiro.yzzt.top"]}`), "不存在")
}

func TestValidateOptionValue_GroupEntryRejectMessage(t *testing.T) {
	const key = "general_setting.group_entry_reject_message"

	require.NoError(t, validateOptionValue(key, ""))
	require.NoError(t, validateOptionValue(key, "{group} 请改用 https://{entry}"))
	require.ErrorContains(t, validateOptionValue(key, "请改用 {host}"), "{host}")
	require.ErrorContains(t, validateOptionValue(key, "第一行\n第二行"), "控制字符")
}

// 对客接口地址进令牌页的一键导入链接：写入时就拦住带路径、结尾斜杠的值。
func TestValidateOptionValue_ApiBaseURL(t *testing.T) {
	const key = "general_setting.api_base_url"

	require.NoError(t, validateOptionValue(key, ""))
	require.NoError(t, validateOptionValue(key, "https://cc.yzzt.top"))
	require.ErrorContains(t, validateOptionValue(key, "https://cc.yzzt.top/"), "结尾斜杠")
	require.ErrorContains(t, validateOptionValue(key, "cc.yzzt.top"), "http")
}

// 经 handleConfigUpdate 落到字段上，/api/status 读到的就是新值。
func TestHandleConfigUpdate_AppliesApiBaseURL(t *testing.T) {
	const key = "general_setting.api_base_url"
	t.Cleanup(func() { handleConfigUpdate(key, "") })

	require.True(t, handleConfigUpdate(key, "https://cc.yzzt.top"))
	require.Equal(t, "https://cc.yzzt.top", operation_setting.GetGeneralSetting().ApiBaseURL)
}
