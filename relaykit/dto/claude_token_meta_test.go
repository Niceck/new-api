package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// fakeBase64 生成一段只含 base64 字符的长串，用来模拟截图数据。
func fakeBase64(n int) string {
	return strings.Repeat("iVBORw0KGgoAAAANSUhEUg", n/22+1)[:n]
}

func parseClaudeRequest(t *testing.T, raw string) *ClaudeRequest {
	t.Helper()
	var req ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	return &req
}

func countFiles(meta *types.TokenCountMeta, fileType types.FileType) int {
	n := 0
	for _, f := range meta.Files {
		if f.FileType == fileType {
			n++
		}
	}
	return n
}

// 2026-10-14 事故形状：Claude Code 读截图时，图片以 base64 嵌在 tool_result 的 content 数组里。
// 旧实现把整个 tool_result.content 序列化成 JSON 当文本计数，9MB 请求被估成 376 万 token。
func TestClaudeTokenMetaToolResultImageNotCountedAsText(t *testing.T) {
	img := fakeBase64(200_000)
	raw := `{"model":"claude-opus-5-5","max_tokens":1024,"stream":true,
		"system":[{"type":"text","text":"You are a coding assistant.","cache_control":{"type":"ephemeral"}}],
		"tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}}],
		"messages":[
		{"role":"user","content":"看看这张截图"},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/shot.png"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[
			{"type":"text","text":"截图如下"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}}
		]}]}
	]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()

	assert.NotContains(t, meta.CombineText, img[:64], "tool_result 里的图片 base64 不能进入文本计数")
	assert.Less(t, len(meta.CombineText), 2_000)
	assert.Contains(t, meta.CombineText, "You are a coding assistant.")
	assert.Contains(t, meta.CombineText, "截图如下", "tool_result 里的文字仍要计数")
	assert.Contains(t, meta.CombineText, "/tmp/shot.png", "tool_use 的 input 仍要计数")
	require.Len(t, meta.Files, 1, "tool_result 里的图片按图片计")
	assert.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	assert.False(t, meta.Files[0].Source.IsURL())
	assert.Equal(t, img, meta.Files[0].Source.GetRawData(), "媒体来源保留原始数据")
	assert.Equal(t, 3, meta.MessagesCount)
}

// 估算不能随编码体积增长：同一请求换成大十倍的截图，文本部分一个字都不变。
func TestClaudeTokenMetaTextIndependentOfImageSize(t *testing.T) {
	build := func(size int) string {
		return `{"model":"claude-opus-5-5","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t",
			"content":[{"type":"text","text":"ok"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + fakeBase64(size) + `"}}]}]}]}`
	}
	small := parseClaudeRequest(t, build(1_000)).GetTokenCountMeta()
	large := parseClaudeRequest(t, build(1_000_000)).GetTokenCountMeta()
	assert.Equal(t, small.CombineText, large.CombineText)
	assert.Len(t, large.Files, 1)
}

func TestClaudeTokenMetaToolResultStringAndTextBlocks(t *testing.T) {
	raw := `{"model":"claude-opus-5-5","messages":[
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"a","content":"命令输出：ok"},
			{"type":"tool_result","tool_use_id":"b","is_error":true,"content":[{"type":"text","text":"第一段"},{"type":"text","text":"第二段"}]},
			{"type":"tool_result","tool_use_id":"c"}
		]}
	]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()

	assert.Contains(t, meta.CombineText, "命令输出：ok")
	assert.Contains(t, meta.CombineText, "第一段")
	assert.Contains(t, meta.CombineText, "第二段")
	assert.Empty(t, meta.Files)
}

// 文档块：base64（PDF）按文件计，不进文本；纯文本来源与 content 来源按文字计；file_id 引用拿不到内容，不计。
func TestClaudeTokenMetaDocumentBlocks(t *testing.T) {
	pdf := fakeBase64(100_000)
	raw := `{"model":"claude-opus-5-5","messages":[
		{"role":"user","content":[
			{"type":"document","title":"季度报告","source":{"type":"base64","media_type":"application/pdf","data":"` + pdf + `"}},
			{"type":"document","source":{"type":"text","media_type":"text/plain","data":"纯文本文档内容"}},
			{"type":"document","source":{"type":"content","content":[{"type":"text","text":"分块文档内容"}]}},
			{"type":"document","source":{"type":"url","url":"https://example.com/a.pdf"}},
			{"type":"document","source":{"type":"file","file_id":"file_123"}},
			{"type":"tool_result","tool_use_id":"c","content":[
				{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + pdf + `"}}
			]}
		]}
	]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()

	assert.NotContains(t, meta.CombineText, pdf[:64])
	assert.NotContains(t, meta.CombineText, "https://example.com/a.pdf", "文档地址不是模型看到的文字")
	assert.Contains(t, meta.CombineText, "季度报告")
	assert.Contains(t, meta.CombineText, "纯文本文档内容")
	assert.Contains(t, meta.CombineText, "分块文档内容")
	assert.Equal(t, 3, countFiles(meta, types.FileTypeFile))
	assert.Equal(t, 0, countFiles(meta, types.FileTypeImage))
}

// search_result 的 source 是字符串：旧实现按强类型解析整条消息失败，同一条里的其它文字也一起漏掉。
func TestClaudeTokenMetaSearchResultKeepsNeighbours(t *testing.T) {
	raw := `{"model":"claude-opus-5-5","messages":[
		{"role":"user","content":[
			{"type":"text","text":"根据检索结果回答"},
			{"type":"search_result","source":"https://docs.example.com/x","title":"文档标题","content":[{"type":"text","text":"检索到的正文"}]},
			{"type":"some_future_block","data":"` + fakeBase64(50_000) + `"}
		]}
	]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()

	assert.Contains(t, meta.CombineText, "根据检索结果回答")
	assert.Contains(t, meta.CombineText, "文档标题")
	assert.Contains(t, meta.CombineText, "检索到的正文")
	assert.Less(t, len(meta.CombineText), 1_000, "不认识的块不计，也不能把它整段序列化")
	assert.Empty(t, meta.Files)
}

// 语义保护：模型真看到的文字照常计数，哪怕长得像 base64。
func TestClaudeTokenMetaBase64LookingTextStillCounted(t *testing.T) {
	blob := fakeBase64(5_000)
	raw := `{"model":"claude-opus-5-5","messages":[
		{"role":"user","content":[{"type":"text","text":"data:image/png;base64,` + blob + `"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"w","name":"Write","input":{"content":"` + blob + `"}}]}
	]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()

	assert.Equal(t, 2, strings.Count(meta.CombineText, blob))
	assert.Empty(t, meta.Files)
}

func TestClaudeTokenMetaSystemBlocksAndURLImage(t *testing.T) {
	raw := `{"model":"claude-opus-5-5","system":[{"type":"text","text":"系统提示"},{"type":"image","source":{"type":"url","url":"https://example.com/s.png"}}],
		"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"u","content":[{"type":"image","source":{"type":"url","url":"https://example.com/t.png"}}]}]}]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()

	assert.Contains(t, meta.CombineText, "系统提示")
	assert.NotContains(t, meta.CombineText, "example.com")
	require.Len(t, meta.Files, 2)
	for _, f := range meta.Files {
		assert.Equal(t, types.FileTypeImage, f.FileType)
		assert.True(t, f.Source.IsURL())
	}
}

// 代码内部构造的强类型内容（格式转换产物）与 JSON 解出来的结果一致。
func TestClaudeTokenMetaTypedContentMatchesJSON(t *testing.T) {
	img := fakeBase64(10_000)
	raw := `{"model":"claude-opus-5-5","messages":[{"role":"user","content":[
		{"type":"text","text":"看图"},
		{"type":"tool_result","tool_use_id":"x","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}}]}
	]}]}`
	fromJSON := parseClaudeRequest(t, raw).GetTokenCountMeta()

	text := "看图"
	typed := &ClaudeRequest{Model: "claude-opus-5-5", Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeMediaMessage{
		{Type: "text", Text: &text},
		{Type: "tool_result", ToolUseId: "x", Content: []ClaudeMediaMessage{
			{Type: "image", Source: &ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: img}},
		}},
	}}}}
	fromTyped := typed.GetTokenCountMeta()

	assert.Equal(t, fromJSON.CombineText, fromTyped.CombineText)
	require.Len(t, fromTyped.Files, 1)
	assert.Equal(t, types.FileTypeImage, fromTyped.Files[0].FileType)
}

// 嵌套深度有上限：构造的深层嵌套不再往下计，也不会无限递归。
func TestClaudeTokenMetaDepthLimit(t *testing.T) {
	inner := `[{"type":"text","text":"最深处"}]`
	for i := 0; i < 10; i++ {
		inner = `[{"type":"tool_result","tool_use_id":"d","content":` + inner + `}]`
	}
	raw := `{"model":"claude-opus-5-5","messages":[{"role":"user","content":` + inner + `}]}`
	meta := parseClaudeRequest(t, raw).GetTokenCountMeta()
	assert.NotContains(t, meta.CombineText, "最深处")
}
