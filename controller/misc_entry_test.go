package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getStatusData(t *testing.T) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	GetStatus(context)

	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload.Data
}

// withEntrySettings 临时设置对客接口地址、分组入口与对客可见分组，测试结束后还原。
func withEntrySettings(t *testing.T, apiBase string, entries map[string][]string, usableGroups string) {
	t.Helper()
	general := operation_setting.GetGeneralSetting()
	savedBase, savedEntries := general.ApiBaseURL, general.GroupEntryHosts
	savedUsable := setting.UserUsableGroups2JSONString()
	savedOptionMap := common.OptionMap
	t.Cleanup(func() {
		general.ApiBaseURL, general.GroupEntryHosts = savedBase, savedEntries
		operation_setting.RefreshGroupEntryHosts()
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(savedUsable))
		common.OptionMap = savedOptionMap
	})
	common.OptionMap = map[string]string{}
	general.ApiBaseURL, general.GroupEntryHosts = apiBase, entries
	operation_setting.RefreshGroupEntryHosts()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(usableGroups))
}

// 令牌页的「复制服务器地址」「CC Switch」靠这两个字段给出能用的接入地址：
// 主站用对客地址（ServerAddress 带端口，留给支付回调），限定入口的分组用它自己的入口。
func TestGetStatusExposesApiBaseURLAndGroupEntryHost(t *testing.T) {
	withEntrySettings(t, "https://cc.yzzt.top",
		map[string][]string{"claude kiro": {"kiro.yzzt.top"}},
		`{"claude kiro":"claude kiro","满血稳定claude":"满血稳定claude"}`)

	data := getStatusData(t)
	assert.Equal(t, "https://cc.yzzt.top", data["api_base_url"])
	assert.Equal(t, map[string]any{"claude kiro": "kiro.yzzt.top"}, data["group_entry_host"])
}

// /api/status 不登录也能读：对客不可见的分组（如管理员专用分组）即使登记了入口也不下发，
// 免得把分组名泄露出去。
func TestGetStatusGroupEntryHostOmitsGroupsNotUsableByCustomers(t *testing.T) {
	withEntrySettings(t, "",
		map[string][]string{"claude kiro": {"kiro.yzzt.top"}, "sole": {"sole.example.com"}},
		`{"claude kiro":"claude kiro"}`)

	data := getStatusData(t)
	assert.Equal(t, "", data["api_base_url"])
	assert.Equal(t, map[string]any{"claude kiro": "kiro.yzzt.top"}, data["group_entry_host"])
}

func TestGetStatusGroupEntryHostIsEmptyObjectWhenNothingRestricted(t *testing.T) {
	withEntrySettings(t, "", map[string][]string{}, `{"claude kiro":"claude kiro"}`)

	data := getStatusData(t)
	assert.Equal(t, map[string]any{}, data["group_entry_host"])
}
