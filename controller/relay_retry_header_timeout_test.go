package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 等响应头超时沿用 do_request_failed（500）这类错误。这条用例钉住「它会被重试」：
// 重试判定若改成跳过这类错误，超时就只会让请求更快失败，而不是换一次尝试。
func TestShouldRetry_DoRequestFailedIsRetriedWhileRetriesRemain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	headerTimeout := types.NewError(
		errors.New("upstream response header timeout after 40s"),
		types.ErrorCodeDoRequestFailed,
		types.ErrOptionWithHideErrMsg("upstream error: response header timeout"),
	)

	require.Equal(t, http.StatusInternalServerError, headerTimeout.StatusCode)
	require.True(t, shouldRetry(c, headerTimeout, 2))
	require.True(t, shouldRetry(c, headerTimeout, 1))
	require.False(t, shouldRetry(c, headerTimeout, 0))

	// 总预算用尽时 doRequest 返回的错误带「不重试」标记：即使还有重试次数也到此为止
	budgetExhausted := types.NewError(
		errors.New("upstream response header timeout"),
		types.ErrorCodeDoRequestFailed,
		types.ErrOptionWithHideErrMsg("upstream error: response header timeout"),
		types.ErrOptionWithSkipRetry(),
	)
	require.False(t, shouldRetry(c, budgetExhausted, 2))

	// 令牌点名了渠道时不换渠道重试（既有规则，不因本次改动而变）
	c.Set("specific_channel_id", 6)
	require.False(t, shouldRetry(c, headerTimeout, 2))
}
