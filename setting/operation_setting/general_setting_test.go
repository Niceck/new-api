package operation_setting

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// 模拟一次配置更新：写结构体字段，再发布快照（生产里由 model.handleConfigUpdate 触发）。
func withStreamHeaderPolicy(t *testing.T, byChannel map[string]int, budgetSeconds int) {
	t.Helper()
	savedTimeouts := generalSetting.StreamHeaderTimeoutByChannel
	savedBudget := generalSetting.StreamHeaderTotalBudgetSeconds
	generalSetting.StreamHeaderTimeoutByChannel = byChannel
	generalSetting.StreamHeaderTotalBudgetSeconds = budgetSeconds
	RefreshStreamHeaderPolicy()
	t.Cleanup(func() {
		generalSetting.StreamHeaderTimeoutByChannel = savedTimeouts
		generalSetting.StreamHeaderTotalBudgetSeconds = savedBudget
		RefreshStreamHeaderPolicy()
	})
}

func TestStreamHeaderTimeout_DefaultIsOffForEveryChannel(t *testing.T) {
	require.Empty(t, GetGeneralSetting().StreamHeaderTimeoutByChannel)
	require.Zero(t, StreamHeaderTimeout(6))
	require.Zero(t, StreamHeaderTotalBudget())
}

func TestStreamHeaderTimeout_OnlyListedChannels(t *testing.T) {
	withStreamHeaderPolicy(t, map[string]int{"6": 40}, 95)

	require.Equal(t, 40*time.Second, StreamHeaderTimeout(6))
	require.Zero(t, StreamHeaderTimeout(1))
	require.Zero(t, StreamHeaderTimeout(60))
	require.Equal(t, 95*time.Second, StreamHeaderTotalBudget())
}

func TestStreamHeaderTimeout_UnusableEntriesAreIgnored(t *testing.T) {
	withStreamHeaderPolicy(t, map[string]int{
		"6": 0, "7": -5, "8": MaxStreamHeaderTimeoutSeconds + 1, "abc": 30, "010": 30, "9": 30,
	}, -1)
	require.Zero(t, StreamHeaderTimeout(10)) // 非规范的键不发布，免得和 "10" 互相覆盖

	for _, channelId := range []int{6, 7, 8} {
		require.Zero(t, StreamHeaderTimeout(channelId), channelId)
	}
	require.Equal(t, 30*time.Second, StreamHeaderTimeout(9))
	require.Zero(t, StreamHeaderTotalBudget())
}

func TestStreamHeaderTimeout_NilMapIsOff(t *testing.T) {
	withStreamHeaderPolicy(t, nil, 0)

	require.Zero(t, StreamHeaderTimeout(6))
}

// 没发布快照之前，改结构体字段不影响请求路径：请求路径只读快照。
func TestStreamHeaderTimeout_RequestPathReadsThePublishedSnapshot(t *testing.T) {
	withStreamHeaderPolicy(t, map[string]int{"6": 40}, 0)

	generalSetting.StreamHeaderTimeoutByChannel = map[string]int{"6": 5}
	require.Equal(t, 40*time.Second, StreamHeaderTimeout(6))

	RefreshStreamHeaderPolicy()
	require.Equal(t, 5*time.Second, StreamHeaderTimeout(6))
}

// go test -race 下必须干净：一边反复发布，一边在请求路径上读。
func TestStreamHeaderTimeout_ConcurrentRefreshAndRead(t *testing.T) {
	withStreamHeaderPolicy(t, map[string]int{"6": 40}, 95)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2000 {
				_ = StreamHeaderTimeout(6)
				_ = StreamHeaderTotalBudget()
			}
		}()
	}
	// 唯一的写入者反复「改配置 → 发布」，读的一方只会看到其中某一版完整的快照
	for i := range 200 {
		generalSetting.StreamHeaderTimeoutByChannel = map[string]int{"6": 40 + i%2}
		RefreshStreamHeaderPolicy()
	}
	wg.Wait()
	require.Equal(t, 41*time.Second, StreamHeaderTimeout(6))
}

// 管理接口写入的是字符串化的 JSON：整张表替换，被删掉的渠道必须真的消失。
func TestStreamHeaderTimeout_LoadsFromOptionValueAndReplacesWholeMap(t *testing.T) {
	setting := GeneralSetting{StreamHeaderTimeoutByChannel: map[string]int{"1": 30}}

	err := config.UpdateConfigFromMap(&setting, map[string]string{
		"stream_header_timeout_by_channel":   `{"6":40}`,
		"stream_header_total_budget_seconds": "95",
	})

	require.NoError(t, err)
	require.Equal(t, map[string]int{"6": 40}, setting.StreamHeaderTimeoutByChannel)
	require.Equal(t, 95, setting.StreamHeaderTotalBudgetSeconds)

	exported, err := config.ConfigToMap(&setting)
	require.NoError(t, err)
	require.JSONEq(t, `{"6":40}`, exported["stream_header_timeout_by_channel"])
	require.Equal(t, "95", exported["stream_header_total_budget_seconds"])
}

// 配置框架对解析失败是静默跳过的，所以写入前必须拦住：否则接口报成功、实际没生效。
func TestValidateStreamHeaderTimeouts(t *testing.T) {
	for _, value := range []string{`{}`, `{"6":40}`, `{"6":40,"12":600}`, `{"6":0}`} {
		require.NoError(t, ValidateStreamHeaderTimeouts(value), value)
	}
	for _, value := range []string{
		``, `null`, `[6]`, `{"6":"40"}`, `{"6":40.5}`, `{"abc":40}`, `{"0":40}`, `{"-6":40}`,
		`{"6":-1}`, `{"6":601}`, `{"6":9223372037}`, `{"6":40`, `40`,
		`{"6":null}`, // 空值不能被当成 0 放过
		`{"06":40}`,  // 非规范写法：会和 "6" 指向同一个渠道
		`{"6":40,"06":30}`, `{"+6":40}`, `{" 6":40}`,
	} {
		require.Error(t, ValidateStreamHeaderTimeouts(value), value)
	}
}

func TestValidateStreamHeaderTotalBudget(t *testing.T) {
	for _, value := range []string{"0", "95", "600"} {
		require.NoError(t, ValidateStreamHeaderTotalBudget(value), value)
	}
	for _, value := range []string{"", "-1", "601", "95.5", "abc", "9223372037"} {
		require.Error(t, ValidateStreamHeaderTotalBudget(value), value)
	}
}
