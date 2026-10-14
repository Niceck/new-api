package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runResponsesStreamForLabel(t *testing.T, events ...string) (*gin.Context, int, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	var body strings.Builder
	for _, e := range events {
		body.WriteString("data: " + e + "\n\n")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "responses-fallback-label-test")
	info := &relaycommon.RelayInfo{
		OriginModelName: "test-model",
		DisablePing:     true,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
	}
	info.SetEstimatePromptTokens(1_000)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body.String())),
		Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	return c, usage.PromptTokens, usage.CompletionTokens
}

const labelDelta = `{"type":"response.output_text.delta","delta":"部分输出"}`

func labelCompleted(input, output int) string {
	return `{"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":` +
		strconv.Itoa(input) + `,"output_tokens":` + strconv.Itoa(output) + `,"total_tokens":` + strconv.Itoa(input+output) + `}}}`
}

// 用量全是上游给的：不打本地估算标记。
func TestOaiResponsesStreamUpstreamUsageNotMarked(t *testing.T) {
	c, in, out := runResponsesStreamForLabel(t, labelDelta, labelCompleted(12, 3))
	assert.Equal(t, 12, in)
	assert.Equal(t, 3, out)
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// 流没收到 response.completed：输入按本地估算补、输出按已收到的文字算，要打标记——之前没打，
// 日志的 usage_billing_path 记成 upstream，看起来像上游实报。
func TestOaiResponsesStreamFallbackMarkedLocal(t *testing.T) {
	c, in, out := runResponsesStreamForLabel(t, labelDelta)
	assert.Equal(t, 1_000, in)
	assert.Greater(t, out, 0)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// 只有输入是估的（completed 报了输出、输入为 0）。
func TestOaiResponsesStreamInputOnlyFallbackMarked(t *testing.T) {
	c, in, out := runResponsesStreamForLabel(t, labelDelta, labelCompleted(0, 5))
	assert.Equal(t, 1_000, in)
	assert.Equal(t, 5, out)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// 只有输出是估的（completed 报了输入、输出为 0，但收到过文字）。
func TestOaiResponsesStreamOutputOnlyFallbackMarked(t *testing.T) {
	c, in, out := runResponsesStreamForLabel(t, labelDelta, labelCompleted(12, 0))
	assert.Equal(t, 12, in)
	assert.Greater(t, out, 0)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}
