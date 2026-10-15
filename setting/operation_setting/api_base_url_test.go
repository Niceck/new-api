package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// 空串 = 不设（前端退回 ServerAddress）；其余只认「协议 + 主机[:端口]」。
func TestValidateApiBaseURL_AcceptsEmptyOrSchemeAndHost(t *testing.T) {
	for _, value := range []string{
		"",
		"https://cc.yzzt.top",
		"https://cc.yzzt.top:2087",
		"http://127.0.0.1:3000",
		"http://[::1]:3000",
		"https://CC.yzzt.top",
	} {
		require.NoError(t, ValidateApiBaseURL(value), value)
	}
}

// 带路径、结尾斜杠、查询串、账号密码、空白的都拒绝：前端会在后面拼 /v1，
// 写成 https://x/ 会变成 https://x//v1；这个值还会原样进客户端的一键导入链接。
func TestValidateApiBaseURL_RejectsAnythingBeyondSchemeAndHost(t *testing.T) {
	for _, value := range []string{
		"cc.yzzt.top",
		"//cc.yzzt.top",
		"ftp://cc.yzzt.top",
		"javascript:alert(1)",
		"https://",
		"https://cc.yzzt.top/",
		"https://cc.yzzt.top/v1",
		"https://cc.yzzt.top?x=1",
		"https://cc.yzzt.top#top",
		"https://user:pass@cc.yzzt.top",
		" https://cc.yzzt.top",
		"https://cc.yzzt.top\n",
		"https://cc yzzt.top",
		// 端口：空、非数字、越界
		"https://cc.yzzt.top:",
		"https://cc.yzzt.top:abc",
		"https://cc.yzzt.top:0",
		"https://cc.yzzt.top:99999",
		// 非 ASCII（全角空格、不换行空格、未转成 punycode 的域名）与主机名里不该有的字符
		"https://cc.yzzt.top　",
		"https://cc.yzzt.top ",
		"https://例子.com",
		"https://cc.yzzt.top\\x",
		"https://cc_yzzt.top",
		"https://-cc.yzzt.top",
	} {
		require.Error(t, ValidateApiBaseURL(value), value)
	}
}

func TestGeneralSetting_ApiBaseURLDefaultsEmptyAndLoadsFromOption(t *testing.T) {
	require.Empty(t, GetGeneralSetting().ApiBaseURL)

	saved := generalSetting.ApiBaseURL
	t.Cleanup(func() { generalSetting.ApiBaseURL = saved })
	require.NoError(t, config.UpdateConfigFromMap(&generalSetting, map[string]string{
		"api_base_url": "https://cc.yzzt.top",
	}))
	require.Equal(t, "https://cc.yzzt.top", GetGeneralSetting().ApiBaseURL)
}
