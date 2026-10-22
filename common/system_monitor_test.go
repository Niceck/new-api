package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 移植自上游 09109ec31 的两组测试（去掉了容器口径部分），另加本站阈值 97 的取整边界。

// sample 按 5 秒间隔推进采样时间，模拟采样 goroutine 的节奏。
func sample(detector *overloadDetector, config PerformanceMonitorConfig, at time.Time, status SystemStatus) *SystemOverload {
	status.SampledAt = at
	detector.mark(&status, config)
	return CheckSystemOverload(status, config, at)
}

func TestSystemOverloadRequiresConsecutiveCPUSamples(t *testing.T) {
	config := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90, MemoryThreshold: 90, DiskThreshold: 95}
	start := time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)
	cpuSamples := []struct {
		usage        float64
		wantOverload bool
	}{
		{usage: 95, wantOverload: false},
		{usage: 95, wantOverload: false},
		{usage: 95, wantOverload: true},
		{usage: 95, wantOverload: true},
		{usage: 50, wantOverload: false}, // 回落即恢复
		{usage: 95, wantOverload: false}, // 重新累计
		{usage: 95, wantOverload: false},
		{usage: 95, wantOverload: true},
	}

	var detector overloadDetector
	for i, s := range cpuSamples {
		overload := sample(&detector, config, start.Add(time.Duration(i)*systemMonitorInterval), SystemStatus{CPUUsage: s.usage})
		if !s.wantOverload {
			assert.Nil(t, overload, "sample %d", i+1)
			continue
		}
		require.NotNil(t, overload, "sample %d", i+1)
		assert.Equal(t, "system_cpu_overloaded", overload.Code)
	}
}

func TestSystemOverloadMemoryAndDiskRejectOnFirstSample(t *testing.T) {
	config := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 97, MemoryThreshold: 90, DiskThreshold: 95}
	start := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)

	var detector overloadDetector
	assert.Nil(t, sample(&detector, config, start, SystemStatus{MemoryUsage: 50, DiskUsage: 50}))
	overload := sample(&detector, config, start.Add(systemMonitorInterval), SystemStatus{MemoryUsage: 99})
	require.NotNil(t, overload, "内存逼近耗尽时不等连续采样")
	assert.Equal(t, "system_memory_overloaded", overload.Code)

	detector = overloadDetector{}
	overload = sample(&detector, config, start, SystemStatus{DiskUsage: 99})
	require.NotNil(t, overload, "磁盘逼近写满时不等连续采样")
	assert.Equal(t, "system_disk_overloaded", overload.Code)
}

func TestSystemOverloadCPUStreakIgnoresOtherResources(t *testing.T) {
	config := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 97}
	start := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	var detector overloadDetector
	// CPU 隔一次超一次：始终凑不满连续 3 次。
	for i, usage := range []float64{99, 10, 99, 10, 99} {
		assert.Nil(t, sample(&detector, config, start.Add(time.Duration(i)*systemMonitorInterval), SystemStatus{CPUUsage: usage}), "sample %d", i+1)
	}
}

func TestSystemOverloadThresholdChangeResetsStreak(t *testing.T) {
	start := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	at := func(i int) time.Time { return start.Add(time.Duration(i) * systemMonitorInterval) }
	low := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90}
	high := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 97}

	var detector overloadDetector
	assert.Nil(t, sample(&detector, low, at(0), SystemStatus{CPUUsage: 91}))
	assert.Nil(t, sample(&detector, low, at(1), SystemStatus{CPUUsage: 91}))
	published := SystemStatus{CPUUsage: 99, SampledAt: at(2)}
	detector.mark(&published, low)
	require.NotNil(t, CheckSystemOverload(published, low, at(2)), "阈值 90 下连续 3 次")
	// 改成 97 后、下一次采样前的请求：旧标记是在 90 下形成的，作废放行。
	assert.Nil(t, CheckSystemOverload(published, high, at(2).Add(time.Second)))
	detector = overloadDetector{}
	assert.Nil(t, sample(&detector, low, at(0), SystemStatus{CPUUsage: 91}))
	assert.Nil(t, sample(&detector, low, at(1), SystemStatus{CPUUsage: 91}))
	// 调高到 97 后只有这一次超过新阈值，不能借旧阈值下的两次凑满。
	assert.Nil(t, sample(&detector, high, at(2), SystemStatus{CPUUsage: 99}))
	assert.Nil(t, sample(&detector, high, at(3), SystemStatus{CPUUsage: 99}))
	overload := sample(&detector, high, at(4), SystemStatus{CPUUsage: 99})
	require.NotNil(t, overload)
	assert.Equal(t, "system cpu overloaded (current: 99.0%, threshold: 97%)", overload.Error())
}

func TestSystemOverloadSampleGapResetsStreak(t *testing.T) {
	config := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90}
	start := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)

	var detector overloadDetector
	assert.Nil(t, sample(&detector, config, start, SystemStatus{CPUUsage: 99}))
	assert.Nil(t, sample(&detector, config, start.Add(5*time.Second), SystemStatus{CPUUsage: 99}))
	// 监控被阻塞一分钟后恢复：之前的两次作废，这一次重新从 1 计。
	resumed := start.Add(65 * time.Second)
	assert.Nil(t, sample(&detector, config, resumed, SystemStatus{CPUUsage: 99}))
	assert.Nil(t, sample(&detector, config, resumed.Add(5*time.Second), SystemStatus{CPUUsage: 99}))
	require.NotNil(t, sample(&detector, config, resumed.Add(10*time.Second), SystemStatus{CPUUsage: 99}))

	// 间隔恰好等于上限仍算连续。
	detector = overloadDetector{}
	assert.Nil(t, sample(&detector, config, start, SystemStatus{CPUUsage: 99}))
	assert.Nil(t, sample(&detector, config, start.Add(maxSampleGap), SystemStatus{CPUUsage: 99}))
	require.NotNil(t, sample(&detector, config, start.Add(2*maxSampleGap), SystemStatus{CPUUsage: 99}))
}

func TestSystemOverloadThresholdUsesIntegerPart(t *testing.T) {
	// 与旧判定一致：int(97.9) = 97，不大于阈值 97。
	config := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 97}
	start := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	var detector overloadDetector
	for i := 0; i < cpuConsecutiveSamples+1; i++ {
		assert.Nil(t, sample(&detector, config, start.Add(time.Duration(i)*systemMonitorInterval), SystemStatus{CPUUsage: 97.9}), "sample %d", i+1)
	}
	next := start.Add(time.Duration(cpuConsecutiveSamples+1) * systemMonitorInterval)
	for i := 0; i < cpuConsecutiveSamples; i++ {
		overload := sample(&detector, config, next.Add(time.Duration(i)*systemMonitorInterval), SystemStatus{CPUUsage: 98.0})
		if i < cpuConsecutiveSamples-1 {
			assert.Nil(t, overload, "sample %d", i+1)
			continue
		}
		require.NotNil(t, overload)
		assert.Equal(t, "system cpu overloaded (current: 98.0%, threshold: 97%)", overload.Error())
	}
}

func TestCheckSystemOverload(t *testing.T) {
	now := time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)
	enabled := PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90, MemoryThreshold: 90, DiskThreshold: 95}
	cpuOverloaded := SystemStatus{CPUUsage: 99.5, CPUOverloaded: true, CPUOverloadThreshold: 90, SampledAt: now}
	tests := []struct {
		name        string
		status      SystemStatus
		config      PerformanceMonitorConfig
		wantCode    string
		wantMessage string
	}{
		{
			name:        "fresh cpu overload",
			status:      cpuOverloaded,
			config:      enabled,
			wantCode:    "system_cpu_overloaded",
			wantMessage: "system cpu overloaded (current: 99.5%, threshold: 90%)",
		},
		{
			name:        "fresh memory overload",
			status:      SystemStatus{MemoryUsage: 96.2, SampledAt: now},
			config:      enabled,
			wantCode:    "system_memory_overloaded",
			wantMessage: "system memory overloaded (current: 96.2%, threshold: 90%)",
		},
		{
			name:        "fresh disk overload",
			status:      SystemStatus{DiskUsage: 97, SampledAt: now},
			config:      enabled,
			wantCode:    "system_disk_overloaded",
			wantMessage: "system disk overloaded (current: 97.0%, threshold: 95%)",
		},
		{
			name:        "memory threshold lowered applies to the latest sample at once",
			status:      SystemStatus{MemoryUsage: 85, SampledAt: now},
			config:      PerformanceMonitorConfig{Enabled: true, MemoryThreshold: 80},
			wantCode:    "system_memory_overloaded",
			wantMessage: "system memory overloaded (current: 85.0%, threshold: 80%)",
		},
		// CPU 标记是在阈值 90 下连续形成的：阈值一改（调高或调低），旧标记作废，按新阈值重新累计。
		{name: "cpu threshold lowered waits for a new streak", status: cpuOverloaded, config: PerformanceMonitorConfig{Enabled: true, CPUThreshold: 80}},
		{name: "cpu threshold raised before resample", status: cpuOverloaded, config: PerformanceMonitorConfig{Enabled: true, CPUThreshold: 97}},
		{name: "monitor disabled", status: cpuOverloaded, config: PerformanceMonitorConfig{Enabled: false, CPUThreshold: 90}},
		{name: "cpu threshold disabled at runtime", status: cpuOverloaded, config: PerformanceMonitorConfig{Enabled: true, CPUThreshold: 0}},
		{name: "cpu threshold raised above usage", status: cpuOverloaded, config: PerformanceMonitorConfig{Enabled: true, CPUThreshold: 100}},
		{name: "stale sample", status: SystemStatus{CPUUsage: 99.5, CPUOverloaded: true, CPUOverloadThreshold: 90, SampledAt: now.Add(-31 * time.Second)}, config: enabled},
		{name: "stale memory sample", status: SystemStatus{MemoryUsage: 99, SampledAt: now.Add(-31 * time.Second)}, config: enabled},
		{
			name:        "sample exactly at max age still counts",
			status:      SystemStatus{CPUUsage: 99.5, CPUOverloaded: true, CPUOverloadThreshold: 90, SampledAt: now.Add(-systemStatusMaxAge)},
			config:      enabled,
			wantCode:    "system_cpu_overloaded",
			wantMessage: "system cpu overloaded (current: 99.5%, threshold: 90%)",
		},
		{name: "missing sample", status: SystemStatus{CPUUsage: 99.5, CPUOverloaded: true, CPUOverloadThreshold: 90}, config: enabled},
		{name: "usage above threshold without consecutive samples", status: SystemStatus{CPUUsage: 99.5, SampledAt: now}, config: enabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overload := CheckSystemOverload(tt.status, tt.config, now)
			if tt.wantCode == "" {
				assert.Nil(t, overload)
				return
			}
			require.NotNil(t, overload)
			assert.Equal(t, tt.wantCode, overload.Code)
			assert.Equal(t, tt.wantMessage, overload.Error())
		})
	}
}
