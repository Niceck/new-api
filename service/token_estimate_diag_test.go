package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enableTokenCountingForTest(t *testing.T) {
	t.Helper()
	prevCount, prevMedia := constant.CountToken, constant.GetMediaToken
	constant.CountToken, constant.GetMediaToken = true, true
	t.Cleanup(func() { constant.CountToken, constant.GetMediaToken = prevCount, prevMedia })
}

func estimateOf(t *testing.T, ctx *gin.Context) TokenEstimateBreakdown {
	t.Helper()
	got, ok := common.GetContextKeyType[TokenEstimateBreakdown](ctx, constant.ContextKeyTokenEstimate)
	require.True(t, ok, "估算构成要记进上下文")
	return got
}

// Claude：文字、两张图（一张在 tool_result 里）、一份文档；没有 OpenAI 格式的补偿。
func TestEstimateRequestTokenRecordsBreakdownClaude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableTokenCountingForTest(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "claude-opus-5-5")

	img := strings.Repeat("iVBORw0KGgo", 1000)
	raw := `{"model":"claude-opus-5-5","stream":true,"messages":[{"role":"user","content":[
		{"type":"text","text":"看两张图和一份文档"},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}},
		{"type":"tool_result","tool_use_id":"t","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}}]},
		{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + img + `"}}
	]}]}`
	var req dto.ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(raw), &req))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, IsStream: true}

	tokens, err := EstimateRequestToken(ctx, req.GetTokenCountMeta(), info)
	require.NoError(t, err)

	got := estimateOf(t, ctx)
	assert.Equal(t, "heuristic", got.Method, "Claude 的文字走估算公式，不是分词器")
	assert.Equal(t, tokens, got.TotalTokens)
	assert.Equal(t, 2, got.ImageCount)
	assert.Equal(t, 1, got.OtherFileCount)
	assert.Equal(t, 0, got.OverheadTokens)
	assert.Equal(t, tokens-2*520-4096, got.TextTokens)
	assert.Greater(t, got.TextTokens, 0)
}

// OpenAI 格式：补偿单列，文字 + 补偿 = 总数。（模型用走估算公式的，测试环境没初始化 tiktoken）
func TestEstimateRequestTokenRecordsBreakdownOpenAIOverhead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableTokenCountingForTest(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "claude-opus-5-5")

	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"claude-opus-5-5","messages":[
		{"role":"system","content":"你是助手"},{"role":"user","content":"你好"}]}`), &req))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}

	tokens, err := EstimateRequestToken(ctx, req.GetTokenCountMeta(), info)
	require.NoError(t, err)

	got := estimateOf(t, ctx)
	assert.Equal(t, 2*3+3, got.OverheadTokens, "两条消息各 3，再加 3")
	assert.Equal(t, tokens, got.TextTokens+got.OverheadTokens)
	assert.Equal(t, 0, got.ImageCount+got.OtherFileCount)
}

// 关了本地计数就不记，不能记成「零 token」。
func TestEstimateRequestTokenNoBreakdownWhenCountingDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = prev })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	_, err := EstimateRequestToken(ctx, &types.TokenCountMeta{CombineText: "hi"}, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	_, ok := common.GetContextKeyType[TokenEstimateBreakdown](ctx, constant.ContextKeyTokenEstimate)
	assert.False(t, ok)
}

// 写日志时放进 admin_info.input_token_estimate（成功请求也写，校准最需要实报的样本）；没有估算就不写。
func TestGenerateTextOtherInfoIncludesEstimate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyTokenEstimate, TokenEstimateBreakdown{
		Method: "tokenizer", TotalTokens: 5_200, TextTokens: 3_631, OverheadTokens: 9, ImageCount: 3,
	})
	other := GenerateTextOtherInfo(ctx, newInfo(), 2, 1.75, 5, 0, 0.1, 0, 1.75)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, map[string]interface{}{
		"version": 1, "method": "tokenizer", "total_tokens": 5_200, "text_tokens": 3_631,
		"overhead_tokens": 9, "image_count": 3, "image_fallback_count": 0, "other_file_count": 0,
	}, adminInfo["input_token_estimate"])
	_, local := adminInfo["local_count_tokens"]
	assert.False(t, local, "成功请求也写估算，不依赖本地兜底标记")

	bare, _ := gin.CreateTestContext(httptest.NewRecorder())
	other = GenerateTextOtherInfo(bare, newInfo(), 2, 1.75, 5, 0, 0.1, 0, 1.75)
	adminInfo, ok = other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	_, has := adminInfo["input_token_estimate"]
	assert.False(t, has)
}
