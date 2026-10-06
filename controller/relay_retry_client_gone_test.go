package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newRetryTestContext(t *testing.T) (*gin.Context, context.CancelFunc) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	return c, cancel
}

// 客户端已经断开时不再发起新的重试：没人在等回复，再发一次只会白耗上游；
// 重试若成功，还要向客户收一笔他收不到的回复的钱。
//
// 这里只拦「下一次尝试」，不取消正在进行的那一次——那一次以错误结束会全额退预扣，
// 而上游未必因为我方断开就不计费，取消等于给客户一个「断开即免单」的窗口。
func TestShouldRetry_StopsOnceClientIsGone(t *testing.T) {
	c, cancel := newRetryTestContext(t)
	retryable := types.NewError(errors.New("upstream request failed"), types.ErrorCodeDoRequestFailed)
	// 渠道类错误原本无条件重试（不看剩余次数），断开后同样要停
	channelErr := types.NewError(errors.New("no available key"), types.ErrorCodeChannelNoAvailableKey)

	require.True(t, shouldRetry(c, retryable, 2), "客户端还在：照常重试")
	require.True(t, shouldRetry(c, channelErr, 0), "客户端还在：渠道类错误照常重试")

	cancel()

	require.False(t, shouldRetry(c, retryable, 2))
	require.False(t, shouldRetry(c, channelErr, 2))
}

// 任务提交走另一条重试判定，口径保持一致。已经提交成功的任务不经过这里，不受影响。
func TestShouldRetryTaskRelay_StopsOnceClientIsGone(t *testing.T) {
	c, cancel := newRetryTestContext(t)
	upstreamDown := &taskdto.TaskError{StatusCode: http.StatusBadGateway}

	require.True(t, shouldRetryTaskRelay(c, 1, upstreamDown, 2), "客户端还在：照常重试")

	cancel()

	require.False(t, shouldRetryTaskRelay(c, 1, upstreamDown, 2))
}
