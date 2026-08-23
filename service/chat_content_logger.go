package service

// 聊天正文采集（隐私敏感）。
//
// 设计取舍（2026-08-23）：
//  1. 落点是 logs.other 的 chat_content 键，**不覆盖 logs.content**——
//     content 里是计费明细（"模型 X, Audio Input 花费 Y"），日志页与
//     reconcile/runtime_health 都依赖它，覆盖即破坏现有展示与对账口径。
//  2. 构建入口收敛在 PostTextConsumeQuota 一处，读 info.Request 拿原始请求，
//     因此 OpenAI 格式与 Claude 透传（生产主力，channel 1/2）同时生效，
//     不需要在每个 handler 里分别接线。
//  3. 截断按 rune 而非字节：按字节切会把多字节汉字劈成半个 rune，
//     产出替换字符乱码。
//  4. 图片/音频/文件一律脱敏为占位符——base64 正文进库会让 logs 表爆炸，
//     且那是最敏感的用户数据。
//
// 保留期由 newapi-ops 的 purge_chat_content.py 按 30 天清理（剥键不删行）。

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ChatContentKey 是 logs.other 里存放正文的键名。
// purge_chat_content.py 按这个字面量匹配，改名必须同步改清理脚本，
// 否则历史正文永远不会被清理（静默失效，不会报错）。
const ChatContentKey = "chat_content"

// BuildChatContent 构建正文快照；开关关闭或无请求时返回 nil。
// 返回 map 而非 JSON 串：调用方要把它塞进 other map，再由既有逻辑统一序列化。
func BuildChatContent(info *relaycommon.RelayInfo, usage *dto.Usage) map[string]any {
	if !common.LogChatContentEnabled {
		return nil
	}
	if info == nil || info.Request == nil {
		return nil
	}
	request := buildRequestSnapshot(info.Request)
	if request == nil {
		return nil
	}
	return map[string]any{
		"request":  request,
		"response": buildResponseSnapshot(info, usage),
		"metadata": buildMetadataSnapshot(info),
	}
}

// SaveChatLogResponse 暂存响应侧采集结果，供 PostTextConsumeQuota 取用。
//
// finishReason 传空串时**不覆盖**已记录的值：Claude 流式路径的 stop_reason
// 在中途的 message_delta 事件里就到手了（由 SaveChatLogFinishReason 记下），
// 而本函数在流结束后才调用，此时手上没有 reason——无条件写入会把它抹成空。
func SaveChatLogResponse(info *relaycommon.RelayInfo, content, finishReason, model string) {
	if !common.LogChatContentEnabled || info == nil {
		return
	}
	info.ChatLogResponseContent = content
	if finishReason != "" {
		info.ChatLogResponseFinishReason = finishReason
	}
	info.ChatLogResponseModel = model
}

// SaveChatLogFinishReason 单独记录 stop_reason，供解析期即时捕获。
func SaveChatLogFinishReason(info *relaycommon.RelayInfo, finishReason string) {
	if !common.LogChatContentEnabled || info == nil || finishReason == "" {
		return
	}
	info.ChatLogResponseFinishReason = finishReason
}

// truncateText 按 rune 截断，返回 (文本, 是否截断)。
func truncateText(text string) (string, bool) {
	if !common.LogChatContentTruncate {
		return text, false
	}
	maxLen := common.GetLogChatContentMaxLength()
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text, false
	}
	return string(runes[:maxLen]) + "... [truncated]", true
}

func buildRequestSnapshot(req dto.Request) map[string]any {
	switch r := req.(type) {
	case *dto.GeneralOpenAIRequest:
		return openAIRequestSnapshot(r)
	case *dto.ClaudeRequest:
		return claudeRequestSnapshot(r)
	default:
		// 未知请求类型（rerank/audio/embedding 等）：只记模型名，
		// 不猜结构。猜错会把非对话数据当对话记录，污染审计口径。
		return nil
	}
}

func openAIRequestSnapshot(req *dto.GeneralOpenAIRequest) map[string]any {
	if req == nil {
		return nil
	}
	out := map[string]any{"model": req.Model}
	if len(req.Messages) > 0 {
		messages := make([]any, 0, len(req.Messages))
		for i := range req.Messages {
			messages = append(messages, openAIMessageSnapshot(&req.Messages[i]))
		}
		out["messages"] = messages
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.MaxTokens != 0 {
		out["max_tokens"] = req.MaxTokens
	}
	if req.Stream {
		out["stream"] = true
	}
	if names := openAIToolNames(req); len(names) > 0 {
		out["tools"] = names
	}
	return out
}

func openAIToolNames(req *dto.GeneralOpenAIRequest) []string {
	if len(req.Tools) == 0 {
		return nil
	}
	// 只记工具名，不记 schema：schema 是静态定义，逐条落库纯属放大存储。
	names := make([]string, 0, len(req.Tools))
	for _, tool := range req.Tools {
		if tool.Function.Name != "" {
			names = append(names, tool.Function.Name)
		}
	}
	return names
}

func openAIMessageSnapshot(msg *dto.Message) map[string]any {
	out := map[string]any{"role": msg.Role}
	if msg.IsStringContent() {
		text, truncated := truncateText(msg.StringContent())
		out["content"] = text
		if truncated {
			out["content_truncated"] = true
		}
	} else {
		out["content"] = mediaContentSnapshot(msg.ParseContent())
	}
	if calls := msg.ParseToolCalls(); len(calls) > 0 {
		summary := make([]map[string]any, 0, len(calls))
		for _, tc := range calls {
			summary = append(summary, map[string]any{
				"id": tc.ID, "name": tc.Function.Name,
			})
		}
		out["tool_calls"] = summary
	}
	return out
}

func mediaContentSnapshot(contents []dto.MediaContent) []any {
	out := make([]any, 0, len(contents))
	for _, c := range contents {
		item := map[string]any{"type": c.Type}
		switch c.Type {
		case dto.ContentTypeText:
			text, truncated := truncateText(c.Text)
			item["text"] = text
			if truncated {
				item["truncated"] = true
			}
		case dto.ContentTypeImageURL:
			// 绝不记原始 URL/base64：既是最敏感数据，也会把 logs 表撑爆。
			item["image_url"] = "[image]"
		default:
			item["data"] = "[binary]"
		}
		out = append(out, item)
	}
	return out
}

func claudeRequestSnapshot(req *dto.ClaudeRequest) map[string]any {
	if req == nil {
		return nil
	}
	out := map[string]any{"model": req.Model}
	if req.System != nil {
		if s, ok := req.System.(string); ok && s != "" {
			text, truncated := truncateText(s)
			out["system"] = text
			if truncated {
				out["system_truncated"] = true
			}
		} else {
			out["system"] = "[structured]"
		}
	}
	if len(req.Messages) > 0 {
		messages := make([]any, 0, len(req.Messages))
		for i := range req.Messages {
			messages = append(messages, claudeMessageSnapshot(&req.Messages[i]))
		}
		out["messages"] = messages
	}
	if req.MaxTokens != nil {
		out["max_tokens"] = *req.MaxTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.Stream != nil && *req.Stream {
		out["stream"] = true
	}
	return out
}

func claudeMessageSnapshot(msg *dto.ClaudeMessage) map[string]any {
	out := map[string]any{"role": msg.Role}
	if msg.IsStringContent() {
		text, truncated := truncateText(msg.Content.(string))
		out["content"] = text
		if truncated {
			out["content_truncated"] = true
		}
		return out
	}
	// 结构化 content：Claude 侧是 any，逐块按 type 脱敏，
	// 不整份 marshal——整份进去会把 image/document 的 base64 一起带上。
	blocks, ok := msg.Content.([]any)
	if !ok {
		out["content"] = "[structured]"
		return out
	}
	items := make([]any, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			items = append(items, map[string]any{"type": "[unknown]"})
			continue
		}
		blockType, _ := block["type"].(string)
		item := map[string]any{"type": blockType}
		switch blockType {
		case "text":
			if s, ok := block["text"].(string); ok {
				text, truncated := truncateText(s)
				item["text"] = text
				if truncated {
					item["truncated"] = true
				}
			}
		case "image", "document":
			item["data"] = "[" + blockType + "]"
		case "tool_use":
			item["name"] = block["name"]
		case "tool_result":
			item["data"] = "[tool_result]"
		default:
			item["data"] = "[omitted]"
		}
		items = append(items, item)
	}
	out["content"] = items
	return out
}

func buildResponseSnapshot(info *relaycommon.RelayInfo, usage *dto.Usage) map[string]any {
	out := map[string]any{}
	if info.ChatLogResponseContent != "" {
		text, truncated := truncateText(info.ChatLogResponseContent)
		out["content"] = text
		if truncated {
			out["content_truncated"] = true
		}
	}
	if info.ChatLogResponseFinishReason != "" {
		out["finish_reason"] = info.ChatLogResponseFinishReason
	}
	if info.ChatLogResponseModel != "" {
		out["model"] = info.ChatLogResponseModel
	} else if info.UpstreamModelName != "" {
		out["model"] = info.UpstreamModelName
	}
	if usage != nil {
		out["usage"] = map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}
	return out
}

func buildMetadataSnapshot(info *relaycommon.RelayInfo) map[string]any {
	out := map[string]any{}
	if !info.StartTime.IsZero() {
		out["request_time"] = info.StartTime.Format(time.RFC3339)
		if elapsed := time.Since(info.StartTime).Milliseconds(); elapsed > 0 {
			out["total_time_ms"] = elapsed
		}
		if !info.FirstResponseTime.IsZero() {
			if ttft := info.FirstResponseTime.Sub(info.StartTime).Milliseconds(); ttft > 0 {
				out["first_token_ms"] = ttft
			}
		}
	}
	if info.IsStream {
		out["is_stream"] = true
	}
	if info.ChannelId > 0 {
		out["channel_id"] = info.ChannelId
	}
	return out
}
