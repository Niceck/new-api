package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// codexInput 按 Codex CLI 的会话历史拼一段 input：开发者说明、用户消息与截图、加密推理、shell 函数调用
// 与输出、apply_patch 自定义工具、本地 shell、新版 shell 工具输出、带图片的函数输出、压缩条目。
func codexInput(img, secret string) string {
	return `[
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"看看这个页面"},
			{"type":"input_image","image_url":"data:image/png;base64,` + img + `","detail":"high"}]},
		{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"先检查布局"}],
			"content":[{"type":"reasoning_text","text":"推理明文"}],"encrypted_content":"` + secret + `"},
		{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\"]}","call_id":"c1"},
		{"type":"function_call_output","call_id":"c1","output":"README.md"},
		{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: a.go","call_id":"c2"},
		{"type":"custom_tool_call_output","call_id":"c2","output":"Done!"},
		{"type":"local_shell_call","call_id":"c3","status":"completed","action":{"type":"exec","command":["cat","go.mod"]}},
		{"type":"local_shell_call_output","call_id":"c3","output":"module x"},
		{"type":"shell_call","call_id":"c4","action":{"commands":["cat report.txt"]},"status":"completed"},
		{"type":"shell_call_output","call_id":"c4","output":[{"stdout":"报告正文","stderr":"警告","outcome":{"type":"exit","exit_code":0}}]},
		{"type":"function_call_output","call_id":"c5","output":[{"type":"input_text","text":"截图结果"},
			{"type":"input_image","image_url":{"url":"data:image/png;base64,` + img + `"}}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"布局正常"}]},
		{"type":"compaction","encrypted_content":"` + secret + `"},
		{"type":"item_reference","id":"msg_9"},
		{"role":"user","content":"继续"}
	]`
}

var codexWantTexts = []string{"<permissions instructions>", "看看这个页面", "先检查布局", "推理明文", "shell", `{"command":["ls"]}`,
	"README.md", "apply_patch", "*** Begin Patch", "Done!", "cat", "go.mod", "module x", "cat report.txt", "报告正文", "警告",
	"截图结果", "布局正常", "继续"}

func assertCodexInputCounted(t *testing.T, meta *types.TokenCountMeta, img, secret string) {
	t.Helper()
	assert.NotContains(t, meta.CombineText, img[:64], "图片 base64 不能当文字")
	assert.NotContains(t, meta.CombineText, secret[:64], "加密内容不能当文字")
	for _, want := range codexWantTexts {
		assert.Contains(t, meta.CombineText, want)
	}
	require.Len(t, meta.Files, 2)
	for _, f := range meta.Files {
		assert.Equal(t, types.FileTypeImage, f.FileType)
	}
	assert.Equal(t, "high", meta.Files[0].Detail)
}

// 普通 Responses：旧实现只看 message.content，函数调用、输出、推理、shell 条目整条漏算。
func TestResponsesTokenMetaCountsCodexHistory(t *testing.T) {
	img, secret := fakeBase64(300_000), fakeBase64(80_000)
	raw := `{"model":"gpt-6-astra","instructions":"You are Codex.","tools":[{"type":"function","name":"shell"}],"input":` + codexInput(img, secret) + `}`
	var req OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	meta := req.GetTokenCountMeta()

	assertCodexInputCounted(t, meta, img, secret)
	assert.Contains(t, meta.CombineText, "You are Codex.", "instructions 照旧计入")
	assert.Contains(t, meta.CombineText, `"name":"shell"`, "tools 照旧计入")
}

// compact：旧实现把整个 input 原样当文字，图片与加密内容全算进去。
func TestCompactionTokenMetaCountsCodexHistory(t *testing.T) {
	img, secret := fakeBase64(300_000), fakeBase64(80_000)
	raw := `{"model":"gpt-6-astra","instructions":"压缩这段对话","input":` + codexInput(img, secret) + `}`
	var req OpenAIResponsesCompactionRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	meta := req.GetTokenCountMeta()

	assertCodexInputCounted(t, meta, img, secret)
	assert.Contains(t, meta.CombineText, "压缩这段对话")
	assert.Less(t, len(meta.CombineText), 2_000)
}

// 托管工具的历史条目：可读文字计入，截图与生成的图片按媒体计，坐标、ID、不透明状态不计；
// 不认识的条目与 compaction_summary 一律不计，编码不能漏进文字。
func TestResponsesTokenMetaHostedToolItems(t *testing.T) {
	img, secret := fakeBase64(100_000), fakeBase64(40_000)
	raw := `{"model":"gpt-6-astra","input":[
		{"type":"mcp_list_tools","server_label":"docs","tools":[{"name":"lookup","description":"查文档","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]},
		{"type":"mcp_approval_request","id":"ap_1","name":"lookup","arguments":"{\"q\":\"计费\"}","server_label":"docs"},
		{"type":"mcp_approval_response","approval_request_id":"ap_1","approve":false,"reason":"不允许联网"},
		{"type":"mcp_call","name":"lookup","arguments":"{\"q\":\"缓存\"}","output":"缓存说明","server_label":"docs"},
		{"type":"computer_call","call_id":"cc1","action":{"type":"type","text":"hello world","x":10,"y":20}},
		{"type":"computer_call_output","call_id":"cc1","output":{"type":"computer_screenshot","image_url":"data:image/png;base64,` + img + `"}},
		{"type":"image_generation_call","id":"ig1","revised_prompt":"一只橘猫","result":"` + img + `"},
		{"type":"web_search_call","id":"ws1","action":{"type":"search","query":"new-api 计费"}},
		{"type":"file_search_call","id":"fs1","queries":["退款规则"],"results":[{"filename":"faq.md","text":"七天内可退"}]},
		{"type":"shell_call_output","call_id":"s1","output":[{"stdout":"第一段","stderr":""},{"stdout":"第二段","stderr":"错误"}]},
		{"type":"compaction_summary","data":"` + secret + `"},
		{"type":"some_future_item","payload":"` + secret + `"}
	]}`
	var req OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	meta := req.GetTokenCountMeta()

	for _, want := range []string{"lookup", "查文档", `"q"`, `{"q":"计费"}`, "不允许联网", `{"q":"缓存"}`, "缓存说明",
		"hello world", "一只橘猫", "new-api 计费", "退款规则", "faq.md", "七天内可退", "第一段", "第二段", "错误"} {
		assert.Contains(t, meta.CombineText, want)
	}
	assert.NotContains(t, meta.CombineText, img[:64])
	assert.NotContains(t, meta.CombineText, secret[:64])
	assert.NotContains(t, meta.CombineText, "ap_1")
	require.Len(t, meta.Files, 2, "computer 截图与生成的图片各算一张")
	for _, f := range meta.Files {
		assert.Equal(t, types.FileTypeImage, f.FileType)
	}
}

// compact 现在与普通 Responses 一样计入工具定义与输出格式。
func TestCompactionTokenMetaIncludesToolsAndText(t *testing.T) {
	raw := `{"model":"gpt-6-astra","input":"压缩","tools":[{"type":"function","name":"apply_patch"}],"text":{"format":{"type":"text"}}}`
	var req OpenAIResponsesCompactionRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	meta := req.GetTokenCountMeta()
	assert.Contains(t, meta.CombineText, "压缩")
	assert.Contains(t, meta.CombineText, `"name":"apply_patch"`)
	assert.Contains(t, meta.CombineText, `"format"`)
}

// computer_call 的正式协议是有序的 actions 数组，旧版是单个 action；两种都要读，同时出现时只按 actions 计。
func TestResponsesTokenMetaComputerCallActions(t *testing.T) {
	raw := `{"model":"gpt-6-astra","input":[
		{"type":"computer_call","call_id":"c1","actions":[{"type":"type","text":"penguin"},{"type":"keypress","keys":["CTRL","S"]},{"type":"click","x":1,"y":2}]},
		{"type":"computer_call","call_id":"c2","action":{"type":"type","text":"legacy"}},
		{"type":"computer_call","call_id":"c3","action":{"type":"type","text":"dup"},"actions":[{"type":"type","text":"only-actions"}]}
	]}`
	var req OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	meta := req.GetTokenCountMeta()
	for _, want := range []string{"penguin", "CTRL", "S", "legacy", "only-actions"} {
		assert.Contains(t, meta.CombineText, want)
	}
	assert.NotContains(t, meta.CombineText, "dup")
}

func TestResponsesTokenMetaFilesAndPlainInput(t *testing.T) {
	pdf := fakeBase64(50_000)
	raw := `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[
		{"type":"input_file","filename":"a.pdf","file_data":"data:application/pdf;base64,` + pdf + `"},
		{"type":"input_file","file_url":"https://example.com/b.pdf"},
		{"type":"input_file","file_id":"file_1"},
		{"type":"input_image","file_id":"file_2"}
	]}]}`
	var req OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	meta := req.GetTokenCountMeta()
	assert.NotContains(t, meta.CombineText, pdf[:64])
	assert.Equal(t, 2, len(meta.Files), "file_id 引用拿不到内容，不计")

	var plain OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gpt-6-astra","input":"只有一句话"}`), &plain))
	assert.Equal(t, "只有一句话", strings.TrimSpace(plain.GetTokenCountMeta().CombineText))
}
