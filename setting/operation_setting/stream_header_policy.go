package operation_setting

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// MaxStreamHeaderTimeoutSeconds 是单次上限与总预算允许的最大值（秒）。
const MaxStreamHeaderTimeoutSeconds = 600

// streamHeaderPolicy 是请求路径读取的只读快照。
//
// 配置框架用反射直接改 generalSetting 的字段，不加锁；请求路径若直接读那两个字段，
// 就和热更新构成数据竞争。所以每次配置更新后把它们整理成一份新快照原子地换上去，
// 请求路径只读快照，快照发布后不再修改。
type streamHeaderPolicy struct {
	byChannel   map[int]time.Duration
	totalBudget time.Duration
}

var streamHeaderPolicySnapshot atomic.Pointer[streamHeaderPolicy]

func usableStreamHeaderSeconds(seconds int) bool {
	return seconds > 0 && seconds <= MaxStreamHeaderTimeoutSeconds
}

// parseChannelIdKey 只认规范写法的正整数（"6"），拒绝 "06"、"+6"、" 6" 这类：
// 它们和规范写法指向同一个渠道，放进同一张表就会互相覆盖。
func parseChannelIdKey(key string) (int, bool) {
	channelId, err := strconv.Atoi(key)
	if err != nil || channelId <= 0 || strconv.Itoa(channelId) != key {
		return 0, false
	}
	return channelId, true
}

// RefreshStreamHeaderPolicy 把 general_setting 里的相关配置发布成快照。
// 调用方须是唯一的写入者（配置更新持有 OptionMap 写锁时调用）。
func RefreshStreamHeaderPolicy() {
	policy := &streamHeaderPolicy{byChannel: make(map[int]time.Duration, len(generalSetting.StreamHeaderTimeoutByChannel))}
	for key, seconds := range generalSetting.StreamHeaderTimeoutByChannel {
		channelId, ok := parseChannelIdKey(key)
		if !ok || !usableStreamHeaderSeconds(seconds) {
			continue
		}
		policy.byChannel[channelId] = time.Duration(seconds) * time.Second
	}
	if usableStreamHeaderSeconds(generalSetting.StreamHeaderTotalBudgetSeconds) {
		policy.totalBudget = time.Duration(generalSetting.StreamHeaderTotalBudgetSeconds) * time.Second
	}
	streamHeaderPolicySnapshot.Store(policy)
}

// StreamHeaderTimeout 返回该渠道流式请求单次尝试等上游响应头的上限；0 表示不限。
func StreamHeaderTimeout(channelId int) time.Duration {
	policy := streamHeaderPolicySnapshot.Load()
	if policy == nil {
		return 0
	}
	return policy.byChannel[channelId]
}

// StreamHeaderTotalBudget 返回一次请求连同各次重试等响应头的总预算；0 表示不设。
func StreamHeaderTotalBudget() time.Duration {
	policy := streamHeaderPolicySnapshot.Load()
	if policy == nil {
		return 0
	}
	return policy.totalBudget
}

// ValidateStreamHeaderTimeouts 校验 stream_header_timeout_by_channel 的待写入值。
// 配置框架对解析失败是静默跳过的，不在写入前拦住，接口会报成功而实际没生效。
func ValidateStreamHeaderTimeouts(value string) error {
	var parsed map[string]*int // 用指针区分「没写值（null）」和 0
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
		return fmt.Errorf("stream_header_timeout_by_channel 必须是 {\"渠道ID\": 秒} 形式的 JSON 对象: %w", err)
	}
	if parsed == nil {
		return fmt.Errorf("stream_header_timeout_by_channel 必须是 JSON 对象，清空请写 {}")
	}
	for key, seconds := range parsed {
		if _, ok := parseChannelIdKey(key); !ok {
			return fmt.Errorf("渠道 ID %q 不是规范写法的正整数", key)
		}
		if seconds == nil {
			return fmt.Errorf("渠道 %s 的秒数不能为 null", key)
		}
		if *seconds < 0 || *seconds > MaxStreamHeaderTimeoutSeconds {
			return fmt.Errorf("渠道 %s 的秒数 %d 不在 0~%d 之间", key, *seconds, MaxStreamHeaderTimeoutSeconds)
		}
	}
	return nil
}

// ValidateStreamHeaderTotalBudget 校验 stream_header_total_budget_seconds 的待写入值。
func ValidateStreamHeaderTotalBudget(value string) error {
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("stream_header_total_budget_seconds 必须是整数秒: %w", err)
	}
	if seconds < 0 || seconds > MaxStreamHeaderTimeoutSeconds {
		return fmt.Errorf("stream_header_total_budget_seconds %d 不在 0~%d 之间", seconds, MaxStreamHeaderTimeoutSeconds)
	}
	return nil
}
