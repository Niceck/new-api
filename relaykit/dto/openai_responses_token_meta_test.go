package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func parseCompactionRequest(t *testing.T, raw string) *OpenAIResponsesCompactionRequest {
	t.Helper()
	var req OpenAIResponsesCompactionRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	return &req
}

// Codex 压缩请求带着整段历史：截图（data URI）、函数输出、加密的推理内容。
// 旧实现把 input 原样当文本计数，图片 base64 与 encrypted_content 全被当成文字。
func TestCompactionTokenMetaSplitsMediaAndSkipsEncrypted(t *testing.T) {
	img := fakeBase64(300_000)
	secret := fakeBase64(80_000)
	raw := `{"model":"gpt-5.5","instructions":"压缩这段对话","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"看看这个页面"},
			{"type":"input_image","image_url":"data:image/png;base64,` + img + `","detail":"high"}]},
		{"type":"reasoning","summary":[{"type":"summary_text","text":"先检查布局"}],"encrypted_content":"` + secret + `"},
		{"type":"function_call","name":"shell","arguments":"{\"cmd\":\"ls\"}","call_id":"c1"},
		{"type":"function_call_output","call_id":"c1","output":"README.md"},
		{"type":"function_call_output","call_id":"c2","output":[{"type":"input_text","text":"截图结果"},
			{"type":"input_image","image_url":{"url":"data:image/png;base64,` + img + `"}}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"布局正常"}]},
		{"type":"compaction","encrypted_content":"` + secret + `"},
		{"role":"user","content":"继续"}
	]}`
	meta := parseCompactionRequest(t, raw).GetTokenCountMeta()

	assert.NotContains(t, meta.CombineText, img[:64], "图片 base64 不能当文字")
	assert.NotContains(t, meta.CombineText, secret[:64], "加密内容不能当文字")
	assert.Less(t, len(meta.CombineText), 1_000)
	for _, want := range []string{"压缩这段对话", "看看这个页面", "先检查布局", "shell", `{"cmd":"ls"}`, "README.md", "截图结果", "布局正常", "继续"} {
		assert.Contains(t, meta.CombineText, want)
	}
	require.Len(t, meta.Files, 2)
	for _, f := range meta.Files {
		assert.Equal(t, types.FileTypeImage, f.FileType)
	}
	assert.Equal(t, "high", meta.Files[0].Detail)
}

func TestCompactionTokenMetaStringInputAndFiles(t *testing.T) {
	pdf := fakeBase64(50_000)
	raw := `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[
		{"type":"input_file","filename":"a.pdf","file_data":"data:application/pdf;base64,` + pdf + `"},
		{"type":"input_file","file_url":"https://example.com/b.pdf"},
		{"type":"input_file","file_id":"file_1"}
	]}]}`
	meta := parseCompactionRequest(t, raw).GetTokenCountMeta()
	assert.NotContains(t, meta.CombineText, pdf[:64])
	assert.Equal(t, 2, len(meta.Files))

	plain := parseCompactionRequest(t, `{"model":"gpt-5.5","input":"只有一句话"}`).GetTokenCountMeta()
	assert.Equal(t, "只有一句话", plain.CombineText)
	assert.True(t, strings.TrimSpace(plain.CombineText) != "")
}
