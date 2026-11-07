package admin

import (
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"time"
)

var processStarted = time.Now()

// goRuntime is the 概览 page's view of this server process. runtime/metrics and
// ReadGCStats read without stopping the world; the counters are cumulative since
// start, so the page derives the allocation rate between two refreshes.
type goRuntime struct {
	GoVersion        string   `json:"go_version"`
	Platform         string   `json:"platform"`
	GOMAXPROCS       int      `json:"gomaxprocs"`
	LogicalCPUs      int      `json:"logical_cpus"`
	ProcessStartedMS int64    `json:"process_started_unix_ms"`
	CPUSecondsTotal  *float64 `json:"process_cpu_seconds_total"`
	UptimeSeconds    int64    `json:"uptime_seconds"`
	Goroutines       uint64   `json:"goroutines"`
	HeapLiveBytes    uint64   `json:"heap_live_bytes"`
	HeapGoalBytes    uint64   `json:"heap_goal_bytes"`
	MemoryBytes      uint64   `json:"memory_bytes"` // mapped by the runtime, minus what it returned to the OS
	AllocBytesTotal  uint64   `json:"alloc_bytes_total"`
	GCCycles         uint64   `json:"gc_cycles"`
	GCCPUFraction    float64  `json:"gc_cpu_fraction"`
	LastGCUnixMilli  int64    `json:"last_gc_unix_ms,omitempty"`
	LastGCPauseNanos int64    `json:"last_gc_pause_ns,omitempty"`
	SampledUnixMilli int64    `json:"sampled_unix_ms"`
}

var goRuntimeMetrics = [...]string{
	"/sched/goroutines:goroutines",
	"/memory/classes/heap/objects:bytes",
	"/gc/heap/goal:bytes",
	"/memory/classes/total:bytes",
	"/memory/classes/heap/released:bytes",
	"/gc/heap/allocs:bytes",
	"/gc/cycles/total:gc-cycles",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/total:cpu-seconds",
}

func readGoRuntime(now time.Time) goRuntime {
	var samples [len(goRuntimeMetrics)]metrics.Sample
	for index, name := range goRuntimeMetrics {
		samples[index].Name = name
	}
	metrics.Read(samples[:])
	// An unknown name reads as KindBad, which counts as zero here and fails the test.
	value := func(index int) uint64 {
		if samples[index].Value.Kind() == metrics.KindUint64 {
			return samples[index].Value.Uint64()
		}
		return 0
	}
	seconds := func(index int) float64 {
		if samples[index].Value.Kind() == metrics.KindFloat64 {
			return samples[index].Value.Float64()
		}
		return 0
	}
	stats := goRuntime{
		GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0),
		LogicalCPUs: runtime.NumCPU(), ProcessStartedMS: processStarted.UnixMilli(), CPUSecondsTotal: processCPUSeconds(),
		UptimeSeconds: int64(now.Sub(processStarted) / time.Second), Goroutines: value(0),
		HeapLiveBytes: value(1), HeapGoalBytes: value(2), AllocBytesTotal: value(5), GCCycles: value(6),
		SampledUnixMilli: now.UnixMilli(),
	}
	if total, released := value(3), value(4); total > released {
		stats.MemoryBytes = total - released
	}
	if total := seconds(8); total > 0 {
		stats.GCCPUFraction = seconds(7) / total
	}
	var gc debug.GCStats
	debug.ReadGCStats(&gc)
	if !gc.LastGC.IsZero() && len(gc.Pause) > 0 {
		stats.LastGCUnixMilli, stats.LastGCPauseNanos = gc.LastGC.UnixMilli(), gc.Pause[0].Nanoseconds()
	}
	return stats
}
