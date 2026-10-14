package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// 回归 2026-10-14 的误扣：Claude Code 长会话里每次读截图，图片都以 base64 嵌在 tool_result 中。
// 上游没回用量时本站按这个估算扣费；估算必须接近「文字 + 每张图固定额度」，不能随 base64 体积线性膨胀。
func TestEstimateRequestTokenClaudeToolResultScreenshots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevCount, prevMedia := constant.CountToken, constant.GetMediaToken
	constant.CountToken, constant.GetMediaToken = true, true
	t.Cleanup(func() { constant.CountToken, constant.GetMediaToken = prevCount, prevMedia })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "claude-opus-5-5")

	const shots = 5
	img := strings.Repeat("iVBORw0KGgoAAAANSUhEUg", 1_000_000/22) // 每张约 1MB base64
	var b strings.Builder
	b.WriteString(`{"model":"claude-opus-5-5","stream":true,"max_tokens":32000,"messages":[{"role":"user","content":"帮我检查页面布局"}`)
	for i := 0; i < shots; i++ {
		fmt.Fprintf(&b, `,{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%d","name":"Read","input":{"file_path":"/tmp/shot%d.png"}}]}`, i, i)
		fmt.Fprintf(&b, `,{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"%s"}}]}]}`, i, img)
	}
	b.WriteString(`]}`)

	var req dto.ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(b.String()), &req))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, IsStream: true}

	tokens, err := EstimateRequestToken(ctx, req.GetTokenCountMeta(), info)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, tokens, shots*520, "每张图至少按固定额度计")
	assert.Less(t, tokens, 10_000, "5MB 的截图不能被估成上百万 token")
}

// Claude 模型的图片按固定额度计，估算时不该为它下载 URL：旧实现对已知类型的 URL 也先整份下载，
// 下载失败还会直接拒掉请求；把 tool_result 里的图片认作媒体之后，这条路径只会更常走到。
func TestEstimateRequestTokenClaudeURLImageNotFetched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevCount, prevMedia := constant.CountToken, constant.GetMediaToken
	constant.CountToken, constant.GetMediaToken = true, true
	t.Cleanup(func() { constant.CountToken, constant.GetMediaToken = prevCount, prevMedia })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not really a png"))
	}))
	t.Cleanup(srv.Close)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "claude-opus-5-5")
	raw := `{"model":"claude-opus-5-5","stream":true,"messages":[
		{"role":"user","content":[{"type":"image","source":{"type":"url","url":"` + srv.URL + `/top.png"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"image","source":{"type":"url","url":"` + srv.URL + `/nested.png"}}]}]}
	]}`
	var req dto.ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(raw), &req))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, IsStream: true}

	tokens, err := EstimateRequestToken(ctx, req.GetTokenCountMeta(), info)
	require.NoError(t, err)
	assert.Equal(t, int32(0), hits.Load(), "固定估值不需要图片内容")
	assert.GreaterOrEqual(t, tokens, 2*520)
}
