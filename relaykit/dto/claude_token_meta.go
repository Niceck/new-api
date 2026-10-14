package dto

import (
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Claude 请求的本地计数输入提取：按协议块类型把内容拆成「文本」与「媒体」两类。
//
// 图片、PDF 这类以 base64 内嵌的二进制只能按媒体计，绝不能把编码当文字交给计数器：
// 2026-10-14 一个 Claude Code 读截图的长会话（截图嵌在 tool_result 里）被整段序列化计数，
// 9MB 请求估成 376 万 token，上游实报约 33 万；上游没回用量时本站按估算扣费，客户被多扣了十倍。
//
// 规则由块类型决定，不按字段名猜：tool_use.input、text 里的长串照常按文字计（那是模型真看到的
// 文字）；只有 image / document 的 source 按媒体计。直接读 JSON 解出来的 map，不经强类型 DTO
// 转换——search_result 的 source 是字符串，强类型解析会连同整条消息一起失败、把相邻文字也漏掉。

// claudeTokenWalkMaxDepth 限制嵌套层数。合法请求最深是 tool_result → document(content) → 块，
// 更深的只可能是构造的，不再往下计。
const claudeTokenWalkMaxDepth = 4

type claudeTokenWalker struct {
	texts []string
	files []*types.FileMeta
}

// walkContent 处理一个 content 字段：字符串按文字计，块数组逐块处理。
func (w *claudeTokenWalker) walkContent(content any, depth int) {
	if depth > claudeTokenWalkMaxDepth {
		return
	}
	if text, ok := content.(string); ok {
		w.addText(text)
		return
	}
	for _, block := range claudeContentBlocks(content) {
		w.walkBlock(block, depth)
	}
}

func (w *claudeTokenWalker) walkBlock(block map[string]any, depth int) {
	blockType, _ := block["type"].(string)
	switch blockType {
	case "text":
		w.addText(block["text"])
	case "image":
		w.addMedia(types.FileTypeImage, block["source"])
	case "document":
		w.walkDocument(block, depth)
	case "search_result":
		w.addText(block["title"])
		w.addText(block["source"])
		w.walkContent(block["content"], depth+1)
	case "tool_use":
		w.addText(block["name"])
		if input := block["input"]; input != nil {
			b, _ := kitutil.Marshal(input)
			w.texts = append(w.texts, string(b))
		}
	case "tool_result":
		w.walkContent(block["content"], depth+1)
	}
}

// walkDocument 文档块：纯文本来源按文字计，content 来源逐块处理，base64 / URL（PDF 等）按文件计。
func (w *claudeTokenWalker) walkDocument(block map[string]any, depth int) {
	w.addText(block["title"])
	w.addText(block["context"])
	source, _ := block["source"].(map[string]any)
	if source == nil {
		return
	}
	switch sourceType, _ := source["type"].(string); sourceType {
	case "text":
		w.addText(source["data"])
	case "content":
		w.walkContent(source["content"], depth+1)
	default:
		w.addMedia(types.FileTypeFile, source)
	}
}

func (w *claudeTokenWalker) addText(value any) {
	if text, ok := value.(string); ok && text != "" {
		w.texts = append(w.texts, text)
	}
}

// addMedia 与 ClaudeMediaMessage.ToFileSource 同一口径：优先 url，其次 data。
// 只有 file_id 引用（Files API）的拿不到内容，与旧实现一样不计。
func (w *claudeTokenWalker) addMedia(fileType types.FileType, rawSource any) {
	source, _ := rawSource.(map[string]any)
	if source == nil {
		return
	}
	data, _ := source["url"].(string)
	if data == "" {
		data = kitutil.Interface2String(source["data"])
	}
	if data == "" {
		return
	}
	mediaType, _ := source["media_type"].(string)
	w.files = append(w.files, &types.FileMeta{
		FileType: fileType,
		Source:   types.NewFileSourceFromData(data, mediaType),
	})
}

// claudeContentBlocks 把 content 统一成块的 map 列表。JSON 解出来的请求是 []any；
// 代码内部构造的强类型切片（如格式转换产物）先转成通用 map，再走同一套规则。
func claudeContentBlocks(content any) []map[string]any {
	switch v := content.(type) {
	case nil:
		return nil
	case []any:
		blocks := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				blocks = append(blocks, block)
				continue
			}
			// 混在 []any 里的强类型块（代码拼出来的内容）逐个转成 map；数字、字符串这类不是块的元素跳过
			if block, err := kitutil.Any2Type[map[string]any](item); err == nil && block != nil {
				blocks = append(blocks, block)
			}
		}
		return blocks
	case []map[string]any:
		return v
	}
	blocks, err := kitutil.Any2Type[[]map[string]any](content)
	if err != nil {
		return nil
	}
	return blocks
}
