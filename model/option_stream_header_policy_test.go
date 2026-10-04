package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// 管理接口 / 启动加载 / 定时同步都走 handleConfigUpdate：它必须在写完配置后发布快照，
// 请求路径才看得到新值。
func TestHandleConfigUpdate_PublishesStreamHeaderPolicy(t *testing.T) {
	t.Cleanup(func() {
		handleConfigUpdate("general_setting.stream_header_timeout_by_channel", "{}")
		handleConfigUpdate("general_setting.stream_header_total_budget_seconds", "0")
	})

	require.True(t, handleConfigUpdate("general_setting.stream_header_timeout_by_channel", `{"6":40}`))
	require.True(t, handleConfigUpdate("general_setting.stream_header_total_budget_seconds", "95"))

	require.Equal(t, 40*time.Second, operation_setting.StreamHeaderTimeout(6))
	require.Equal(t, 95*time.Second, operation_setting.StreamHeaderTotalBudget())

	require.True(t, handleConfigUpdate("general_setting.stream_header_timeout_by_channel", "{}"))
	require.Zero(t, operation_setting.StreamHeaderTimeout(6))
}
