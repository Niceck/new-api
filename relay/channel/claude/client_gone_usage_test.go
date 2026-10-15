package claude

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-15 用户 487：客户端约 58 秒放弃，上游 57–284 秒后才回响应头。原来一进扫描器就关上游，
// message_start 里的真实用量（几乎全是缓存读）没读到，按本地估算全价扣了 ¥2.56/笔（正常一轮约 ¥0.2）。
const clientGoneEstimate = 364_604

func init() {
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func runClientGoneClaudeStream(t *testing.T, upstream func(w *io.PipeWriter)) (*dto.Usage, *relaycommon.RelayInfo, time.Duration) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(reqCtx)

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	go upstream(pw)

	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
	}
	info.SetEstimatePromptTokens(clientGoneEstimate)

	start := time.Now()
	usage, apiErr := ClaudeStreamHandler(c, &http.Response{Body: pr}, info)
	elapsed := time.Since(start)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Empty(t, recorder.Body.String(), "客户端已断开，不该往外写")
	return usage, info, elapsed
}

func writeMessageStart(w *io.PipeWriter, usageJSON string) {
	_, _ = fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-5-5\",\"usage\":%s}}\n\n", usageJSON)
}

func withClientGoneUsageGrace(t *testing.T, d time.Duration) {
	t.Helper()
	old := clientGoneUsageGrace
	clientGoneUsageGrace = d
	t.Cleanup(func() { clientGoneUsageGrace = old })
}

// 断开后上游送来 message_start：按上游实报的输入与缓存计费，不再按估算。
func TestClaudeStreamHandlerClientGoneBillsUpstreamMessageStart(t *testing.T) {
	withClientGoneUsageGrace(t, 3*time.Second)
	usage, info, elapsed := runClientGoneClaudeStream(t, func(w *io.PipeWriter) {
		time.Sleep(100 * time.Millisecond)
		writeMessageStart(w, `{"input_tokens":2,"cache_read_input_tokens":534005,"cache_creation_input_tokens":4013,"output_tokens":1}`)
		// 上游接着生成，但没人在听：不再写，等着被关闭。
	})

	assert.Less(t, elapsed, 2*time.Second, "拿到用量就该结束，不等满宽限期")
	assert.Equal(t, 2, usage.PromptTokens)
	assert.Equal(t, 534_005, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 4_013, usage.PromptTokensDetails.CachedCreationTokens)
	require.NotNil(t, usage.BillingUsage)
	assert.False(t, usage.BillingUsage.Estimated, "上游实报的用量不能标成估算")
	require.NotNil(t, usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, 534_005, usage.BillingUsage.ClaudeUsage.CacheReadInputTokens)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}

// 上游在宽限期内什么都没发：照旧按本地估算，并标成估算。
func TestClaudeStreamHandlerClientGoneNoEventFallsBackToEstimate(t *testing.T) {
	withClientGoneUsageGrace(t, 200*time.Millisecond)
	usage, _, elapsed := runClientGoneClaudeStream(t, func(w *io.PipeWriter) {})

	assert.Less(t, elapsed, 2*time.Second)
	assert.Equal(t, clientGoneEstimate, usage.PromptTokens)
	require.NotNil(t, usage.BillingUsage)
	assert.True(t, usage.BillingUsage.Estimated)
}

// message_start 只带输出、不带任何输入侧用量（有的中转把输入放到末尾的 message_delta）：
// 输入必须按估算补上，不能按上游的 0 计（否则断开的请求输入免费）。
func TestClaudeStreamHandlerClientGoneOutputOnlyMessageStartUsesEstimate(t *testing.T) {
	withClientGoneUsageGrace(t, 200*time.Millisecond)
	usage, _, _ := runClientGoneClaudeStream(t, func(w *io.PipeWriter) {
		writeMessageStart(w, `{"input_tokens":0,"output_tokens":1}`)
	})

	assert.Equal(t, clientGoneEstimate, usage.PromptTokens)
	require.NotNil(t, usage.BillingUsage)
	assert.True(t, usage.BillingUsage.Estimated)
	require.NotNil(t, usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, clientGoneEstimate, usage.BillingUsage.ClaudeUsage.InputTokens, "结算用的是计费用量，输入要在这里补上")
}

// 输入全部命中缓存（input_tokens=0、缓存读非 0）：这是上游实报，不能再叠一份估算。
func TestClaudeStreamHandlerClientGoneCacheOnlyInputIsNotEstimated(t *testing.T) {
	withClientGoneUsageGrace(t, 3*time.Second)
	usage, _, elapsed := runClientGoneClaudeStream(t, func(w *io.PipeWriter) {
		writeMessageStart(w, `{"input_tokens":0,"cache_read_input_tokens":500000,"output_tokens":1}`)
	})

	assert.Less(t, elapsed, 2*time.Second)
	assert.Equal(t, 0, usage.PromptTokens)
	assert.Equal(t, 500_000, usage.PromptTokensDetails.CachedTokens)
	require.NotNil(t, usage.BillingUsage)
	assert.False(t, usage.BillingUsage.Estimated)
}
