package dto

import (
	"encoding/json"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Responses 输入（/v1/responses 与 /v1/responses/compact 共用）的本地计数提取：按条目与内容块类型
// 拆成文本与媒体。
//
// 旧实现两头都错：普通 Responses 只看 message.content，函数调用与输出、推理、shell 等条目整条漏算
// （预扣偏低）；compact 把整个 input 原样当文字，图片 data URI、文件 base64、encrypted_content 都算
// 进去（预扣虚高，余额不够时连带后续请求被准入拒绝）。这里按条目类型取模型真正读到的文字；
// 图片、文件按媒体计；加密的推理与压缩内容不计；不认识的条目不计，也不整段序列化。

const responsesTokenWalkMaxDepth = 3

type responsesTokenWalker struct {
	texts []string
	files []*types.FileMeta
}

// collectResponsesInputForTokenCount 解析 input（字符串或条目数组）并提取计数输入。
func collectResponsesInputForTokenCount(raw json.RawMessage) ([]string, []*types.FileMeta) {
	w := &responsesTokenWalker{}
	if len(raw) == 0 {
		return w.texts, w.files
	}
	var input any
	if err := kitutil.Unmarshal(raw, &input); err != nil {
		return w.texts, w.files
	}
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
	return w.texts, w.files
}

func (w *responsesTokenWalker) walkItem(item map[string]any) {
	itemType, _ := item["type"].(string)
	switch itemType {
	case "", "message":
		w.walkContent(item["content"], 0)
	case "reasoning":
		// summary 与 content（开放权重模型的推理明文）计文字；encrypted_content 不计
		w.walkContent(item["summary"], 0)
		w.walkContent(item["content"], 0)
	case "function_call", "custom_tool_call", "mcp_call", "mcp_approval_request":
		w.addText(item["name"])
		w.addText(item["arguments"])
		w.addText(item["input"])
		w.walkContent(item["output"], 0)
		w.addText(item["error"])
	case "mcp_approval_response":
		w.addText(item["reason"])
	case "mcp_list_tools":
		w.walkToolDefinitions(item["tools"])
		w.addText(item["error"])
	case "local_shell_call", "shell_call":
		if action, ok := item["action"].(map[string]any); ok {
			w.addText(action["command"])
			w.addText(action["commands"])
			w.addText(action["working_directory"])
		}
	case "apply_patch_call":
		if operation, ok := item["operation"].(map[string]any); ok {
			w.addText(operation["path"])
			w.addText(operation["diff"])
		}
	case "shell_call_output":
		w.walkShellOutput(item["output"])
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output", "apply_patch_call_output":
		w.walkContent(item["output"], 0)
	case "computer_call":
		// 只取动作里的可读输入（键入的文字、按键），坐标不是正文。正式协议是有序的 actions 数组，
		// 旧版是单个 action；有 actions 就只按它计，免得同一动作算两次。
		actions, hasActions := item["actions"].([]any)
		if !hasActions {
			actions = []any{item["action"]}
		}
		for _, a := range actions {
			if action, ok := a.(map[string]any); ok {
				w.addText(action["text"])
				w.addText(action["keys"])
			}
		}
	case "computer_call_output":
		if output, ok := item["output"].(map[string]any); ok {
			w.addImage(output["image_url"], "")
		}
	case "image_generation_call":
		w.addText(item["revised_prompt"])
		w.addImage(item["result"], "")
	case "web_search_call":
		if action, ok := item["action"].(map[string]any); ok {
			w.addText(action["query"])
			w.addText(action["queries"])
			w.addText(action["pattern"])
			w.addText(action["url"])
		}
	case "file_search_call":
		w.addText(item["queries"])
		if results, ok := item["results"].([]any); ok {
			for _, r := range results {
				if m, ok := r.(map[string]any); ok {
					w.addText(m["filename"])
					w.addText(m["text"])
				}
			}
		}
	case "code_interpreter_call":
		w.addText(item["code"])
		w.walkContent(item["outputs"], 0)
	}
	// 不计：compaction / compaction_summary（不透明的压缩状态）、item_reference（服务端历史，本地无正文）、
	// tool_search / program 等 Codex CLI 不发的条目，以及任何不认识的条目——宁可少算，不把编码当文字。
}

// walkToolDefinitions MCP 工具清单：名字、描述计文字，参数 schema 是模型读到的工具定义，序列化后计。
func (w *responsesTokenWalker) walkToolDefinitions(tools any) {
	list, ok := tools.([]any)
	if !ok {
		return
	}
	for _, t := range list {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		w.addText(tool["name"])
		w.addText(tool["description"])
		if schema, ok := tool["input_schema"].(map[string]any); ok {
			if b, err := kitutil.Marshal(schema); err == nil {
				w.texts = append(w.texts, string(b))
			}
		}
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
	case "logs":
		w.addText(part["logs"])
	case "input_image":
		detail, _ := part["detail"].(string)
		w.addImage(part["image_url"], detail)
	case "image":
		w.addImage(part["url"], "")
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
		if nested, ok := part["content"]; ok {
			w.walkContent(nested, depth+1)
		}
	}
}

// walkShellOutput shell_call_output 的 output 是 [{stdout, stderr, outcome}]，也兼容字符串。
func (w *responsesTokenWalker) walkShellOutput(output any) {
	switch v := output.(type) {
	case string:
		w.addText(v)
	case []any:
		for _, entry := range v {
			if m, ok := entry.(map[string]any); ok {
				w.addText(m["stdout"])
				w.addText(m["stderr"])
			}
		}
	}
}

// addImage 图片地址可能是字符串或 {url}；只有 file_id 引用的拿不到内容，不计。
func (w *responsesTokenWalker) addImage(value any, detail string) {
	imageURL, _ := value.(string)
	if obj, ok := value.(map[string]any); ok {
		imageURL, _ = obj["url"].(string)
	}
	if imageURL == "" {
		return
	}
	w.files = append(w.files, &types.FileMeta{
		FileType: types.FileTypeImage,
		Source:   types.NewFileSourceFromData(imageURL, ""),
		Detail:   detail,
	})
}

// addText 字符串原样计；字符串数组（shell 命令）逐项计；其余跳过。
func (w *responsesTokenWalker) addText(value any) {
	switch v := value.(type) {
	case string:
		if v != "" {
			w.texts = append(w.texts, v)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				w.texts = append(w.texts, s)
			}
		}
	}
}
