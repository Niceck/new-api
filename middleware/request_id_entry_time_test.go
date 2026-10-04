package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 入口中间件要记下请求进站的时间：「等响应头」的总预算从这里起算，
// 而不是从鉴权、读完请求体、选完渠道之后才记的 RequestStartTime。
func TestRequestId_RecordsRequestEntryTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	before := time.Now()
	var entry time.Time
	engine := gin.New()
	engine.Use(RequestId())
	engine.GET("/", func(c *gin.Context) {
		entry = common.GetContextKeyTime(c, constant.ContextKeyRequestEntryTime)
	})

	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.False(t, entry.Before(before))
	require.False(t, entry.After(time.Now()))
}
