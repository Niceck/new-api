package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
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

// 回退时只认 Anthropic 的错误类型：上游塞进 type 的任意字符串（域名、URL、key）不能原样给客户。
func TestRelayErrorHandlerRejectsUnknownUpstreamType(t *testing.T) {
	for _, code := range []string{``, `"code":null,`, `"code":"",`} {
		body := `{"error":{` + code + `"type":"https://vendor.example.com/v1?api_key=secret","message":"failed"}}`
		errType, _ := claudeErrorFromUpstream(t, http.StatusBadGateway, body)
		assert.Equal(t, "upstream_error", errType, body)
	}
}

// code 为 JSON null 与没有 code 等价；数字 code 维持原行为。
func TestRelayErrorHandlerNullAndNumericCode(t *testing.T) {
	errType, _ := claudeErrorFromUpstream(t, http.StatusBadRequest,
		`{"error":{"code":null,"type":"overloaded_error","message":"busy"}}`)
	assert.Equal(t, "overloaded_error", errType)
	errType, _ = claudeErrorFromUpstream(t, http.StatusBadRequest,
		`{"error":{"code":0,"type":"invalid_request_error","message":"bad"}}`)
	assert.Equal(t, "0", errType)
}

// OpenAI 格式的出口不受这次改动影响：type 仍是上游的 type。
func TestRelayErrorHandlerOpenAIOutputUnchanged(t *testing.T) {
	newAPIErr := RelayErrorHandler(context.Background(), upstreamErrorResponse(http.StatusBadRequest,
		`{"type":"error","error":{"type":"invalid_request_error","message":"bad thinking.type"}}`), false)
	require.NotNil(t, newAPIErr)
	oaiErr := newAPIErr.ToOpenAIError()
	assert.Equal(t, "invalid_request_error", oaiErr.Type)
	assert.Equal(t, "bad thinking.type", oaiErr.Message)
}

// 另外两个脱敏调用方：保留参数路径、照旧遮上游主机。
func TestOtherMaskCallersKeepFieldPathsAndMaskHosts(t *testing.T) {
	taskErr := TaskErrorWrapper(errors.New("post https://cc-hq.freemodel.dev failed thinking.type"), "bad", http.StatusBadGateway)
	assert.Equal(t, "post https://***.dev failed thinking.type", taskErr.Message)

	newAPIErr := types.WithOpenAIError(types.OpenAIError{Message: "lookup cc-hq.freemodel.dev failed for thinking.type"}, http.StatusBadGateway)
	assert.Equal(t, "lookup ***.***.dev failed for thinking.type", newAPIErr.MaskSensitiveError())
}

// 既没有 code 也没有 type 时退回 upstream_error（WithOpenAIError 补的默认值）。
func TestRelayErrorHandlerFallsBackToUpstreamErrorType(t *testing.T) {
	body := `{"error":{"message":"upstream overloaded"}}`
	errType, _ := claudeErrorFromUpstream(t, http.StatusServiceUnavailable, body)
	assert.Equal(t, "upstream_error", errType)
}
