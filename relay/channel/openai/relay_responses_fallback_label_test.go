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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 流没收到 response.completed（客户端断开或上游中断）：输入按本地估算补、输出按已收到的文字算，
// 要打上本地估算标记——之前没打，日志的 usage_billing_path 记成 upstream，看起来像上游实报。
func TestOaiResponsesStreamFallbackMarkedLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"部分输出\"}\n\n"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "responses-fallback-label-test")
	info := &relaycommon.RelayInfo{
		OriginModelName: "test-model",
		DisablePing:     true,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
	}
	info.SetEstimatePromptTokens(1_000)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 1_000, usage.PromptTokens)
	assert.Greater(t, usage.CompletionTokens, 0)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}
