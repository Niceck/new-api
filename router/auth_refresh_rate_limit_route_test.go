package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useAuthRateLimits turns on the sensitive-route limiter and the session
// refresh limiter with small budgets. Both are read when the routes are
// registered, so callers must build the engine after this.
func useAuthRateLimits(t *testing.T, criticalNum int, refreshNum int) {
	t.Helper()

	previousCriticalEnable := common.CriticalRateLimitEnable
	previousCriticalNum := common.CriticalRateLimitNum
	previousCriticalDuration := common.CriticalRateLimitDuration
	previousRefreshEnable := common.AuthRefreshRateLimitEnable
	previousRefreshNum := common.AuthRefreshRateLimitNum
	previousRefreshDuration := common.AuthRefreshRateLimitDuration
	previousRedisEnabled := common.RedisEnabled

	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = criticalNum
	common.CriticalRateLimitDuration = 60
	common.AuthRefreshRateLimitEnable = true
	common.AuthRefreshRateLimitNum = refreshNum
	common.AuthRefreshRateLimitDuration = 90
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.CriticalRateLimitEnable = previousCriticalEnable
		common.CriticalRateLimitNum = previousCriticalNum
		common.CriticalRateLimitDuration = previousCriticalDuration
		common.AuthRefreshRateLimitEnable = previousRefreshEnable
		common.AuthRefreshRateLimitNum = previousRefreshNum
		common.AuthRefreshRateLimitDuration = previousRefreshDuration
		common.RedisEnabled = previousRedisEnabled
	})
}

// The in-memory limiter is shared by the whole test process and never reset,
// so every test run takes its own client address from a process-wide counter.
var authRateLimitRouteTestRuns atomic.Int64

func nextAuthRateLimitTestAddr() string {
	run := authRateLimitRouteTestRuns.Add(1)
	return fmt.Sprintf("198.18.%d.%d:40000", run>>8&0xff, run&0xff)
}

func postFromIP(engine http.Handler, path string, remoteAddr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.RemoteAddr = remoteAddr
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

// Every full page load of the console refreshes the session once. A customer
// pressing F5 to watch a top-up arrive used up the shared sensitive-route
// budget and was then locked out of password login for the rest of the window.
func TestAuthRefreshExhaustionDoesNotBlockLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useAuthRateLimits(t, 2, 3)
	engine := gin.New()
	SetApiRouter(engine)

	remoteAddr := nextAuthRateLimitTestAddr()
	for i := 1; i <= 3; i++ {
		response := postFromIP(engine, "/api/user/auth/refresh", remoteAddr)
		require.NotEqual(t, http.StatusTooManyRequests, response.Code, "refresh %d is within its own budget", i)
	}
	limited := postFromIP(engine, "/api/user/auth/refresh", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "90", limited.Header().Get("Retry-After"))

	login := postFromIP(engine, "/api/user/login", remoteAddr)
	assert.NotEqual(t, http.StatusTooManyRequests, login.Code, "password login must not share the refresh budget")
}

func TestLoginRateLimitStillAppliesAndDoesNotBlockAuthRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useAuthRateLimits(t, 2, 3)
	engine := gin.New()
	SetApiRouter(engine)

	remoteAddr := nextAuthRateLimitTestAddr()
	for i := 1; i <= 2; i++ {
		response := postFromIP(engine, "/api/user/login", remoteAddr)
		require.NotEqual(t, http.StatusTooManyRequests, response.Code, "login %d is within the sensitive-route budget", i)
	}
	limited := postFromIP(engine, "/api/user/login", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "60", limited.Header().Get("Retry-After"))

	refresh := postFromIP(engine, "/api/user/auth/refresh", remoteAddr)
	assert.NotEqual(t, http.StatusTooManyRequests, refresh.Code, "session refresh must not share the login budget")
}

func TestAuthRefreshRateLimitCanBeDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useAuthRateLimits(t, 2, 1)
	common.AuthRefreshRateLimitEnable = false
	engine := gin.New()
	SetApiRouter(engine)

	remoteAddr := nextAuthRateLimitTestAddr()
	for i := 1; i <= 5; i++ {
		response := postFromIP(engine, "/api/user/auth/refresh", remoteAddr)
		require.NotEqual(t, http.StatusTooManyRequests, response.Code, "refresh %d with the limiter disabled", i)
	}
}
