package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func parseChatRequest(t *testing.T, raw string) *GeneralOpenAIRequest {
	t.Helper()
	var req GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
	return &req
}

// assistant 历史里的工具调用（函数名与参数）是模型会再读一遍的输入，旧实现整段漏算。
func TestChatTokenMetaCountsAssistantToolCalls(t *testing.T) {
	raw := `{"model":"gpt-6-astra","messages":[
		{"role":"user","content":"列出目录"},
		{"role":"assistant","content":null,"tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"command\":[\"ls\",\"-la\"]}"}},
			{"id":"call_2","type":"custom","custom":{"name":"apply_patch","input":"*** Begin Patch\n*** End Patch"}}
		]},
		{"role":"tool","tool_call_id":"call_1","content":"README.md"}
	]}`
	meta := parseChatRequest(t, raw).GetTokenCountMeta()

	for _, want := range []string{"列出目录", "shell", `{"command":["ls","-la"]}`, "apply_patch", "*** Begin Patch", "README.md"} {
		assert.Contains(t, meta.CombineText, want)
	}
	// 历史调用只加文字，不改 OpenAI 格式的固定补偿项（消息数、工具定义数、name 数）
	assert.Equal(t, 3, meta.MessagesCount)
	assert.Equal(t, 0, meta.ToolsCount)
	assert.Equal(t, 0, meta.NameCount)
}

// tool_calls 不是数组或解析不了时，不能让整条请求的计数出错，也不能把原始 JSON 整段当文字。
func TestChatTokenMetaIgnoresMalformedToolCalls(t *testing.T) {
	raw := `{"model":"gpt-6-astra","messages":[
		{"role":"assistant","content":"好的","tool_calls":{"not":"an array"}},
		{"role":"assistant","content":"再来","tool_calls":[1,"x",{"type":"function"},{"type":"function","function":{"name":"after_bad","arguments":"{}"}}]}
	]}`
	meta := parseChatRequest(t, raw).GetTokenCountMeta()

	assert.Contains(t, meta.CombineText, "好的")
	assert.Contains(t, meta.CombineText, "再来")
	assert.NotContains(t, meta.CombineText, "not")
	assert.NotContains(t, meta.CombineText, "an array")
	assert.Contains(t, meta.CombineText, "after_bad", "坏项只跳过自己，后面的有效调用照常计")
}
