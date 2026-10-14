package dto

import (
	"encoding/json"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Responses 输入（目前供 /v1/responses/compact 使用）的本地计数提取：按条目与内容块类型拆成文本与媒体。
//
// 旧实现把整个 input 原样当文本计数，其中的 data URI 图片、文件 base64、reasoning / compaction 的
// encrypted_content 都被当成文字——与 2026-10-14 Claude 截图按文字计数多扣十倍是同一类问题。
// 文字类内容（消息文字、函数参数与输出、可见的推理摘要）照常计数；不透明的加密内容不计；
// 图片、文件按媒体计。不认识的条目不计，也不整段序列化。

const responsesTokenWalkMaxDepth = 3

type responsesTokenWalker struct {
	texts []string
	files []*types.FileMeta
}

func collectResponsesInputForTokenCount(raw json.RawMessage) ([]string, []*types.FileMeta) {
	w := &responsesTokenWalker{}
	if len(raw) == 0 {
		return w.texts, w.files
	}
	var input any
	if err := kitutil.Unmarshal(raw, &input); err != nil {
		return w.texts, w.files
	}
	w.walkInput(input)
	return w.texts, w.files
}

func (w *responsesTokenWalker) walkInput(input any) {
	switch v := input.(type) {
	case string:
		w.addText(v)
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				w.walkItem(m)
			}
		}
	}
}

func (w *responsesTokenWalker) walkItem(item map[string]any) {
	itemType, _ := item["type"].(string)
	switch itemType {
	case "", "message":
		w.walkContent(item["content"], 0)
	case "function_call":
		w.addText(item["name"])
		w.addText(item["arguments"])
	case "custom_tool_call":
		w.addText(item["name"])
		w.addText(item["input"])
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
		w.walkContent(item["output"], 0)
	case "reasoning":
		w.walkContent(item["summary"], 0)
	}
}

// walkContent 处理 content / output / summary：字符串按文字计，块数组逐块处理。
func (w *responsesTokenWalker) walkContent(content any, depth int) {
	if depth > responsesTokenWalkMaxDepth {
		return
	}
	switch v := content.(type) {
	case string:
		w.addText(v)
	case []any:
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				w.walkPart(m, depth)
			}
		}
	}
}

func (w *responsesTokenWalker) walkPart(part map[string]any, depth int) {
	partType, _ := part["type"].(string)
	switch partType {
	case "input_text", "output_text", "text", "summary_text", "reasoning_text":
		w.addText(part["text"])
	case "refusal":
		w.addText(part["refusal"])
	case "input_image":
		imageURL, _ := part["image_url"].(string)
		if obj, ok := part["image_url"].(map[string]any); ok {
			imageURL, _ = obj["url"].(string)
		}
		if imageURL != "" {
			detail, _ := part["detail"].(string)
			w.files = append(w.files, &types.FileMeta{
				FileType: types.FileTypeImage,
				Source:   types.NewFileSourceFromData(imageURL, ""),
				Detail:   detail,
			})
		}
	case "input_file":
		data, _ := part["file_data"].(string)
		if data == "" {
			data, _ = part["file_url"].(string)
		}
		if data != "" {
			w.files = append(w.files, &types.FileMeta{
				FileType: types.FileTypeFile,
				Source:   types.NewFileSourceFromData(data, ""),
			})
		}
	default:
		// 嵌套的内容数组（个别客户端把输出包一层）按同一规则往下走
		if nested, ok := part["content"]; ok {
			w.walkContent(nested, depth+1)
		}
	}
}

func (w *responsesTokenWalker) addText(value any) {
	if text, ok := value.(string); ok && text != "" {
		w.texts = append(w.texts, text)
	}
}
