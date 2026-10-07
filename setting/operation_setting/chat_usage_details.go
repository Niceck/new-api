package operation_setting

import (
	"fmt"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

// chatUsageDetailsFillSnapshot 是请求路径读取的只读快照：被点名的渠道号集合。
//
// 和 streamHeaderPolicySnapshot 同理：配置框架用反射直接改 generalSetting 的字段，
// 请求路径读快照而不读字段，快照发布后不再修改。
var chatUsageDetailsFillSnapshot atomic.Pointer[map[int]struct{}]

// RefreshChatUsageDetailsFill 把 general_setting 里点名的渠道发布成快照。
// 调用方须是唯一的写入者（配置更新持有 OptionMap 写锁时调用）。
func RefreshChatUsageDetailsFill() {
	channels := make(map[int]struct{}, len(generalSetting.ChatUsageDetailsFillChannels))
	for _, channelId := range generalSetting.ChatUsageDetailsFillChannels {
		if channelId > 0 {
			channels[channelId] = struct{}{}
		}
	}
	chatUsageDetailsFillSnapshot.Store(&channels)
}

// ShouldFillChatUsageDetails 返回该渠道是否被点名：上游 chat 用量里省略的零值明细，
// 回给客户端时要写明为 0。
func ShouldFillChatUsageDetails(channelId int) bool {
	channels := chatUsageDetailsFillSnapshot.Load()
	if channels == nil {
		return false
	}
	_, ok := (*channels)[channelId]
	return ok
}

// ValidateChatUsageDetailsFillChannels 校验 chat_usage_details_fill_channels 的待写入值。
// 配置框架对解析失败是静默跳过的，不在写入前拦住，接口会报成功而实际没生效。
func ValidateChatUsageDetailsFillChannels(value string) error {
	var parsed []*int // 用指针区分「没写值（null）」和 0
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
		return fmt.Errorf("chat_usage_details_fill_channels 必须是 [渠道ID, ...] 形式的 JSON 数组: %w", err)
	}
	if parsed == nil {
		return fmt.Errorf("chat_usage_details_fill_channels 必须是 JSON 数组，清空请写 []")
	}
	seen := make(map[int]struct{}, len(parsed))
	for _, channelId := range parsed {
		if channelId == nil {
			return fmt.Errorf("渠道 ID 不能为 null")
		}
		if *channelId <= 0 {
			return fmt.Errorf("渠道 ID %d 不是正整数", *channelId)
		}
		if _, dup := seen[*channelId]; dup {
			return fmt.Errorf("渠道 ID %d 重复", *channelId)
		}
		seen[*channelId] = struct{}{}
	}
	return nil
}
