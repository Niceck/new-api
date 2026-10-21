package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func upstreamErrorResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func claudeErrorFromUpstream(t *testing.T, status int, body string) (string, string) {
	t.Helper()
	newAPIErr := RelayErrorHandler(context.Background(), upstreamErrorResponse(status, body), false)
	require.NotNil(t, newAPIErr)
	claudeErr := newAPIErr.ToClaudeError()
	return claudeErr.Type, claudeErr.Message
}

// Anthropic 格式的上游报错没有 code 字段：转回 Claude 格式时要保留上游的 type，
// 以前取的是 code，客户看到的是 "type":"<nil>"。参数路径也不该被当成域名遮掉。
func TestRelayErrorHandlerKeepsAnthropicErrorType(t *testing.T) {
	body := `{"type":"error","error":{"type":"invalid_request_error","message":"\"thinking.type.disabled\" is not supported for this model. Use \"thinking.type.adaptive\" and \"output_config.effort\" to control thinking behavior."}}`
	errType, message := claudeErrorFromUpstream(t, http.StatusBadRequest, body)
	assert.Equal(t, "invalid_request_error", errType)
	assert.Equal(t, `"thinking.type.disabled" is not supported for this model. Use "thinking.type.adaptive" and "output_config.effort" to control thinking behavior.`, message)
}

// code 是空字符串与没有 code 等价。
func TestRelayErrorHandlerTreatsEmptyCodeAsMissing(t *testing.T) {
	body := `{"error":{"type":"rate_limit_error","code":"","message":"slow down"}}`
	errType, _ := claudeErrorFromUpstream(t, http.StatusTooManyRequests, body)
	assert.Equal(t, "rate_limit_error", errType)
}

// 上游给了 code 的报错（OpenAI 系）维持原行为：type 取 code。
func TestRelayErrorHandlerKeepsCodeAsClaudeType(t *testing.T) {
	body := `{"error":{"type":"invalid_request_error","code":"model_not_found","message":"The model does not exist"}}`
	errType, _ := claudeErrorFromUpstream(t, http.StatusNotFound, body)
	assert.Equal(t, "model_not_found", errType)
}

// 既没有 code 也没有 type 时退回 upstream_error（WithOpenAIError 补的默认值）。
func TestRelayErrorHandlerFallsBackToUpstreamErrorType(t *testing.T) {
	body := `{"error":{"message":"upstream overloaded"}}`
	errType, _ := claudeErrorFromUpstream(t, http.StatusServiceUnavailable, body)
	assert.Equal(t, "upstream_error", errType)
}
