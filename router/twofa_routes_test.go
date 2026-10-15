package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each 2FA setup regenerates and bcrypt-hashes four backup codes, so it is
// limited per IP and per user like the other critical endpoints. The in-memory
// limiter is shared by the whole test process and never reset, so every run
// uses addresses and user ids no other run has used.
func TestTwoFASetupRoute_IsRateLimitedPerIPAndPerUser(t *testing.T) {
	setupTronRouteAuthTest(t)
	previousLogDB := model.LOG_DB
	model.LOG_DB = model.DB
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	previousEnable, previousNum := common.CriticalRateLimitEnable, common.CriticalRateLimitNum
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum = true, 2
	t.Cleanup(func() { common.CriticalRateLimitEnable, common.CriticalRateLimitNum = previousEnable, previousNum })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	run := time.Now().UnixNano()
	address := func(index int64) string {
		n := (run + index) & 0xffffff
		return fmt.Sprintf("10.%d.%d.%d:1000", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
	}
	user := func(index int) string {
		id := int(run%1_000_000)*10 + 2_000_000 + index
		token := fmt.Sprintf("twofa-route-%d", id)
		require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("twofa-%d", id), Password: "password-placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1, AffCode: fmt.Sprintf("twofa-aff-%d", id)}).Error)
		return token
	}
	setup := func(token string, remoteAddr string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/user/2fa/setup", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.RemoteAddr = remoteAddr
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	requireSetUp := func(response *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), `"success":true`)
	}

	// Per IP: a second user behind an exhausted address is refused although
	// their own budget is untouched.
	sharedIP := address(0)
	first := user(1)
	requireSetUp(setup(first, sharedIP))
	requireSetUp(setup(first, sharedIP))
	assert.Equal(t, http.StatusTooManyRequests, setup(user(2), sharedIP).Code, "同一 IP 换用户仍被拦")

	// Per user: one user moving across fresh addresses is refused although
	// each address is untouched.
	mover := user(3)
	requireSetUp(setup(mover, address(1)))
	requireSetUp(setup(mover, address(2)))
	assert.Equal(t, http.StatusTooManyRequests, setup(mover, address(3)).Code, "同一用户换 IP 仍被拦")
}
