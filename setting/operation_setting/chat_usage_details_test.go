package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// withChatUsageDetailsFillChannels 临时点名一批渠道并发布快照，测试结束后还原。
func withChatUsageDetailsFillChannels(t *testing.T, channels []int) {
	t.Helper()
	saved := generalSetting.ChatUsageDetailsFillChannels
	generalSetting.ChatUsageDetailsFillChannels = channels
	RefreshChatUsageDetailsFill()
	t.Cleanup(func() {
		generalSetting.ChatUsageDetailsFillChannels = saved
		RefreshChatUsageDetailsFill()
	})
}

func TestChatUsageDetailsFill_DefaultIsOffForEveryChannel(t *testing.T) {
	require.Empty(t, GetGeneralSetting().ChatUsageDetailsFillChannels)
	require.False(t, ShouldFillChatUsageDetails(2))
}

func TestChatUsageDetailsFill_OnlyListedChannels(t *testing.T) {
	withChatUsageDetailsFillChannels(t, []int{2, 9})

	require.True(t, ShouldFillChatUsageDetails(2))
	require.True(t, ShouldFillChatUsageDetails(9))
	require.False(t, ShouldFillChatUsageDetails(1))
	require.False(t, ShouldFillChatUsageDetails(20))
	require.False(t, ShouldFillChatUsageDetails(0))
}

func TestChatUsageDetailsFill_NonPositiveIdsAreIgnored(t *testing.T) {
	withChatUsageDetailsFillChannels(t, []int{0, -2, 5})

	require.False(t, ShouldFillChatUsageDetails(0))
	require.False(t, ShouldFillChatUsageDetails(-2))
	require.True(t, ShouldFillChatUsageDetails(5))
}

// 快照发布后不再跟着配置字段变：请求路径读到的要么是旧的一整份，要么是新的一整份。
func TestChatUsageDetailsFill_SnapshotDoesNotAliasSetting(t *testing.T) {
	channels := []int{2}
	withChatUsageDetailsFillChannels(t, channels)

	channels[0] = 7

	require.True(t, ShouldFillChatUsageDetails(2))
	require.False(t, ShouldFillChatUsageDetails(7))
}

func TestChatUsageDetailsFill_ClearingTurnsItOff(t *testing.T) {
	withChatUsageDetailsFillChannels(t, []int{2})
	require.True(t, ShouldFillChatUsageDetails(2))

	generalSetting.ChatUsageDetailsFillChannels = []int{}
	RefreshChatUsageDetailsFill()

	require.False(t, ShouldFillChatUsageDetails(2))
}

func TestValidateChatUsageDetailsFillChannels(t *testing.T) {
	valid := []string{`[]`, `[2]`, `[2,7]`, ` [ 2 , 7 ] `}
	for _, value := range valid {
		require.NoError(t, ValidateChatUsageDetailsFillChannels(value), value)
	}

	invalid := []string{
		``, `null`, `{}`, `2`, `"2"`, // 不是数组
		`[null]`, `["2"]`, `[2.5]`, `[2.0]`, `[true]`, // 元素不是整数
		`[0]`, `[-1]`, // 不是正整数
		`[2,2]`,         // 重复
		`[2,]`, `[2] x`, // 不是合法 JSON
	}
	for _, value := range invalid {
		require.Error(t, ValidateChatUsageDetailsFillChannels(value), value)
	}
}
