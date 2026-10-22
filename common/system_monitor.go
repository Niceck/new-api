package common

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

// DiskSpaceInfo 磁盘空间信息
type DiskSpaceInfo struct {
	// 总空间（字节）
	Total uint64 `json:"total"`
	// 可用空间（字节）
	Free uint64 `json:"free"`
	// 已用空间（字节）
	Used uint64 `json:"used"`
	// 使用百分比
	UsedPercent float64 `json:"used_percent"`
}

// SystemStatus 系统状态信息
type SystemStatus struct {
	CPUUsage    float64
	MemoryUsage float64
	DiskUsage   float64
	SampledAt   time.Time
	// CPU 过载判定带连续性要求：单次采样的尖峰不再直接熔断，需连续多次超阈值。
	// CPUOverloadThreshold 是形成这个标记时的阈值，与当前阈值不一致时标记作废。
	CPUOverloaded        bool
	CPUOverloadThreshold int
}

// SystemOverload 是哨兵据以拒绝请求的过载项，Error 文本带当前值与生效阈值（与此前的报错文案一致）。
type SystemOverload struct {
	Code      string
	Resource  string
	Usage     float64
	Threshold int
}

func (o *SystemOverload) Error() string {
	return fmt.Sprintf("system %s overloaded (current: %.1f%%, threshold: %d%%)", o.Resource, o.Usage, o.Threshold)
}

var latestSystemStatus atomic.Value

const (
	systemMonitorInterval = 5 * time.Second
	// CPU 连续 3 次采样（约 15 秒）仍高于阈值才判定过载，任一采样回落到阈值以下即恢复。
	// 本站 2 核整机上的 CPU 拒绝多是几秒的尖峰（2026-10 统计：阈值 97 之后约一半落在尖峰头 10 秒内），
	// 对这种尖峰拒绝客户请求得不偿失。移植自上游 09109ec31 的连续性与新鲜度判定；
	// 上游同一提交里改看容器自身 CPU / 内存的部分没有移植——本站刻意按整机口径保护。
	// 内存、磁盘逼近耗尽时多放进来的请求可能直接导致 OOM 或写入失败，仍按最新一次采样对当前阈值
	// 判定（与上游不同：上游三项都要求连续 3 次；尖峰统计只证明了 CPU 适合延迟保护）。
	cpuConsecutiveSamples = 3
	// 相邻两次采样间隔超过该时长（监控被阻塞或暂停过）时，之前累计的连续次数作废。
	maxSampleGap = 3 * systemMonitorInterval
	// 采样超过该时长未刷新（监控未运行或被阻塞）时，哨兵不据此熔断。
	systemStatusMaxAge = 30 * time.Second
)

func init() {
	latestSystemStatus.Store(SystemStatus{})
}

// StartSystemMonitor 启动系统监控
func StartSystemMonitor() {
	go func() {
		var detector overloadDetector
		for {
			config := GetPerformanceMonitorConfig()
			if !config.Enabled {
				detector = overloadDetector{}
				latestSystemStatus.Store(SystemStatus{SampledAt: time.Now()})
				time.Sleep(systemMonitorInterval)
				continue
			}

			status := collectSystemStatus()
			detector.mark(&status, config)
			latestSystemStatus.Store(status)

			time.Sleep(systemMonitorInterval)
		}
	}()
}

// overloadDetector 累计 CPU 在同一阈值下连续超阈值的采样次数：连续 cpuConsecutiveSamples 次
// 才标记过载，任一次回落、阈值变化或采样中断都清零重新累计。只在采样 goroutine 里使用。
type overloadDetector struct {
	lastSampledAt time.Time
	cpuSamples    int
	cpuThreshold  int
}

func (d *overloadDetector) mark(status *SystemStatus, config PerformanceMonitorConfig) {
	// 采样中断过（之前的连续次数不再代表「连续」），或阈值热改过（旧阈值下的次数不能算进新阈值）。
	gap := !d.lastSampledAt.IsZero() && status.SampledAt.Sub(d.lastSampledAt) > maxSampleGap
	if gap || config.CPUThreshold != d.cpuThreshold {
		*d = overloadDetector{cpuThreshold: config.CPUThreshold}
	}
	d.lastSampledAt = status.SampledAt
	if exceedsThreshold(status.CPUUsage, config.CPUThreshold) {
		d.cpuSamples = min(d.cpuSamples+1, cpuConsecutiveSamples)
	} else {
		d.cpuSamples = 0
	}
	status.CPUOverloaded = d.cpuSamples == cpuConsecutiveSamples
	status.CPUOverloadThreshold = config.CPUThreshold
}

// exceedsThreshold 判断采样值是否超过阈值；阈值 <= 0 表示不检查该项。
func exceedsThreshold(usage float64, threshold int) bool {
	return threshold > 0 && int(usage) > threshold
}

// collectSystemStatus 按整机口径采样 CPU、内存与缓存目录所在磁盘。
func collectSystemStatus() SystemStatus {
	status := SystemStatus{SampledAt: time.Now()}

	// 注意：cpu.Percent(0, false) 返回自上次调用以来的 CPU 使用率，即上一个采样间隔的平均值
	percents, err := cpu.Percent(0, false)
	if err == nil && len(percents) > 0 {
		status.CPUUsage = percents[0]
	}

	memInfo, err := mem.VirtualMemory()
	if err == nil {
		status.MemoryUsage = memInfo.UsedPercent
	}

	diskInfo := GetDiskSpaceInfo()
	if diskInfo.Total > 0 {
		status.DiskUsage = diskInfo.UsedPercent
	}
	return status
}

// GetSystemStatus 获取当前系统状态
func GetSystemStatus() SystemStatus {
	return latestSystemStatus.Load().(SystemStatus)
}

// CheckSystemOverload 返回哨兵应据以熔断的过载项。监控关闭、采样缺失或已超过
// systemStatusMaxAge 时返回 nil。CPU 只认在当前阈值下连续形成的过载标记：
// 运行时关闭或改过阈值后立即放行，按新阈值重新累计。内存、磁盘按最新一次采样对当前阈值判定。
func CheckSystemOverload(status SystemStatus, config PerformanceMonitorConfig, now time.Time) *SystemOverload {
	if !config.Enabled || status.SampledAt.IsZero() || now.Sub(status.SampledAt) > systemStatusMaxAge {
		return nil
	}
	if status.CPUOverloaded && status.CPUOverloadThreshold == config.CPUThreshold &&
		exceedsThreshold(status.CPUUsage, config.CPUThreshold) {
		return &SystemOverload{Code: "system_cpu_overloaded", Resource: "cpu", Usage: status.CPUUsage, Threshold: config.CPUThreshold}
	}
	if exceedsThreshold(status.MemoryUsage, config.MemoryThreshold) {
		return &SystemOverload{Code: "system_memory_overloaded", Resource: "memory", Usage: status.MemoryUsage, Threshold: config.MemoryThreshold}
	}
	if exceedsThreshold(status.DiskUsage, config.DiskThreshold) {
		return &SystemOverload{Code: "system_disk_overloaded", Resource: "disk", Usage: status.DiskUsage, Threshold: config.DiskThreshold}
	}
	return nil
}
