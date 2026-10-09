package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withGroupEntryHostsForMiddleware 临时登记「分组 -> 允许的入口主机名」，测试结束后还原。
func withGroupEntryHostsForMiddleware(t *testing.T, entries map[string][]string, message string) {
	t.Helper()
	setting := operation_setting.GetGeneralSetting()
	savedEntries := setting.GroupEntryHosts
	savedMessage := setting.GroupEntryRejectMessage
	setting.GroupEntryHosts = entries
	setting.GroupEntryRejectMessage = message
	operation_setting.RefreshGroupEntryHosts()
	t.Cleanup(func() {
		setting.GroupEntryHosts = savedEntries
		setting.GroupEntryRejectMessage = savedMessage
		operation_setting.RefreshGroupEntryHosts()
	})
}

func newGroupEntryContext(host string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Host = host
	return ctx, recorder
}

func TestRejectGroupOnWrongEntry_NoRestrictionPassesEverything(t *testing.T) {
	ctx, recorder := newGroupEntryContext("cc.yzzt.top")

	require.False(t, rejectGroupOnWrongEntry(ctx, "claude kiro"))
	assert.False(t, ctx.IsAborted())
	assert.Zero(t, recorder.Body.Len())
}

func TestRejectGroupOnWrongEntry_RestrictedGroupOnMainEntryIsRejected(t *testing.T) {
	withGroupEntryHostsForMiddleware(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	for _, host := range []string{"cc.yzzt.top", "cc.yzzt.top:2087", "127.0.0.1:3000", ""} {
		ctx, recorder := newGroupEntryContext(host)

		require.True(t, rejectGroupOnWrongEntry(ctx, "claude kiro"), host)
		assert.True(t, ctx.IsAborted(), host)
		assert.Equal(t, http.StatusForbidden, recorder.Code, host)

		var body struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), host)
		assert.Contains(t, body.Error.Message, "claude kiro", host)
		assert.Contains(t, body.Error.Message, "https://kiro.yzzt.top", host)
		assert.Equal(t, "new_api_error", body.Error.Type, host)
		assert.Equal(t, "access_denied", body.Error.Code, host)
	}
}

func TestRejectGroupOnWrongEntry_RestrictedGroupOnItsOwnEntryPasses(t *testing.T) {
	withGroupEntryHostsForMiddleware(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	for _, host := range []string{"kiro.yzzt.top", "kiro.yzzt.top:2087", "KIRO.YZZT.TOP"} {
		ctx, recorder := newGroupEntryContext(host)

		require.False(t, rejectGroupOnWrongEntry(ctx, "claude kiro"), host)
		assert.False(t, ctx.IsAborted(), host)
		assert.Zero(t, recorder.Body.Len(), host)
	}
}

// 没登记的分组在哪个入口都照常：主站入口不因为别的分组受限而受影响。
func TestRejectGroupOnWrongEntry_OtherGroupsUnaffected(t *testing.T) {
	withGroupEntryHostsForMiddleware(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	for _, group := range []string{"满血稳定claude", "满血稳定codex", "sole", "auto"} {
		for _, host := range []string{"cc.yzzt.top", "cc.yzzt.top:2087", "kiro.yzzt.top"} {
			ctx, _ := newGroupEntryContext(host)
			require.False(t, rejectGroupOnWrongEntry(ctx, group), "%s @ %s", group, host)
			assert.False(t, ctx.IsAborted())
		}
	}
}

func TestRejectGroupOnWrongEntry_UsesCustomMessage(t *testing.T) {
	withGroupEntryHostsForMiddleware(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}},
		"{group} 线路请改用 https://{entry}:2087")
	ctx, recorder := newGroupEntryContext("cc.yzzt.top")

	require.True(t, rejectGroupOnWrongEntry(ctx, "claude kiro"))
	assert.Contains(t, recorder.Body.String(), "claude kiro 线路请改用 https://kiro.yzzt.top:2087")
}
