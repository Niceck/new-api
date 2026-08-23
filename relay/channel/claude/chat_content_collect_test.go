package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func strPtr(s string) *string { return &s }

func enableChatLog(t *testing.T) {
	t.Helper()
	old := common.LogChatContentEnabled
	common.LogChatContentEnabled = true
	t.Cleanup(func() { common.LogChatContentEnabled = old })
}

// 回归（2026-08-23 生产实测）：claudeInfo.ResponseText 只由流式 delta 事件
// 累积，非流式的 ClaudeHandler 路径永远走不到那里 —— 上线首次验证时
// response.content 落库为 NULL。非流式必须从 content 块自行采集。
func TestCollectClaudeChatLogTextFromContentBlocks(t *testing.T) {
	enableChatLog(t)
	info := &relaycommon.RelayInfo{}
	resp := &dto.ClaudeResponse{
		Content: []dto.ClaudeMediaMessage{
			{Type: "text", Text: strPtr("你好")},
			{Type: "text", Text: strPtr("世界")},
		},
	}
	collectClaudeChatLogText(info, resp)
	if info.ChatLogResponseContent != "你好世界" {
		t.Errorf("正文采集失败: %q", info.ChatLogResponseContent)
	}
}

func TestCollectClaudeChatLogTextIncludesThinking(t *testing.T) {
	enableChatLog(t)
	info := &relaycommon.RelayInfo{}
	resp := &dto.ClaudeResponse{
		Content: []dto.ClaudeMediaMessage{
			{Type: "thinking", Thinking: strPtr("推理段")},
			{Type: "text", Text: strPtr("答案")},
		},
	}
	collectClaudeChatLogText(info, resp)
	if info.ChatLogResponseContent != "推理段答案" {
		t.Errorf("thinking 段应计入: %q", info.ChatLogResponseContent)
	}
}

func TestCollectClaudeChatLogTextNoopWhenDisabled(t *testing.T) {
	common.LogChatContentEnabled = false
	info := &relaycommon.RelayInfo{}
	collectClaudeChatLogText(info, &dto.ClaudeResponse{
		Content: []dto.ClaudeMediaMessage{{Type: "text", Text: strPtr("x")}},
	})
	if info.ChatLogResponseContent != "" {
		t.Error("开关关闭时不应采集")
	}
}

func TestCollectClaudeChatLogTextNilSafe(t *testing.T) {
	enableChatLog(t)
	collectClaudeChatLogText(nil, &dto.ClaudeResponse{})
	collectClaudeChatLogText(&relaycommon.RelayInfo{}, nil)
}

// 空 content 不得清空已采集的值：tool_use-only 响应没有 text 块，
// 无条件写入会把流式路径先前采集到的正文抹成空串。
func TestCollectClaudeChatLogTextKeepsExistingWhenEmpty(t *testing.T) {
	enableChatLog(t)
	info := &relaycommon.RelayInfo{}
	info.ChatLogResponseContent = "已有内容"
	collectClaudeChatLogText(info, &dto.ClaudeResponse{
		Content: []dto.ClaudeMediaMessage{{Type: "tool_use"}},
	})
	if info.ChatLogResponseContent != "已有内容" {
		t.Errorf("不该被空 content 覆盖: %q", info.ChatLogResponseContent)
	}
}
