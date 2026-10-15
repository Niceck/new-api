package helper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 客户端在上游回响应头之前就断开（2026-10-15 用户 487 的 10 笔即此）：进入扫描器时请求上下文已取消。
func setupClientGoneStreamTest(t *testing.T) (*gin.Context, *http.Response, *relaycommon.RelayInfo, *io.PipeWriter) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)

	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	return c, &http.Response{Body: pr}, info, pw
}

func runScannerAsync(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, opts StreamScannerOptions, handler func(string, *StreamResult)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		StreamScannerHandlerWithOptions(c, resp, info, opts, handler)
		close(done)
	}()
	return done
}

func waitDone(t *testing.T, done <-chan struct{}, within time.Duration, msg string) time.Duration {
	t.Helper()
	start := time.Now()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatal(msg)
	}
	return time.Since(start)
}

// 断开后上游才把带用量的首个事件送到：在宽限期内读到它、交给处理函数，然后照常关闭上游。
func TestStreamScannerHandler_ClientGoneWaitsForUsageWithinGrace(t *testing.T) {
	c, resp, info, pw := setupClientGoneStreamTest(t)

	usageSeen := make(chan struct{})
	var once sync.Once
	var handled []string
	var mu sync.Mutex
	done := runScannerAsync(c, resp, info, StreamScannerOptions{
		UsageSeen:            usageSeen,
		ClientGoneUsageGrace: 3 * time.Second,
	}, func(data string, sr *StreamResult) {
		mu.Lock()
		handled = append(handled, data)
		mu.Unlock()
		if data == "usage" {
			once.Do(func() { close(usageSeen) })
		}
	})

	time.Sleep(100 * time.Millisecond)
	_, err := fmt.Fprint(pw, "data: usage\n")
	require.NoError(t, err)

	waitDone(t, done, 2*time.Second, "处理函数拿到用量后应当立即结束，不等满宽限期")

	mu.Lock()
	assert.Equal(t, []string{"usage"}, handled)
	mu.Unlock()
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason, "结束原因仍记客户端断开")

	_, err = fmt.Fprint(pw, "data: more\n")
	require.ErrorIs(t, err, io.ErrClosedPipe, "拿到用量后要关闭上游，别继续为没人听的请求生成")
}

// 上游迟迟不发事件：最多等宽限期，然后照旧结束（之后按本地估算计费）。
func TestStreamScannerHandler_ClientGoneUsageGraceIsBounded(t *testing.T) {
	c, resp, info, pw := setupClientGoneStreamTest(t)

	var count atomic.Int64
	done := runScannerAsync(c, resp, info, StreamScannerOptions{
		UsageSeen:            make(chan struct{}),
		ClientGoneUsageGrace: 200 * time.Millisecond,
	}, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	elapsed := waitDone(t, done, 2*time.Second, "宽限期到了必须返回")
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond, "应当等过宽限期")
	assert.Equal(t, int64(0), count.Load())
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)

	_, err := fmt.Fprint(pw, "data: late\n")
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

// 宽限期内上游自己结束了（EOF）：不必等满。
func TestStreamScannerHandler_ClientGoneGraceEndsOnUpstreamEOF(t *testing.T) {
	c, resp, info, pw := setupClientGoneStreamTest(t)

	done := runScannerAsync(c, resp, info, StreamScannerOptions{
		UsageSeen:            make(chan struct{}),
		ClientGoneUsageGrace: 5 * time.Second,
	}, func(data string, sr *StreamResult) {})

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, pw.Close())

	waitDone(t, done, 2*time.Second, "上游关闭后应当立即结束")
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason, "先记下的断开原因不被 EOF 覆盖")
}

// 断开前已经拿到用量（流到一半客户端走了）：和原来一样立即结束。
func TestStreamScannerHandler_ClientGoneUsageAlreadySeenReturnsImmediately(t *testing.T) {
	c, resp, info, _ := setupClientGoneStreamTest(t)

	usageSeen := make(chan struct{})
	close(usageSeen)
	done := runScannerAsync(c, resp, info, StreamScannerOptions{
		UsageSeen:            usageSeen,
		ClientGoneUsageGrace: 5 * time.Second,
	}, func(data string, sr *StreamResult) {})

	elapsed := waitDone(t, done, 2*time.Second, "已拿到用量就不该再等")
	assert.Less(t, elapsed, time.Second)
}

// 没登记「等用量」的调用方（OpenAI、Gemini 等）：行为不变，断开即关上游，一个事件都不读。
func TestStreamScannerHandler_ClientGoneWithoutOptionsAbortsImmediately(t *testing.T) {
	c, resp, info, pw := setupClientGoneStreamTest(t)

	var count atomic.Int64
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			count.Add(1)
		})
		close(done)
	}()

	elapsed := waitDone(t, done, 2*time.Second, "没有宽限期时应当立即返回")
	assert.Less(t, elapsed, time.Second)

	_, err := fmt.Fprint(pw, "data: x\n")
	require.ErrorIs(t, err, io.ErrClosedPipe)
	assert.Equal(t, int64(0), count.Load())
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}
