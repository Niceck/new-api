package service

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
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

func pngDataURI(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// OpenAI 模型的图片：估算阶段不联网，读不出尺寸也不拒请求。
// 旧实现对 URL 图片会下载、对解码失败直接返回错误，整个请求在转发前被拒——Responses 的工具输出图片
// 也计入估算之后，这条路径会更常走到。
func TestEstimateRequestTokenOpenAIImagesNeverDownloadOrReject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableTokenCountingForTest(t)
	InitTokenEncoders() // OpenAI 模型的文字走 tiktoken

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-6-astra")
	raw := `{"model":"gpt-6-astra","stream":true,"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"看图"},{"type":"input_image","image_url":"` + pngDataURI(t, 1024, 1024) + `"}]},
		{"type":"function_call","call_id":"c1","name":"screenshot","arguments":"{}"},
		{"type":"function_call_output","call_id":"c1","output":[{"type":"input_image","image_url":"` + srv.URL + `/s.png","detail":"high"}]},
		{"type":"computer_call_output","call_id":"c2","output":{"type":"computer_screenshot","image_url":"data:image/png;base64,@@@@not-base64@@@@"}}
	]}`
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(raw), &req))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true}

	tokens, err := EstimateRequestToken(ctx, req.GetTokenCountMeta(), info)
	require.NoError(t, err, "图片读不出来不能拒请求")
	assert.Equal(t, int32(0), hits.Load(), "估算阶段不下载 URL 图片")

	got := estimateOf(t, ctx)
	assert.Equal(t, 3, got.ImageCount)
	assert.Equal(t, 2, got.ImageFallbackCount, "URL 图片与解码失败的图片按固定额度计")
	// gpt-6-astra 走默认的 85/170：1024×1024 = 4 块 × 170 + 85 = 765；固定额度 = 3 × 85 = 255
	assert.Equal(t, 765+255+255, tokens-got.TextTokens-got.OverheadTokens)
}

// 估算方法按实际执行的分支记：OpenAI 模型用分词器，其它模型用估算公式。
func TestEstimateRequestTokenMethodReflectsCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableTokenCountingForTest(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "claude-opus-5-5")

	_, err := EstimateRequestToken(ctx, &types.TokenCountMeta{CombineText: "你好"}, &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude})
	require.NoError(t, err)
	assert.Equal(t, "heuristic", estimateOf(t, ctx).Method)
}
