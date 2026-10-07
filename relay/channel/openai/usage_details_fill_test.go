package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 以下样本取自 2026-10-07 渠道 2 上游（chat 兼容层）的真实响应，只缩短了 id。
const (
	chatBodyHead    = `{"id":"resp_0cca","object":"chat.completion","created":1791337491,"model":"gpt-6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":`
	chatBodyTail    = `,"service_tier":"default"}`
	usageBare       = `{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27}`
	usageBareFilled = `{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}`
)

func chatBody(usage string) string {
	return chatBodyHead + usage + chatBodyTail
}

func TestFillChatUsageDetails(t *testing.T) {
	cases := []struct {
		name  string
		usage string
		want  string // 空串 = 预期原样返回
	}{
		{
			name:  "两类明细都被省略时都写明为 0",
			usage: usageBare,
			want:  usageBareFilled,
		},
		{
			name:  "上游带了推理明细时原值不动，只补缓存明细",
			usage: `{"prompt_tokens":37,"completion_tokens":21,"total_tokens":58,"completion_tokens_details":{"reasoning_tokens":14}}`,
			want:  `{"prompt_tokens":37,"completion_tokens":21,"total_tokens":58,"completion_tokens_details":{"reasoning_tokens":14},"prompt_tokens_details":{"cached_tokens":0}}`,
		},
		{
			name:  "上游带了缓存明细时原值不动，只补推理明细",
			usage: `{"prompt_tokens":900,"completion_tokens":5,"total_tokens":905,"prompt_tokens_details":{"cached_tokens":768}}`,
			want:  `{"prompt_tokens":900,"completion_tokens":5,"total_tokens":905,"prompt_tokens_details":{"cached_tokens":768},"completion_tokens_details":{"reasoning_tokens":0}}`,
		},
		{
			name:  "明细对象在但缺主字段时只补主字段",
			usage: `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cache_write_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}`,
			want:  `{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14,"prompt_tokens_details":{"cache_write_tokens":4,"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":3}}`,
		},
		{
			name:  "明细写成 null 时换成写明 0 的对象",
			usage: `{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27,"prompt_tokens_details":null,"completion_tokens_details":null}`,
			want:  `{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}`,
		},
		{
			name:  "两类明细齐全时原样返回",
			usage: `{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}`,
		},
		{
			name:  "usage 为 null 时原样返回",
			usage: `null`,
		},
		{
			name:  "不是 chat 形态的 usage 原样返回",
			usage: `{"input_tokens":22,"output_tokens":5,"total_tokens":27}`,
		},
		{
			name:  "空的 usage 对象原样返回",
			usage: `{}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := chatBody(tc.usage)

			got, changed, err := fillChatUsageDetails([]byte(body))

			require.NoError(t, err)
			if tc.want == "" {
				require.False(t, changed)
				require.Equal(t, body, string(got))
				return
			}
			require.True(t, changed)
			require.Equal(t, chatBody(tc.want), string(got))
		})
	}
}

func TestFillChatUsageDetails_LeavesUnexpectedInputAlone(t *testing.T) {
	inputs := []string{
		``,
		`[DONE]`,
		`{"id":"x","choices":[]}`,
		`{"id":"x","usage":"n/a"}`,
		`{"id":"x","usage":{"prompt_tokens":1`, // 截断的 JSON
		`{"choices":[{"usage":{"prompt_tokens":1,"completion_tokens":1}}]}`, // 只有嵌套的 usage
	}
	for _, input := range inputs {
		got, changed, err := fillChatUsageDetails([]byte(input))
		require.NoError(t, err, input)
		require.False(t, changed, input)
		require.Equal(t, input, string(got), input)
	}
}

func TestFillChatUsageDetails_DoesNotMutateInputAndIsIdempotent(t *testing.T) {
	body := chatBody(usageBare)
	input := []byte(body)

	once, changed, err := fillChatUsageDetails(input)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, body, string(input))

	twice, changedAgain, err := fillChatUsageDetails(once)
	require.NoError(t, err)
	require.False(t, changedAgain)
	require.Equal(t, string(once), string(twice))
}

func TestFillChatUsageDetails_KeepsOtherFieldsOfFormattedJSON(t *testing.T) {
	body := "{\n  \"id\": \"x\",\n  \"usage\": {\n    \"prompt_tokens\": 22,\n    \"completion_tokens\": 5,\n    \"total_tokens\": 27\n  },\n  \"service_tier\": \"default\"\n}"

	got, changed, err := fillChatUsageDetails([]byte(body))

	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, gjson.ValidBytes(got))
	parsed := gjson.ParseBytes(got)
	require.Equal(t, int64(22), parsed.Get("usage.prompt_tokens").Int())
	require.Equal(t, int64(27), parsed.Get("usage.total_tokens").Int())
	require.Equal(t, `{"cached_tokens":0}`, parsed.Get("usage.prompt_tokens_details").Raw)
	require.Equal(t, `{"reasoning_tokens":0}`, parsed.Get("usage.completion_tokens_details").Raw)
	require.Equal(t, "default", parsed.Get("service_tier").String())
	require.Equal(t, "x", parsed.Get("id").String())
}

// 只在 usage 里追加键，正文其余部分（转义、非 ASCII、超出 float64 精度的大数、写法特别的数字）一个字节都不动。
func TestFillChatUsageDetails_KeepsEveryOtherByte(t *testing.T) {
	head := `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"好的 \u00e9 😀 \"引号\" \n换行"}}],"big":9007199254740993,"usage":`
	tail := `,"ratio":1.0e3,"usage_note":"usage"}`

	got, changed, err := fillChatUsageDetails([]byte(head + usageBare + tail))

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, head+usageBareFilled+tail, string(got))
}

// 流式路径把字符串的零拷贝视图交给改写函数：改写必须新建缓冲区，不能写穿到原字符串。
func TestFillChatUsageDetailsInChunk_DoesNotWriteThroughZeroCopyView(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	for _, usage := range []string{
		usageBare, // 追加新键
		`{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27,"prompt_tokens_details":null,"completion_tokens_details":null}`, // 原位替换
	} {
		data := strings.Clone(chatBody(usage)) // 堆上的字符串：真被原地写了，内容会变
		saved := strings.Clone(data)

		got := fillChatUsageDetailsInChunk(newChatUsageTestInfo(2, true), data)

		require.Equal(t, saved, data)
		require.Equal(t, chatBody(usageBareFilled), got)
	}
}

// nameChatUsageDetailsFillChannels 临时点名一批渠道，测试结束后还原。
func nameChatUsageDetailsFillChannels(t *testing.T, channels ...int) {
	t.Helper()
	setting := operation_setting.GetGeneralSetting()
	saved := setting.ChatUsageDetailsFillChannels
	setting.ChatUsageDetailsFillChannels = channels
	operation_setting.RefreshChatUsageDetailsFill()
	t.Cleanup(func() {
		setting.ChatUsageDetailsFillChannels = saved
		operation_setting.RefreshChatUsageDetailsFill()
	})
}

func newChatUsageTestInfo(channelId int, isStream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         channelId,
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gpt-6-luna",
		},
		OriginModelName:    "gpt-6-luna",
		IsStream:           isStream,
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
}

func newChatUsageTestContext(t *testing.T, upstreamBody string, contentType string) (*gin.Context, *httptest.ResponseRecorder, *http.Response) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "chat-usage-details-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	return c, recorder, resp
}

func TestShouldFillChatUsageDetails(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)

	require.True(t, shouldFillChatUsageDetails(newChatUsageTestInfo(2, false)))
	require.False(t, shouldFillChatUsageDetails(newChatUsageTestInfo(7, false)), "没被点名的渠道")
	require.False(t, shouldFillChatUsageDetails(nil))
	require.False(t, shouldFillChatUsageDetails(&relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
	}), "没有渠道信息")

	legacyCompletions := newChatUsageTestInfo(2, false)
	legacyCompletions.RelayMode = relayconstant.RelayModeCompletions
	require.False(t, shouldFillChatUsageDetails(legacyCompletions), "只管 chat/completions")

	claudeFormat := newChatUsageTestInfo(2, false)
	claudeFormat.RelayFormat = types.RelayFormatClaude
	require.False(t, shouldFillChatUsageDetails(claudeFormat), "回给客户端的不是 OpenAI 格式")
}

func TestOpenaiHandler_FillsUsageDetailsForNamedChannel(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	c, recorder, resp := newChatUsageTestContext(t, chatBody(usageBare), "application/json")

	usage, apiErr := OpenaiHandler(c, newChatUsageTestInfo(2, false), resp)

	require.Nil(t, apiErr)
	require.Equal(t, chatBody(usageBareFilled), recorder.Body.String())
	// 计费用的用量来自上游原文，不受回给客户端的正文影响
	require.Equal(t, 22, usage.PromptTokens)
	require.Equal(t, 5, usage.CompletionTokens)
	require.Equal(t, 27, usage.TotalTokens)
	require.Zero(t, usage.PromptTokensDetails.CachedTokens)
	require.Zero(t, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestOpenaiHandler_LeavesUnnamedChannelByteIdentical(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	c, recorder, resp := newChatUsageTestContext(t, chatBody(usageBare), "application/json")

	_, apiErr := OpenaiHandler(c, newChatUsageTestInfo(7, false), resp)

	require.Nil(t, apiErr)
	require.Equal(t, chatBody(usageBare), recorder.Body.String())
}

func TestOpenaiHandler_DefaultSettingLeavesBodyByteIdentical(t *testing.T) {
	c, recorder, resp := newChatUsageTestContext(t, chatBody(usageBare), "application/json")

	_, apiErr := OpenaiHandler(c, newChatUsageTestInfo(2, false), resp)

	require.Nil(t, apiErr)
	require.Equal(t, chatBody(usageBare), recorder.Body.String())
}

// 被点名渠道上的旧版 /v1/completions 不在范围内：正文逐字节不变。
func TestOpenaiHandler_LeavesLegacyCompletionsByteIdentical(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	c, recorder, resp := newChatUsageTestContext(t, chatBody(usageBare), "application/json")
	info := newChatUsageTestInfo(2, false)
	info.RelayMode = relayconstant.RelayModeCompletions

	_, apiErr := OpenaiHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.Equal(t, chatBody(usageBare), recorder.Body.String())
}

const (
	chunkRole    = `{"id":"resp_0f90","object":"chat.completion.chunk","created":1791337492,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}],"service_tier":"auto"}`
	chunkContent = `{"id":"resp_0f90","object":"chat.completion.chunk","created":1791337492,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}],"service_tier":"auto"}`
	chunkStop    = `{"id":"resp_0f90","object":"chat.completion.chunk","created":1791337492,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"service_tier":"default"}`
	chunkUsage   = `{"id":"resp_0f90","object":"chat.completion.chunk","created":1791337492,"model":"gpt-6-luna","choices":[],"usage":{"prompt_tokens":21,"completion_tokens":5,"total_tokens":26},"service_tier":"default"}`
	chunkUsageOK = `{"id":"resp_0f90","object":"chat.completion.chunk","created":1791337492,"model":"gpt-6-luna","choices":[],"usage":{"prompt_tokens":21,"completion_tokens":5,"total_tokens":26,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}},"service_tier":"default"}`
)

func sseBody(payloads ...string) string {
	var b strings.Builder
	for _, payload := range payloads {
		b.WriteString("data: " + payload + "\n\n")
	}
	return b.String()
}

// ssePayloads 取出响应里每个 data 事件的内容，按发送顺序返回。
func ssePayloads(body string) []string {
	var payloads []string
	for _, line := range strings.Split(body, "\n") {
		if payload, ok := strings.CutPrefix(line, "data: "); ok {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}

func TestOaiStreamHandler_FillsUsageDetailsOnlyInUsageChunk(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	upstream := sseBody(chunkRole, chunkContent, chunkStop, chunkUsage, "[DONE]")
	c, recorder, resp := newChatUsageTestContext(t, upstream, "text/event-stream")

	usage, apiErr := OaiStreamHandler(c, newChatUsageTestInfo(2, true), resp)

	require.Nil(t, apiErr)
	require.Equal(t,
		[]string{chunkRole, chunkContent, chunkStop, chunkUsageOK, "[DONE]"},
		ssePayloads(recorder.Body.String()))
	require.Equal(t, 21, usage.PromptTokens)
	require.Equal(t, 5, usage.CompletionTokens)
}

func TestOaiStreamHandler_LeavesUnnamedChannelByteIdentical(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	upstream := sseBody(chunkRole, chunkContent, chunkStop, chunkUsage, "[DONE]")
	c, recorder, resp := newChatUsageTestContext(t, upstream, "text/event-stream")

	_, apiErr := OaiStreamHandler(c, newChatUsageTestInfo(7, true), resp)

	require.Nil(t, apiErr)
	require.Equal(t,
		[]string{chunkRole, chunkContent, chunkStop, chunkUsage, "[DONE]"},
		ssePayloads(recorder.Body.String()))
}

// 有的上游把 usage 放在带正文的最后一块里，而不是单独一块。
func TestOaiStreamHandler_FillsUsageCarriedByContentChunk(t *testing.T) {
	nameChatUsageDetailsFillChannels(t, 2)
	last := `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":21,"completion_tokens":5,"total_tokens":26}}`
	lastFilled := `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":21,"completion_tokens":5,"total_tokens":26,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`
	c, recorder, resp := newChatUsageTestContext(t, sseBody(chunkRole, last, "[DONE]"), "text/event-stream")

	_, apiErr := OaiStreamHandler(c, newChatUsageTestInfo(2, true), resp)

	require.Nil(t, apiErr)
	require.Equal(t, []string{chunkRole, lastFilled, "[DONE]"}, ssePayloads(recorder.Body.String()))
}

// 渠道开了「强制格式化」时数据块由结构体重新序列化，两类明细本来就在：这条路径不靠补齐，
// 这里钉住它，免得以后有人给明细字段加 omitempty 时无声回退。
func TestOaiStreamHandler_ForceFormatAlreadyCarriesUsageDetails(t *testing.T) {
	upstream := sseBody(chunkRole, chunkContent, chunkStop, chunkUsage, "[DONE]")
	c, recorder, resp := newChatUsageTestContext(t, upstream, "text/event-stream")
	info := newChatUsageTestInfo(2, true)
	info.ChannelSetting.ForceFormat = true

	_, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	payloads := ssePayloads(recorder.Body.String())
	require.Len(t, payloads, 5)
	usage := gjson.Get(payloads[3], "usage")
	require.Equal(t, int64(21), usage.Get("prompt_tokens").Int())
	require.True(t, usage.Get("prompt_tokens_details.cached_tokens").Exists())
	require.True(t, usage.Get("completion_tokens_details.reasoning_tokens").Exists())
	require.Zero(t, usage.Get("prompt_tokens_details.cached_tokens").Int())
	require.Zero(t, usage.Get("completion_tokens_details.reasoning_tokens").Int())
}
