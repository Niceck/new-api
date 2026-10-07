package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// 管理接口 / 启动加载 / 定时同步都走 handleConfigUpdate：它必须在写完配置后发布快照，
// 请求路径才看得到新点名的渠道。
func TestHandleConfigUpdate_PublishesChatUsageDetailsFill(t *testing.T) {
	const key = "general_setting.chat_usage_details_fill_channels"
	t.Cleanup(func() { handleConfigUpdate(key, "[]") })

	require.True(t, handleConfigUpdate(key, "[2]"))
	require.True(t, operation_setting.ShouldFillChatUsageDetails(2))
	require.False(t, operation_setting.ShouldFillChatUsageDetails(7))

	require.True(t, handleConfigUpdate(key, "[]"))
	require.False(t, operation_setting.ShouldFillChatUsageDetails(2))
}
