package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// withGroupEntryHostsForService 临时登记「分组 -> 允许的入口主机名」，测试结束后还原。
func withGroupEntryHostsForService(t *testing.T, entries map[string][]string) {
	t.Helper()
	generalSetting := operation_setting.GetGeneralSetting()
	saved := generalSetting.GroupEntryHosts
	generalSetting.GroupEntryHosts = entries
	operation_setting.RefreshGroupEntryHosts()
	t.Cleanup(func() {
		generalSetting.GroupEntryHosts = saved
		operation_setting.RefreshGroupEntryHosts()
	})
}

func newRequestAutoGroupsContextOnHost(host string) *gin.Context {
	ctx := newRequestAutoGroupsContext()
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Host = host
	return ctx
}

// auto 令牌不能把请求带进「不该走这个入口」的分组：受限分组在别的入口上直接不参与挑选。
func TestGetRequestAutoGroupsDropsGroupsNotAllowedOnThisEntry(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	withGroupEntryHostsForService(t, map[string][]string{"vip": {"vip.example.com"}})

	assert.Equal(t, []string{"default", "svip"},
		GetRequestAutoGroups(newRequestAutoGroupsContextOnHost("main.example.com:2087"), "default"))
	assert.Equal(t, []string{"vip", "default", "svip"},
		GetRequestAutoGroups(newRequestAutoGroupsContextOnHost("vip.example.com:2087"), "default"))
}

// 入口筛选排在令牌的个数上限之前：被筛掉的分组不占名额。
func TestGetRequestAutoGroupsEntryFilterAppliesBeforeTokenLimit(t *testing.T) {
	configureRequestAutoGroupsTest(t) // 上限 2
	withGroupEntryHostsForService(t, map[string][]string{"vip": {"vip.example.com"}})

	onMain := newRequestAutoGroupsContextOnHost("main.example.com")
	common.SetContextKey(onMain, constant.ContextKeyTokenAutoGroups, []string{"vip", "default", "svip"})
	assert.Equal(t, []string{"default", "svip"}, GetRequestAutoGroups(onMain, "default"))

	onVip := newRequestAutoGroupsContextOnHost("vip.example.com")
	common.SetContextKey(onVip, constant.ContextKeyTokenAutoGroups, []string{"vip", "default", "svip"})
	assert.Equal(t, []string{"vip", "default"}, GetRequestAutoGroups(onVip, "default"))
}

// 没有请求对象的上下文认不出入口，受限分组按「不在允许的入口上」处理。
func TestGetRequestAutoGroupsWithoutRequestDropsRestrictedGroups(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	withGroupEntryHostsForService(t, map[string][]string{"vip": {"vip.example.com"}})

	assert.Equal(t, []string{"default", "svip"}, GetRequestAutoGroups(newRequestAutoGroupsContext(), "default"))
}

// 没登记任何入口限制时，行为与原来完全一致。
func TestGetRequestAutoGroupsUnchangedWithoutEntryRestrictions(t *testing.T) {
	configureRequestAutoGroupsTest(t)

	assert.Equal(t, []string{"vip", "default", "svip"},
		GetRequestAutoGroups(newRequestAutoGroupsContextOnHost("main.example.com"), "default"))
}
