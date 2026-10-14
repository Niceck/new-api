package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// 本地估算方法：文本按分词器（或按字符数）计，音频转写按时长换算。
const (
	tokenEstimateMethodTokenizer     = "tokenizer"
	tokenEstimateMethodTextNumber    = "text_number"
	tokenEstimateMethodAudioDuration = "audio_duration"
	tokenEstimateVersion             = 1
)

// TokenEstimateBreakdown 本地估算输入 token 的构成，由 EstimateRequestToken 记进请求上下文。
//
// 写进日志的 admin_info.input_token_estimate（普通用户看不到），用上游实报校准估算：例如 Claude 截图
// 每张按 520 计是否偏低（实报的输入 + 缓存读 + 缓存写 − 文字 − 补偿 ≈ 媒体）。上游给了用量时扣费按实报，
// 这里只是记录；上游没给用量时扣的就是 TotalTokens。它是「本地看得见的请求」的估算：加密内容、
// 服务端历史（previous_response_id、item_reference）不在其中。
type TokenEstimateBreakdown struct {
	Method         string
	TotalTokens    int // 估算总数（= 文字 + 补偿 + 媒体）
	TextTokens     int // 文字部分，不含补偿
	OverheadTokens int // OpenAI 格式的固定补偿：每条消息 3、每个工具 8、每个 name 3，再加 3
	ImageCount     int // 参与估算的图片张数（同一张图出现两次算两次）
	OtherFileCount int // 音频、视频、文档等其它文件个数（音频转写是上传的文件数）
}

// appendTokenEstimateAdminInfo 把本次请求的估算构成写进 admin_info；没做本地估算（关了计数、
// 实时语音等）就不写，不写成「零 token」。
func appendTokenEstimateAdminInfo(ctx *gin.Context, adminInfo map[string]interface{}) {
	if ctx == nil || adminInfo == nil {
		return
	}
	est, ok := common.GetContextKeyType[TokenEstimateBreakdown](ctx, constant.ContextKeyTokenEstimate)
	if !ok {
		return
	}
	adminInfo["input_token_estimate"] = map[string]interface{}{
		"version":          tokenEstimateVersion,
		"method":           est.Method,
		"total_tokens":     est.TotalTokens,
		"text_tokens":      est.TextTokens,
		"overhead_tokens":  est.OverheadTokens,
		"image_count":      est.ImageCount,
		"other_file_count": est.OtherFileCount,
	}
}
