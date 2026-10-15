package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Each 2FA setup hashes four backup codes with bcrypt, so it is limited per IP
// and per user like the other critical endpoints.
func TestTwoFASetupRoute_IsRateLimitedPerIPAndPerUser(t *testing.T) {
	token := setupTronRouteAuthTest(t)
	previousLogDB := model.LOG_DB
	model.LOG_DB = model.DB
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	previousEnable, previousNum := common.CriticalRateLimitEnable, common.CriticalRateLimitNum
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum = true, 2
	t.Cleanup(func() { common.CriticalRateLimitEnable, common.CriticalRateLimitNum = previousEnable, previousNum })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	setup := func(remoteAddr string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/user/2fa/setup", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.RemoteAddr = remoteAddr
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response.Code
	}

	assert.NotEqual(t, http.StatusTooManyRequests, setup("198.51.100.21:1000"))
	assert.NotEqual(t, http.StatusTooManyRequests, setup("198.51.100.21:1000"))
	assert.Equal(t, http.StatusTooManyRequests, setup("198.51.100.21:1000"), "同一 IP 超限")
	assert.Equal(t, http.StatusTooManyRequests, setup("198.51.100.22:1000"), "换 IP 仍按用户限流")
}
