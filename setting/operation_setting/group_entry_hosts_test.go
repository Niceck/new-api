package operation_setting

import (
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withGroupEntryHosts 临时登记一批「分组 -> 允许的入口主机名」并发布快照，测试结束后还原。
func withGroupEntryHosts(t *testing.T, entries map[string][]string, message string) {
	t.Helper()
	savedEntries := generalSetting.GroupEntryHosts
	savedMessage := generalSetting.GroupEntryRejectMessage
	generalSetting.GroupEntryHosts = entries
	generalSetting.GroupEntryRejectMessage = message
	RefreshGroupEntryHosts()
	t.Cleanup(func() {
		generalSetting.GroupEntryHosts = savedEntries
		generalSetting.GroupEntryRejectMessage = savedMessage
		RefreshGroupEntryHosts()
	})
}

func TestGroupEntryHosts_DefaultRestrictsNothing(t *testing.T) {
	require.Empty(t, GetGeneralSetting().GroupEntryHosts)
	require.Empty(t, GetGeneralSetting().GroupEntryRejectMessage)

	require.True(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))
	require.True(t, GroupAllowedOnHost("claude kiro", ""))
	require.Empty(t, GroupEntryRejectMessage("claude kiro"))
}

func TestGroupEntryHosts_ListedGroupOnlyOnItsHosts(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	require.True(t, GroupAllowedOnHost("claude kiro", "kiro.yzzt.top"))
	require.False(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))
}

// 没登记的分组不受限：在主站入口、在别的分组的专用入口都照常能用。
func TestGroupEntryHosts_UnlistedGroupsAreUnrestricted(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	for _, host := range []string{"cc.yzzt.top", "cc.yzzt.top:2087", "kiro.yzzt.top", "127.0.0.1:3000", ""} {
		require.True(t, GroupAllowedOnHost("满血稳定claude", host), host)
		require.True(t, GroupAllowedOnHost("auto", host), host)
	}
}

// 端口、大小写、结尾的点都不算入口的一部分：带端口的老地址和无端口地址是同一个入口。
func TestGroupEntryHosts_HostIsComparedWithoutPortOrCase(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	for _, host := range []string{"kiro.yzzt.top:2087", "KIRO.yzzt.top", "Kiro.Yzzt.Top:443", "kiro.yzzt.top.", " kiro.yzzt.top ", "kiro.yzzt.top:"} {
		require.True(t, GroupAllowedOnHost("claude kiro", host), host)
	}
	for _, host := range []string{"cc.yzzt.top:2087", "CC.YZZT.TOP", "cc.yzzt.top."} {
		require.False(t, GroupAllowedOnHost("claude kiro", host), host)
	}
}

// 受限分组在任何没登记的入口都被拒，包括看不懂的 Host：新加的入口默认不放行。
func TestGroupEntryHosts_UnknownOrMalformedHostFailsClosed(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	for _, host := range []string{
		"", " ", "127.0.0.1:3000", "localhost", "[::1]:3000", "::1",
		"kiro.yzzt.top.evil.example", "xkiro.yzzt.top", "kiro.yzzt.top:2087:1", "kiro.yzzt.top/v1",
		"kiro.yzzt.top@cc.yzzt.top", "kiro.yzzt.top:2087@cc.yzzt.top", "kiro.yzzt.top:abc",
		"\u212Airo.yzzt.top", "\u212Airo.yzzt.top:2087", "kiro.yzzt.t\u00f8p", "kiro.yzzt.top..",
	} {
		require.False(t, GroupAllowedOnHost("claude kiro", host), "%q", host)
	}
}

func TestGroupEntryHosts_MultipleHostsAndGroups(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{
		"claude kiro": {"kiro.yzzt.top", "127.0.0.1"},
		"another":     {"b.example.com"},
	}, "")

	require.True(t, GroupAllowedOnHost("claude kiro", "127.0.0.1:3000"))
	require.True(t, GroupAllowedOnHost("claude kiro", "kiro.yzzt.top"))
	require.False(t, GroupAllowedOnHost("claude kiro", "b.example.com"))
	require.True(t, GroupAllowedOnHost("another", "b.example.com"))
	require.False(t, GroupAllowedOnHost("another", "kiro.yzzt.top"))
}

// 库里若有不合规的条目（批量写接口和直接改库不经过校验），按「更严」处理而不是放行：
// 不合规的主机名不发布；一个分组的主机名全都不合规时，这个分组在哪个入口都用不了。
func TestGroupEntryHosts_UnusableEntriesNeverWidenAccess(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{
		"claude kiro": {"https://kiro.yzzt.top", "KIRO.yzzt.top", "kiro.yzzt.top:2087", ""},
		"half":        {"ok.example.com", "bad host"},
		"":            {"x.example.com"},
	}, "")

	for _, host := range []string{"kiro.yzzt.top", "cc.yzzt.top", "https://kiro.yzzt.top"} {
		require.False(t, GroupAllowedOnHost("claude kiro", host), host)
	}
	require.True(t, GroupAllowedOnHost("half", "ok.example.com"))
	require.False(t, GroupAllowedOnHost("half", "bad host"))
	require.True(t, GroupAllowedOnHost("", "cc.yzzt.top")) // 空分组名不是有效条目
}

func TestGroupEntryHosts_ClearingLiftsTheRestriction(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")
	require.False(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))

	generalSetting.GroupEntryHosts = map[string][]string{}
	RefreshGroupEntryHosts()
	require.True(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))

	generalSetting.GroupEntryHosts = nil
	RefreshGroupEntryHosts()
	require.True(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))
}

// 快照发布后不再跟着配置字段变：请求路径读到的要么是旧的一整份，要么是新的一整份。
func TestGroupEntryHosts_SnapshotDoesNotAliasSetting(t *testing.T) {
	hosts := []string{"kiro.yzzt.top"}
	entries := map[string][]string{"claude kiro": hosts}
	withGroupEntryHosts(t, entries, "")

	hosts[0] = "cc.yzzt.top"
	entries["满血稳定claude"] = []string{"kiro.yzzt.top"}

	require.True(t, GroupAllowedOnHost("claude kiro", "kiro.yzzt.top"))
	require.False(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))
	require.True(t, GroupAllowedOnHost("满血稳定claude", "cc.yzzt.top"))
	require.Contains(t, GroupEntryRejectMessage("claude kiro"), "https://kiro.yzzt.top")
}

// go test -race 下必须干净：一边反复发布，一边在请求路径上读。
func TestGroupEntryHosts_ConcurrentRefreshAndRead(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				assert.True(t, GroupAllowedOnHost("claude kiro", "kiro.yzzt.top:2087"))
				_ = GroupEntryRejectMessage("claude kiro")
			}
		}()
	}
	for range 200 {
		RefreshGroupEntryHosts()
	}
	wg.Wait()
}

func TestGroupEntryRejectMessage_DefaultNamesGroupAndEntry(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top", "127.0.0.1"}}, "")

	message := GroupEntryRejectMessage("claude kiro")
	require.Contains(t, message, "claude kiro")
	require.Contains(t, message, "https://kiro.yzzt.top")
	require.NotContains(t, message, "127.0.0.1") // 只提第一个：登记顺序里排在最前的是对客地址
}

func TestGroupEntryRejectMessage_CustomTemplate(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}},
		"{group} 请改用 https://{entry}:2087（{group}）")

	require.Equal(t, "claude kiro 请改用 https://kiro.yzzt.top:2087（claude kiro）",
		GroupEntryRejectMessage("claude kiro"))
}

// 模板里写不写占位符都行；分组名里即使带着占位符字样也不会被二次替换。
func TestGroupEntryRejectMessage_PlaceholdersAreNotReexpanded(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"{entry}": {"kiro.yzzt.top"}}, "分组 {group} 走 {entry}")

	require.Equal(t, "分组 {entry} 走 kiro.yzzt.top", GroupEntryRejectMessage("{entry}"))
}

func TestGroupEntryRejectMessage_UnrestrictedGroupHasNoMessage(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, "")

	require.Empty(t, GroupEntryRejectMessage("满血稳定claude"))
}

func TestParseGroupEntryHosts_Accepts(t *testing.T) {
	cases := map[string]map[string][]string{
		`{}`:                                {},
		`{"claude kiro":["kiro.yzzt.top"]}`: {"claude kiro": {"kiro.yzzt.top"}},
		`{"claude kiro":["kiro.yzzt.top","127.0.0.1"],"b":["x.io"]}`: {"claude kiro": {"kiro.yzzt.top", "127.0.0.1"}, "b": {"x.io"}},
	}
	for value, want := range cases {
		got, err := ParseGroupEntryHosts(value)
		require.NoError(t, err, value)
		require.Equal(t, want, got, value)
	}
}

func TestParseGroupEntryHosts_Rejects(t *testing.T) {
	longHost := strings.Repeat("a", 250) + ".top"
	cases := map[string]string{
		`null`:                               "清空请写 {}",
		``:                                   "JSON 对象",
		`[]`:                                 "JSON 对象",
		`"kiro.yzzt.top"`:                    "JSON 对象",
		`{"claude kiro":"kiro.yzzt.top"}`:    "JSON 对象",
		`{"claude kiro":null}`:               "不能为 null",
		`{"claude kiro":[]}`:                 "至少要有一个",
		`{"claude kiro":[null]}`:             "不能为 null",
		`{"":["kiro.yzzt.top"]}`:             "分组名",
		`{" claude kiro":["kiro.yzzt.top"]}`: "分组名",
		`{"auto":["kiro.yzzt.top"]}`:         "auto",
		`{"g":["https://kiro.yzzt.top"]}`:    "主机名",
		`{"g":["kiro.yzzt.top:2087"]}`:       "主机名",
		`{"g":["kiro.yzzt.top/v1"]}`:         "主机名",
		`{"g":["Kiro.yzzt.top"]}`:            "主机名",
		`{"g":["kiro.yzzt.top."]}`:           "主机名",
		`{"g":[" kiro.yzzt.top"]}`:           "主机名",
		`{"g":[""]}`:                         "主机名",
		`{"g":["-kiro.yzzt.top"]}`:           "主机名",
		`{"g":["kiro..yzzt.top"]}`:           "主机名",
		`{"g":["` + longHost + `"]}`:         "主机名",
		`{"g":["a.io","a.io"]}`:              "重复",
	}
	for value, want := range cases {
		_, err := ParseGroupEntryHosts(value)
		require.Error(t, err, value)
		require.Contains(t, err.Error(), want, value)
	}
}

func TestParseGroupEntryHosts_LimitsSize(t *testing.T) {
	var hosts []string
	for i := range MaxGroupEntryHostsPerGroup + 1 {
		hosts = append(hosts, `"h`+strings.Repeat("x", i+1)+`.io"`)
	}
	_, err := ParseGroupEntryHosts(`{"g":[` + strings.Join(hosts, ",") + `]}`)
	require.ErrorContains(t, err, "最多")
}

func TestValidateGroupEntryRejectMessage(t *testing.T) {
	require.NoError(t, ValidateGroupEntryRejectMessage(""))
	require.NoError(t, ValidateGroupEntryRejectMessage("{group} 分组请把接口地址改为 https://{entry}"))
	require.NoError(t, ValidateGroupEntryRejectMessage("不带占位符也行"))

	require.ErrorContains(t, ValidateGroupEntryRejectMessage(strings.Repeat("长", MaxGroupEntryRejectMessageRunes+1)), "字以内")
	require.ErrorContains(t, ValidateGroupEntryRejectMessage("第一行\n第二行"), "控制字符")
	require.ErrorContains(t, ValidateGroupEntryRejectMessage("a\x00b"), "控制字符")
	require.ErrorContains(t, ValidateGroupEntryRejectMessage("   "), "空白")
	require.ErrorContains(t, ValidateGroupEntryRejectMessage("请改用 {host}"), "{host}")
}

// 配置框架（生产里由 model.handleConfigUpdate 调用）能把写入的 JSON 落到字段上，
// 整体替换而不是与旧值合并：删掉的分组不能残留。
func TestGroupEntryHosts_ConfigFrameworkReplacesWholeMap(t *testing.T) {
	withGroupEntryHosts(t, map[string][]string{"old": {"old.example.com"}}, "")

	config.UpdateConfigFromMap(&generalSetting, map[string]string{
		"group_entry_hosts":          `{"claude kiro":["kiro.yzzt.top"]}`,
		"group_entry_reject_message": "{group} 走 {entry}",
	})
	RefreshGroupEntryHosts()

	require.Equal(t, map[string][]string{"claude kiro": {"kiro.yzzt.top"}}, generalSetting.GroupEntryHosts)
	require.True(t, GroupAllowedOnHost("old", "cc.yzzt.top"))
	require.False(t, GroupAllowedOnHost("claude kiro", "cc.yzzt.top"))
	require.Equal(t, "claude kiro 走 kiro.yzzt.top", GroupEntryRejectMessage("claude kiro"))
}
