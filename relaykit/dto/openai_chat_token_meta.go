package dto

import (
	"encoding/json"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// appendChatToolCallsForTokenCount 把 assistant 历史里的工具调用计入本地估算：function 计函数名与参数，
// custom 工具计名字与输入。这些是模型下一轮要再读的输入，旧实现整段漏算（预扣与无用量兜底都偏低）。
// tool_calls 不是数组、元素不是对象的，跳过，不整段序列化。
func appendChatToolCallsForTokenCount(raw json.RawMessage, texts []string) []string {
	if len(raw) == 0 {
		return texts
	}
	var calls []any
	if err := kitutil.Unmarshal(raw, &calls); err != nil {
		return texts
	}
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"function", "custom"} {
			detail, ok := call[key].(map[string]any)
			if !ok {
				continue
			}
			texts = appendTokenCountText(texts, detail["name"])
			texts = appendTokenCountText(texts, detail["arguments"])
			texts = appendTokenCountText(texts, detail["input"])
		}
	}
	return texts
}

// appendTokenCountText 字符串原样计；对象、数组（个别客户端把参数直接写成 JSON 对象）序列化后计；其余跳过。
func appendTokenCountText(texts []string, value any) []string {
	switch v := value.(type) {
	case string:
		if v != "" {
			texts = append(texts, v)
		}
	case map[string]any, []any:
		if b, err := kitutil.Marshal(v); err == nil {
			texts = append(texts, string(b))
		}
	}
	return texts
}
