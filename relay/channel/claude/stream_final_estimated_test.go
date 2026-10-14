package claude

import (
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFinalResponseTestInfo(estimate int) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
	}
	info.SetEstimatePromptTokens(estimate)
	return info
}

// 上游一个用量事件都没给（客户端断开、没有 message_start）：输入按本地估算补，并标成估算。
func TestHandleStreamFinalResponseMarksLocalEstimate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	claudeInfo := &ClaudeResponseInfo{ResponseText: strings.Builder{}, Usage: &dto.Usage{}}

	HandleStreamFinalResponse(ctx, newFinalResponseTestInfo(334_480), claudeInfo)

	assert.Equal(t, 334_480, claudeInfo.Usage.PromptTokens)
	assert.Equal(t, 0, claudeInfo.Usage.CompletionTokens)
	require.NotNil(t, claudeInfo.Usage.BillingUsage)
	assert.True(t, claudeInfo.Usage.BillingUsage.Estimated)
}

// message_start 已给出输入与缓存、只是输出没收完：输入用上游的，不标估算。
func TestHandleStreamFinalResponseKeepsUpstreamInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	claudeInfo := &ClaudeResponseInfo{ResponseText: strings.Builder{}, Usage: &dto.Usage{PromptTokens: 2}}
	claudeInfo.Usage.PromptTokensDetails.CachedTokens = 333_005

	HandleStreamFinalResponse(ctx, newFinalResponseTestInfo(3_763_116), claudeInfo)

	assert.Equal(t, 2, claudeInfo.Usage.PromptTokens, "上游给了输入就不能被估算替换")
	if claudeInfo.Usage.BillingUsage != nil {
		assert.False(t, claudeInfo.Usage.BillingUsage.Estimated)
	}
}
