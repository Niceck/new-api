package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 结算时整份用量缺失、按本地估算的输入计费：要打上本地估算标记，日志不能记成上游实报。
func TestCalculateTextQuotaSummaryNilUsageMarkedLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "test-model",
		PriceData: hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
	}
	relayInfo.SetEstimatePromptTokens(2_000)

	summary := calculateTextQuotaSummary(ctx, relayInfo, nil)
	assert.Equal(t, 2_000, summary.PromptTokens)
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens))
}
