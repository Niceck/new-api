package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	common2 "github.com/QuantumNous/new-api/common"
	rootconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 模拟一次配置更新（写字段 + 发布快照），用例结束后还原。
func setStreamHeaderPolicy(t *testing.T, byChannel map[string]int, budgetSeconds int) {
	t.Helper()
	setting := operation_setting.GetGeneralSetting()
	savedTimeouts, savedBudget := setting.StreamHeaderTimeoutByChannel, setting.StreamHeaderTotalBudgetSeconds
	setting.StreamHeaderTimeoutByChannel, setting.StreamHeaderTotalBudgetSeconds = byChannel, budgetSeconds
	operation_setting.RefreshStreamHeaderPolicy()
	t.Cleanup(func() {
		setting.StreamHeaderTimeoutByChannel, setting.StreamHeaderTotalBudgetSeconds = savedTimeouts, savedBudget
		operation_setting.RefreshStreamHeaderPolicy()
	})
}

func newUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func newGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	return req
}

func TestDoWithHeaderTimeout_ZeroTimeoutIsPlainDo(t *testing.T) {
	t.Parallel()
	server := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, "ok")
	})

	resp, err := doWithHeaderTimeout(server.Client(), newGet(t, server.URL), 0)

	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
}

func TestDoWithHeaderTimeout_FailsFastWhenHeadersNeverArrive(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	server := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select { // 挂住：既不写响应头也不写正文
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })

	started := time.Now()
	resp, err := doWithHeaderTimeout(server.Client(), newGet(t, server.URL), 100*time.Millisecond)

	require.Nil(t, resp)
	require.ErrorIs(t, err, errResponseHeaderTimeout)
	require.Less(t, time.Since(started), 2*time.Second)
}

// 响应头一到，计时器就必须作废：流式正文可以远远长过这个上限。
func TestDoWithHeaderTimeout_BodyOutlivesTheTimeoutOnceHeadersArrived(t *testing.T) {
	t.Parallel()
	server := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < 4; i++ {
			_, _ = io.WriteString(w, "data: chunk\n\n")
			flusher.Flush()
			time.Sleep(300 * time.Millisecond)
		}
	})

	resp, err := doWithHeaderTimeout(server.Client(), newGet(t, server.URL), 500*time.Millisecond)

	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "data: chunk\n\ndata: chunk\n\ndata: chunk\n\ndata: chunk\n\n", string(body))
}

func TestDoWithHeaderTimeout_OtherFailuresAreNotReportedAsTimeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close() // 连接被拒绝：立刻失败，与等响应头无关

	resp, err := doWithHeaderTimeout(http.DefaultClient, newGet(t, url), 5*time.Second)

	require.Nil(t, resp)
	require.Error(t, err)
	require.NotErrorIs(t, err, errResponseHeaderTimeout)
}

// 调用方自己取消请求（如客户端断开）不是「等响应头超时」，不能被改写成超时错误。
func TestDoWithHeaderTimeout_CallerCancellationIsNotReportedAsTimeout(t *testing.T) {
	t.Parallel()
	server := newUpstream(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	parent, cancelParent := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancelParent)
	t.Cleanup(cancelParent)

	resp, err := doWithHeaderTimeout(server.Client(), newGet(t, server.URL).WithContext(parent), 5*time.Second)

	require.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, errResponseHeaderTimeout)
}

func TestDoWithHeaderTimeout_ClosingTheBodyReleasesTheRequestContext(t *testing.T) {
	t.Parallel()
	server := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	resp, err := doWithHeaderTimeout(server.Client(), newGet(t, server.URL), time.Second)

	require.NoError(t, err)
	ctx := resp.Request.Context()
	require.NoError(t, ctx.Err())
	require.NoError(t, resp.Body.Close())
	require.Error(t, ctx.Err())
}

// HTTP/2 下取消的是挂住的那一条流：同一条连接上并发的另一条流照常完成。
func TestDoWithHeaderTimeout_HTTP2CancelsOnlyTheHungStream(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	remotes := map[string]bool{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		remotes[r.RemoteAddr] = true
		mu.Unlock()
		switch r.URL.Path {
		case "/hang":
			<-r.Context().Done()
		case "/slow": // 挂住的那条流超时被取消时，这条还在途中
			time.Sleep(900 * time.Millisecond)
			_, _ = io.WriteString(w, "slow ok")
		default:
			_, _ = io.WriteString(w, "warm")
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	client := server.Client()

	warm, err := doWithHeaderTimeout(client, newGet(t, server.URL+"/warm"), 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, 2, warm.ProtoMajor)
	_, _ = io.Copy(io.Discard, warm.Body)
	require.NoError(t, warm.Body.Close())

	type result struct {
		body string
		err  error
	}
	slow := make(chan result, 1)
	slowReq := newGet(t, server.URL+"/slow")
	go func() {
		resp, err := doWithHeaderTimeout(client, slowReq, 5*time.Second)
		if err != nil {
			slow <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		slow <- result{body: string(body), err: err}
	}()

	resp, err := doWithHeaderTimeout(client, newGet(t, server.URL+"/hang"), 300*time.Millisecond)
	require.Nil(t, resp)
	require.ErrorIs(t, err, errResponseHeaderTimeout)

	got := <-slow
	require.NoError(t, got.err)
	require.Equal(t, "slow ok", got.body)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, remotes, 1, "三笔请求应共用同一条 HTTP/2 连接")
}

func relayContext(t *testing.T, entry time.Time) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if !entry.IsZero() {
		common2.SetContextKey(ctx, rootconstant.ContextKeyRequestEntryTime, entry)
	}
	return ctx
}

func streamInfo(channelId int, start time.Time) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:    true,
		StartTime:   start,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
	}
}

// 总预算：各次重试合起来等响应头的时间有上限，免得「每次 40 秒 × 3 次」越过前置网关的 100 秒。
func TestStreamHeaderWaitFor_TotalBudgetShrinksLaterAttempts(t *testing.T) {
	setStreamHeaderPolicy(t, map[string]int{"6": 40}, 95)
	attempt := func(elapsed time.Duration) streamHeaderWait {
		return streamHeaderWaitFor(relayContext(t, time.Now().Add(-elapsed)), streamInfo(6, time.Time{}))
	}

	require.Equal(t, streamHeaderWait{timeout: 40 * time.Second}, attempt(0))
	require.Equal(t, streamHeaderWait{timeout: 40 * time.Second}, attempt(40*time.Second))
	third := attempt(80 * time.Second)
	require.False(t, third.exhausted)
	require.InDelta(t, float64(15*time.Second), float64(third.timeout), float64(time.Second))
	// 预算用尽（或剩下不到最短的一段）：不再发这次尝试
	for _, elapsed := range []time.Duration{94500 * time.Millisecond, 95 * time.Second, 300 * time.Second} {
		require.Equal(t, streamHeaderWait{exhausted: true}, attempt(elapsed), elapsed)
	}
}

// 预算从请求进站起算。RelayInfo.StartTime 是鉴权、读完请求体、选完渠道之后才记的，
// 用它起算会让预算的终点晚于前置网关的超时。
func TestStreamHeaderWaitFor_BudgetCountsFromRequestEntryNotRelayStart(t *testing.T) {
	setStreamHeaderPolicy(t, map[string]int{"6": 40}, 95)
	recentStart := streamInfo(6, time.Now().Add(-10*time.Second))

	fromEntry := streamHeaderWaitFor(relayContext(t, time.Now().Add(-80*time.Second)), recentStart)
	require.InDelta(t, float64(15*time.Second), float64(fromEntry.timeout), float64(time.Second))

	// 没有入口时间（不经过入口中间件的调用路径）时退回 StartTime
	oldStart := streamInfo(6, time.Now().Add(-80*time.Second))
	fallback := streamHeaderWaitFor(relayContext(t, time.Time{}), oldStart)
	require.InDelta(t, float64(15*time.Second), float64(fallback.timeout), float64(time.Second))
	noContext := streamHeaderWaitFor(nil, oldStart)
	require.InDelta(t, float64(15*time.Second), float64(noContext.timeout), float64(time.Second))
}

func TestStreamHeaderWaitFor_NoBudgetOrNoStartKeepsPerAttemptTimeout(t *testing.T) {
	setStreamHeaderPolicy(t, map[string]int{"6": 40}, 0)
	late := relayContext(t, time.Now().Add(-10*time.Minute))
	require.Equal(t, streamHeaderWait{timeout: 40 * time.Second}, streamHeaderWaitFor(late, streamInfo(6, time.Time{})))

	setStreamHeaderPolicy(t, map[string]int{"6": 40}, 95)
	require.Equal(t, streamHeaderWait{timeout: 40 * time.Second},
		streamHeaderWaitFor(relayContext(t, time.Time{}), streamInfo(6, time.Time{})))
	// 总预算只约束被点名的渠道：没点名的渠道照旧不限时，也不会被判「预算用尽」
	require.Equal(t, streamHeaderWait{}, streamHeaderWaitFor(late, streamInfo(1, time.Time{})))
}

func TestStreamHeaderWaitFor_OnlyStreamsOnListedChannels(t *testing.T) {
	setStreamHeaderPolicy(t, map[string]int{"6": 40}, 0)
	ctx := relayContext(t, time.Now())
	nonStream := streamInfo(6, time.Time{})
	nonStream.IsStream = false

	require.Equal(t, 40*time.Second, streamHeaderWaitFor(ctx, streamInfo(6, time.Time{})).timeout)
	// 非流式请求要等整段生成完才有响应头，不能套这个上限
	require.Equal(t, streamHeaderWait{}, streamHeaderWaitFor(ctx, nonStream))
	require.Equal(t, streamHeaderWait{}, streamHeaderWaitFor(ctx, streamInfo(1, time.Time{})))
	require.Equal(t, streamHeaderWait{}, streamHeaderWaitFor(ctx, nil))
	require.Equal(t, streamHeaderWait{}, streamHeaderWaitFor(ctx, &relaycommon.RelayInfo{IsStream: true}))
}

// 预算用尽后 doRequest 不再向上游发请求，并把错误标成「不重试」：总预算是硬上限。
func TestDoRequest_ExhaustedBudgetSkipsUpstreamAndStopsRetrying(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	setStreamHeaderPolicy(t, map[string]int{"6": 40}, 95)
	var calls atomic.Int32
	server := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "ok")
	})
	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
	require.NoError(t, err)

	resp, err := doRequest(relayContext(t, time.Now().Add(-96*time.Second)), req, streamInfo(6, time.Time{}))

	require.Nil(t, resp)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Equal(t, types.ErrorCodeDoRequestFailed, apiErr.GetErrorCode())
	require.Equal(t, "upstream error: response header timeout", apiErr.Error())
	require.Zero(t, calls.Load())
}

// doRequest 层面的行为：挂住的上游在被点名渠道的流式请求上要按「请求失败」快速返回
// （错误码与原先的 do_request_failed 相同，这样既有的重试判定原样适用）。
func TestDoRequest_HungUpstreamFailsFastOnlyForListedStreamChannel(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	setStreamHeaderPolicy(t, map[string]int{"6": 1}, 0)

	// 响应头 3 秒后才到：远超渠道 6 的 1 秒上限
	server := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		_, _ = io.WriteString(w, "late")
	})
	send := func(channelId int, stream bool) (*http.Response, error) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
		require.NoError(t, err)
		info := &relaycommon.RelayInfo{
			IsStream:    stream,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelId},
		}
		return doRequest(ctx, req, info)
	}

	started := time.Now()
	resp, err := send(6, true)
	require.Nil(t, resp)
	require.Less(t, time.Since(started), 2500*time.Millisecond)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, types.ErrorCodeDoRequestFailed, apiErr.GetErrorCode())
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	// 对外只给这句提示（原始错误被 ErrOptionWithHideErrMsg 换掉），错误日志里据此区分原因
	require.Equal(t, "upstream error: response header timeout", apiErr.Error())
	require.Equal(t, "status_code=500, upstream error: response header timeout", apiErr.MaskSensitiveErrorWithStatusCode())

	for name, target := range map[string]struct {
		channelId int
		stream    bool
	}{"同渠道的非流式请求": {6, false}, "没被点名的渠道": {1, true}} {
		resp, err := send(target.channelId, target.stream)
		require.NoError(t, err, name)
		body, readErr := io.ReadAll(resp.Body)
		require.NoError(t, readErr, name)
		require.NoError(t, resp.Body.Close(), name)
		require.Equal(t, "late", string(body), name)
	}
}
