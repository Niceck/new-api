package openai

import (
	"bytes"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// chat 用量里的两类明细对象及其主字段。OpenAI 官方响应里它们始终存在；有的上游
// （Go omitempty 式的兼容层）在数值为 0 时把整个对象省掉，客户端就分不清是 0 还是没报。
var chatUsageDetailFields = [...]struct{ object, field string }{
	{"prompt_tokens_details", "cached_tokens"},
	{"completion_tokens_details", "reasoning_tokens"},
}

// 流式响应里绝大多数数据块不带 usage，先用子串判断挡掉，省一次整块 JSON 校验。
var chatUsageKey = []byte(`"usage"`)

// shouldFillChatUsageDetails 只对 chat/completions、以 OpenAI 格式回给客户端、且渠道被
// 点名（general_setting.chat_usage_details_fill_channels）的请求返回 true。
func shouldFillChatUsageDetails(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil &&
		info.RelayMode == relayconstant.RelayModeChatCompletions &&
		info.RelayFormat == types.RelayFormatOpenAI &&
		operation_setting.ShouldFillChatUsageDetails(info.ChannelId)
}

// fillChatUsageDetails 把 chat 格式响应（整段正文或一个流式数据块）里 usage 缺的明细
// 写明为 0，返回新正文与是否有改动；没有可补的、或正文不是预期形态时原样返回入参。
// 入参不会被修改。它不知道真实数值，只是把「省略」还原成显式的 0，所以只该用在确认过
// 「省略即为 0」的上游上（见 shouldFillChatUsageDetails）。
func fillChatUsageDetails(body []byte) ([]byte, bool, error) {
	if !bytes.Contains(body, chatUsageKey) || !gjson.ValidBytes(body) {
		return body, false, nil
	}
	usage := gjson.GetBytes(body, "usage")
	if !usage.IsObject() || !usage.Get("prompt_tokens").Exists() {
		return body, false, nil
	}

	filled, changed := body, false
	for _, detail := range chatUsageDetailFields {
		next, ok, err := fillUsageDetail(filled, usage.Get(detail.object), detail.object, detail.field)
		if err != nil {
			return body, false, fmt.Errorf("补 usage.%s 失败: %w", detail.object, err)
		}
		filled, changed = next, changed || ok
	}
	return filled, changed, nil
}

// fillUsageDetail 处理一类明细：对象缺失或为 null 时整个写上，对象在但缺主字段时只补主字段。
func fillUsageDetail(body []byte, current gjson.Result, object, field string) ([]byte, bool, error) {
	path := "usage." + object
	switch {
	case !current.Exists() || current.Type == gjson.Null:
		next, err := sjson.SetRawBytes(body, path, []byte(`{"`+field+`":0}`))
		return next, err == nil, err
	case current.IsObject() && !current.Get(field).Exists():
		next, err := sjson.SetBytes(body, path+"."+field, 0)
		return next, err == nil, err
	default:
		return body, false, nil
	}
}

// fillChatUsageDetailsForChannel 对被点名渠道的响应做补齐。改写失败时原样转发上游正文
// ——宁可不补，也不能把响应弄坏——并留一行系统日志，免得补齐悄悄失效没人知道。
func fillChatUsageDetailsForChannel(info *relaycommon.RelayInfo, body []byte) ([]byte, bool) {
	if !shouldFillChatUsageDetails(info) {
		return body, false
	}
	filled, changed, err := fillChatUsageDetails(body)
	if err != nil {
		common.SysLog(fmt.Sprintf("渠道 %d 的 chat 用量明细没补上，已原样转发上游正文: %s", info.ChannelId, err.Error()))
		return body, false
	}
	return filled, changed
}

// fillChatUsageDetailsInBody 用于非流式响应的整段正文。
func fillChatUsageDetailsInBody(info *relaycommon.RelayInfo, body []byte) []byte {
	filled, _ := fillChatUsageDetailsForChannel(info, body)
	return filled
}

// fillChatUsageDetailsInChunk 用于流式响应的单个数据块；绝大多数块不带 usage，原样返回。
// 传给改写函数的是 data 的零拷贝只读视图：sjson 不带 ReplaceInPlace 时不会原地写入参。
func fillChatUsageDetailsInChunk(info *relaycommon.RelayInfo, data string) string {
	filled, changed := fillChatUsageDetailsForChannel(info, common.StringToByteSlice(data))
	if !changed {
		return data
	}
	return string(filled)
}
