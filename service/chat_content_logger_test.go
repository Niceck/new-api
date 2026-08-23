package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// withLogging 临时打开开关并在测试结束后复原——全局变量，不复原会污染同包其他用例。
func withLogging(t *testing.T, maxLen int, truncate bool) {
	t.Helper()
	oldEnabled := common.LogChatContentEnabled
	oldMax := common.LogChatContentMaxLength
	oldTrunc := common.LogChatContentTruncate
	common.LogChatContentEnabled = true
	common.LogChatContentMaxLength = maxLen
	common.LogChatContentTruncate = truncate
	t.Cleanup(func() {
		common.LogChatContentEnabled = oldEnabled
		common.LogChatContentMaxLength = oldMax
		common.LogChatContentTruncate = oldTrunc
	})
}

func openAIReq(content string) *dto.GeneralOpenAIRequest {
	req := &dto.GeneralOpenAIRequest{Model: "gpt-4o"}
	msg := dto.Message{Role: "user"}
	msg.SetStringContent(content)
	req.Messages = []dto.Message{msg}
	return req
}

// ---------- 开关语义 ----------

func TestBuildChatContentDisabledReturnsNil(t *testing.T) {
	common.LogChatContentEnabled = false
	info := &relaycommon.RelayInfo{Request: openAIReq("hi")}
	if got := BuildChatContent(info, nil); got != nil {
		t.Fatalf("开关关闭时必须返回 nil，得到 %v", got)
	}
}

func TestBuildChatContentNilInfoReturnsNil(t *testing.T) {
	withLogging(t, 100, true)
	if got := BuildChatContent(nil, nil); got != nil {
		t.Fatalf("info 为 nil 时必须返回 nil，得到 %v", got)
	}
}

func TestBuildChatContentNilRequestReturnsNil(t *testing.T) {
	withLogging(t, 100, true)
	info := &relaycommon.RelayInfo{}
	if got := BuildChatContent(info, nil); got != nil {
		t.Fatalf("无请求时必须返回 nil，得到 %v", got)
	}
}

// ---------- OpenAI 路径 ----------

func TestBuildChatContentOpenAIRequest(t *testing.T) {
	withLogging(t, 1000, true)
	info := &relaycommon.RelayInfo{Request: openAIReq("你好世界")}
	info.ChatLogResponseContent = "回复内容"
	info.ChatLogResponseFinishReason = "stop"

	got := BuildChatContent(info, nil)
	if got == nil {
		t.Fatal("期望非 nil")
	}
	req, ok := got["request"].(map[string]any)
	if !ok {
		t.Fatalf("request 段类型错误: %T", got["request"])
	}
	if req["model"] != "gpt-4o" {
		t.Errorf("model = %v", req["model"])
	}
	msgs, ok := req["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %v", req["messages"])
	}
	m := msgs[0].(map[string]any)
	if m["role"] != "user" || m["content"] != "你好世界" {
		t.Errorf("message = %v", m)
	}
	resp := got["response"].(map[string]any)
	if resp["content"] != "回复内容" || resp["finish_reason"] != "stop" {
		t.Errorf("response = %v", resp)
	}
}

// ---------- Claude 路径（生产主力） ----------

func TestBuildChatContentClaudeRequest(t *testing.T) {
	withLogging(t, 1000, true)
	req := &dto.ClaudeRequest{
		Model:    "claude-haiku-4-5",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "ping"}},
	}
	info := &relaycommon.RelayInfo{Request: req}
	info.ChatLogResponseContent = "pong"

	got := BuildChatContent(info, nil)
	if got == nil {
		t.Fatal("Claude 透传路径必须被记录——生产主力流量走这里")
	}
	r := got["request"].(map[string]any)
	if r["model"] != "claude-haiku-4-5" {
		t.Errorf("model = %v", r["model"])
	}
	msgs := r["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != "ping" {
		t.Errorf("messages = %v", msgs)
	}
}

func TestBuildChatContentClaudeSystemPrompt(t *testing.T) {
	withLogging(t, 1000, true)
	req := &dto.ClaudeRequest{
		Model:    "claude-haiku-4-5",
		System:   "你是助手",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}},
	}
	info := &relaycommon.RelayInfo{Request: req}
	got := BuildChatContent(info, nil)
	r := got["request"].(map[string]any)
	if r["system"] != "你是助手" {
		t.Errorf("system 段应被记录，得到 %v", r["system"])
	}
}

// ---------- 截断（UTF-8 安全） ----------

func TestTruncateDoesNotSplitMultibyteRunes(t *testing.T) {
	withLogging(t, 3, true)
	// 按字节切会把「世」劈成半个 rune，产出乱码；必须按 rune 切。
	out, truncated := truncateText("你好世界啊")
	if !truncated {
		t.Fatal("应标记为已截断")
	}
	if !strings.HasPrefix(out, "你好世") {
		t.Errorf("按 rune 截断失败: %q", out)
	}
	for _, r := range out {
		if r == '�' {
			t.Fatalf("出现替换字符，说明按字节切断了多字节 rune: %q", out)
		}
	}
}

func TestTruncateKeepsShortTextIntact(t *testing.T) {
	withLogging(t, 100, true)
	out, truncated := truncateText("短文本")
	if truncated || out != "短文本" {
		t.Errorf("短文本不该被截断: %q %v", out, truncated)
	}
}

func TestTruncateDisabledKeepsFullText(t *testing.T) {
	withLogging(t, 3, false)
	long := "这是一段很长的文本内容"
	out, truncated := truncateText(long)
	if truncated || out != long {
		t.Errorf("关闭截断时应保留全文: %q %v", out, truncated)
	}
}

func TestTruncateMarksMessageContent(t *testing.T) {
	withLogging(t, 2, true)
	info := &relaycommon.RelayInfo{Request: openAIReq("一二三四五")}
	got := BuildChatContent(info, nil)
	m := got["request"].(map[string]any)["messages"].([]any)[0].(map[string]any)
	if m["content_truncated"] != true {
		t.Errorf("超长消息应带 content_truncated 标记: %v", m)
	}
}

// ---------- 隐私脱敏 ----------

func TestImageContentIsRedacted(t *testing.T) {
	withLogging(t, 1000, true)
	req := &dto.GeneralOpenAIRequest{Model: "gpt-4o"}
	msg := dto.Message{Role: "user"}
	msg.SetMediaContent([]dto.MediaContent{
		{Type: dto.ContentTypeText, Text: "看这张图"},
		{Type: dto.ContentTypeImageURL, ImageUrl: map[string]any{
			"url": "data:image/png;base64,AAAABBBBCCCC"}},
	})
	req.Messages = []dto.Message{msg}
	info := &relaycommon.RelayInfo{Request: req}

	got := BuildChatContent(info, nil)
	flat := flatten(got)
	if strings.Contains(flat, "base64") || strings.Contains(flat, "AAAABBBB") {
		t.Fatalf("图片数据必须脱敏，不能整段落库: %s", flat)
	}
	if !strings.Contains(flat, "[image]") {
		t.Errorf("应保留 [image] 占位: %s", flat)
	}
}

// ---------- usage / metadata ----------

func TestUsageIsRecorded(t *testing.T) {
	withLogging(t, 1000, true)
	info := &relaycommon.RelayInfo{Request: openAIReq("hi")}
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	got := BuildChatContent(info, usage)
	u := got["response"].(map[string]any)["usage"].(map[string]any)
	if u["prompt_tokens"] != 10 || u["total_tokens"] != 15 {
		t.Errorf("usage = %v", u)
	}
}

func TestMetadataIncludesChannelAndStream(t *testing.T) {
	withLogging(t, 1000, true)
	info := &relaycommon.RelayInfo{Request: openAIReq("hi"), IsStream: true}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: 7}
	got := BuildChatContent(info, nil)
	meta := got["metadata"].(map[string]any)
	if meta["channel_id"] != 7 || meta["is_stream"] != true {
		t.Errorf("metadata = %v", meta)
	}
}

// 回归：UpstreamModelName/ChannelId 挂在内嵌 *ChannelMeta 上，
// 该指针在 InitChannelMeta 之前为 nil——裸取会 panic 掉整个计费流程。
func TestBuildChatContentSurvivesNilChannelMeta(t *testing.T) {
	withLogging(t, 1000, true)
	info := &relaycommon.RelayInfo{Request: openAIReq("hi")}
	if info.ChannelMeta != nil {
		t.Fatal("前置条件：ChannelMeta 应为 nil")
	}
	got := BuildChatContent(info, nil)
	if got == nil {
		t.Fatal("ChannelMeta 为 nil 时仍应产出内容")
	}
	if _, ok := got["metadata"].(map[string]any)["channel_id"]; ok {
		t.Error("无 ChannelMeta 时不该出现 channel_id")
	}
}

// ---------- 采集辅助 ----------

func TestSaveResponseInfoNoopWhenDisabled(t *testing.T) {
	common.LogChatContentEnabled = false
	info := &relaycommon.RelayInfo{}
	SaveChatLogResponse(info, "content", "stop", "m")
	if info.ChatLogResponseContent != "" {
		t.Error("开关关闭时不应采集")
	}
}

func TestSaveResponseInfoNilSafe(t *testing.T) {
	withLogging(t, 100, true)
	SaveChatLogResponse(nil, "c", "stop", "m") // 不得 panic
}

func TestSaveResponseInfoStores(t *testing.T) {
	withLogging(t, 100, true)
	info := &relaycommon.RelayInfo{}
	SaveChatLogResponse(info, "c", "stop", "m")
	if info.ChatLogResponseContent != "c" || info.ChatLogResponseModel != "m" {
		t.Errorf("采集失败: %+v", info.ChatLogResponseContent)
	}
}

func flatten(m map[string]any) string {
	s, _ := common.Marshal(m)
	return string(s)
}
