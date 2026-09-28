package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formatUserLogs 是面向普通用户的日志投影。它必须是白名单：
// 未知键默认剔除，而不是「只删已知的敏感键」。
//
// 为什么必须白名单：other 的写入点散布在 service/controller 十多处，
// 黑名单下任何新增字段都默认泄露给用户，机制上无法收敛。
// channel_id/channel_name/channel_type 就是这么漏出去的——
// controller/relay.go 把它们写在 other 顶层，而旧实现只 delete
// admin_info / audit_info 两个嵌套键。

func projectUserLog(t *testing.T, other map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(other)
	require.NoError(t, err)

	logs := []*Log{{Id: 1, Other: string(raw), ChannelName: "上游渠道-A"}}
	formatUserLogs(logs, 0)

	assert.Empty(t, logs[0].ChannelName, "结构体上的渠道名必须清空")

	if logs[0].Other == "" {
		return map[string]interface{}{}
	}
	out := map[string]interface{}{}
	require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &out))
	return out
}

// 内部字段必须被剔除。
func TestFormatUserLogs_StripsInternalFields(t *testing.T) {
	out := projectUserLog(t, map[string]interface{}{
		"model_ratio": 2.5,
		// 渠道元数据：controller/relay.go 写在顶层，属于内部拓扑信息
		"channel_id":   5,
		"channel_name": "上游渠道-A",
		"channel_type": 14,
		// 管理员专用嵌套对象
		"admin_info": map[string]interface{}{"use_channel": []string{"5"}},
		"audit_info": map[string]interface{}{"operator": "root"},
		// 仅管理员可见的拒绝原因（前端已做 isAdmin 门控，后端必须同样挡住）
		"reject_reason": "admin blocked this request",
	})

	assert.Equal(t, 2.5, out["model_ratio"], "计费字段必须保留")
	for _, key := range []string{
		"channel_id", "channel_name", "channel_type",
		"admin_info", "audit_info", "reject_reason",
	} {
		assert.NotContains(t, out, key, "内部字段 %s 不得下发给普通用户", key)
	}
}

// 白名单语义：未登记的新键默认剔除，而不是默认放行。
func TestFormatUserLogs_DropsUnknownKeysByDefault(t *testing.T) {
	out := projectUserLog(t, map[string]interface{}{
		"model_ratio":              1.5,
		"upstream_internal_secret": "should-never-reach-user",
		"some_future_debug_field":  map[string]interface{}{"host": "10.0.0.7"},
	})

	assert.Equal(t, 1.5, out["model_ratio"])
	assert.NotContains(t, out, "upstream_internal_secret", "未登记键必须默认剔除")
	assert.NotContains(t, out, "some_future_debug_field", "未登记键必须默认剔除")
}

// 用户可见的计费/请求字段必须全部保留，否则日志页会丢信息。
// 这些键来自 service/log_info_generate.go、cent_rounding.go、text_quota.go、
// task_billing.go 等写入点的穷举。
func TestFormatUserLogs_KeepsUserVisibleBillingFields(t *testing.T) {
	visible := map[string]interface{}{
		// 倍率与价格
		"model_ratio": 2.0, "group_ratio": 1.0, "completion_ratio": 3.0,
		"user_group_ratio": 1.2, "model_price": 0.05,
		// 缓存
		"cache_tokens": 100, "cache_ratio": 0.05,
		"cache_creation_tokens": 49, "cache_creation_ratio": 1.25,
		"cache_creation_tokens_5m": 49, "cache_creation_ratio_5m": 1.25,
		"cache_creation_tokens_1h": 10, "cache_creation_ratio_1h": 2.0,
		"cache_write_tokens": 49,
		// 请求形态
		"frt": 1234.0, "request_path": "/v1/messages", "claude": true,
		"is_model_mapped": true, "upstream_model_name": "claude-opus-5-5",
		"reasoning_effort": "high", "is_system_prompt_overwritten": true,
		"request_conversion": []interface{}{"Claude Messages"},
		"usage_semantic":     "anthropic", "input_tokens_total": 500,
		// 人民币分取整（本站定制）
		"charge_rounding":          map[string]interface{}{"charged_quota": 15000},
		"charge_rounding_previous": map[string]interface{}{"charged_quota": 14000},
		"charged_delta":            1000,
		// 阶梯计费
		"billing_mode": "tiered_expr", "expr_b64": "ZXhwcg==",
		"matched_tier": 2, "request_rules": []interface{}{"rule-a"},
		// 订阅
		"billing_source": "wallet", "billing_preference": "wallet",
		"subscription_id": 7, "subscription_pre_consumed": 100,
		"subscription_post_delta": -20, "subscription_plan_id": 3,
		"subscription_plan_title": "Pro", "subscription_total": 10000,
		"subscription_used": 2000, "subscription_remain": 8000,
		"subscription_consumed": 80, "wallet_quota_deducted": 0,
		// 音频 / 图像 / 工具
		"audio": true, "ws": true,
		"audio_input": 10, "audio_output": 20,
		"text_input": 30, "text_output": 40,
		"audio_ratio": 1.1, "audio_completion_ratio": 1.2,
		"audio_input_price": 0.01, "audio_input_seperate_price": true,
		"audio_input_token_count": 15,
		"image": true, "image_ratio": 1.3, "image_output": 2,
		"tool_surcharges": []interface{}{map[string]interface{}{"name": "web_search"}},
		// 异步任务
		"is_task": true, "task_id": "task-123", "reason": "task completed",
		"pre_consumed_quota": 500, "actual_quota": 480,
		// 流状态与参数覆盖：前端无 admin 门控地展示给日志所有者
		"stream_status": map[string]interface{}{"status": "ok", "end_reason": "stop"},
		"po":            []interface{}{"set temperature = 0.7"},
		// 登录审计（RecordLoginLog，普通用户可见）
		"op":           map[string]interface{}{"action": "login"},
		"login_method": "password", "user_agent": "curl/8.0",
	}

	out := projectUserLog(t, visible)

	for key, want := range visible {
		assert.Contains(t, out, key, "用户可见字段 %s 被误删", key)
		if _, isMap := want.(map[string]interface{}); isMap {
			continue
		}
		if _, isSlice := want.([]interface{}); isSlice {
			continue
		}
		assert.EqualValues(t, want, out[key], "字段 %s 的值被改动", key)
	}
}

// chat_content 是经授权展示的聊天正文，保留（30 天后由清理任务剥键）。
func TestFormatUserLogs_KeepsAuthorizedChatContent(t *testing.T) {
	out := projectUserLog(t, map[string]interface{}{
		"model_ratio":  1.0,
		"chat_content": map[string]interface{}{"request": "hi", "response": "hello"},
	})

	assert.Contains(t, out, "chat_content", "受控聊天正文是本站刻意保留的能力")
}

// other 为空或非法 JSON 时不得 panic，且不得凭空造出内容。
func TestFormatUserLogs_HandlesEmptyAndInvalidOther(t *testing.T) {
	logs := []*Log{
		{Id: 1, Other: ""},
		{Id: 2, Other: "not-json"},
		{Id: 3, Other: "{}"},
	}
	require.NotPanics(t, func() { formatUserLogs(logs, 0) })

	for _, log := range logs {
		if log.Other == "" {
			continue
		}
		out := map[string]interface{}{}
		require.NoError(t, json.Unmarshal([]byte(log.Other), &out), "输出必须是合法 JSON")
		assert.Empty(t, out, "空/非法输入不得产生字段")
	}
}

// 展示用 id 仍按序重排（原有行为，不能被投影改动破坏）。
func TestFormatUserLogs_StillAssignsDisplayIds(t *testing.T) {
	logs := []*Log{
		{Id: 9001, Other: `{"model_ratio":1}`},
		{Id: 9002, Other: `{"model_ratio":2}`},
	}
	formatUserLogs(logs, 10)

	assert.Equal(t, 11, logs[0].Id)
	assert.Equal(t, 12, logs[1].Id)
}

// 管理员查询路径不受影响：GetAllLogs 不走 formatUserLogs，字段保持完整。
// 这里只钉住「投影函数本身不会被误用到管理员路径」的前提——
// 即投影是显式调用的，而非在 Log 的序列化里全局生效。
func TestFormatUserLogs_IsExplicitNotGlobal(t *testing.T) {
	raw := `{"channel_id":5,"admin_info":{"use_channel":["5"]}}`
	log := &Log{Id: 1, Other: raw, ChannelName: "上游渠道-A"}

	// 未调用 formatUserLogs 时，字段必须原样保留（管理员视图依赖此行为）。
	assert.Equal(t, raw, log.Other)
	assert.Equal(t, "上游渠道-A", log.ChannelName)

	out := map[string]interface{}{}
	require.NoError(t, json.Unmarshal([]byte(log.Other), &out))
	assert.Contains(t, out, "channel_id")
	assert.Contains(t, out, "admin_info")
}

func TestFormatUserLogs_PreservesUnrelatedLogColumns(t *testing.T) {
	logs := []*Log{{
		Id:        1,
		UserId:    42,
		ModelName: "claude-opus-5-5",
		TokenName: "Claude Code",
		Quota:     15000,
		Group:     "超级稳定claude",
		Other:     `{"model_ratio":2}`,
	}}
	formatUserLogs(logs, 0)

	assert.Equal(t, 42, logs[0].UserId, "投影只处理 Other 与 ChannelName")
	assert.Equal(t, "claude-opus-5-5", logs[0].ModelName)
	assert.Equal(t, 15000, logs[0].Quota)
}
