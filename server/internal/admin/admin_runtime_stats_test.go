package admin

import (
	"runtime"
	"runtime/metrics"
	"testing"
	"time"
)

// Metric names are plain strings; a rename in a later Go release would silently read as zero.
func TestGoRuntimeMetricsExist(t *testing.T) {
	known := map[string]bool{}
	for _, description := range metrics.All() {
		known[description.Name] = true
	}
	for _, name := range goRuntimeMetrics {
		if !known[name] {
			t.Fatal("unknown runtime metric", name)
		}
	}
	runtime.GC()
	stats := readGoRuntime(time.Now())
	if stats.LogicalCPUs < 1 || stats.ProcessStartedMS <= 0 || stats.ProcessStartedMS > stats.SampledUnixMilli {
		t.Fatal("invalid process sampling identity", stats)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		if stats.CPUSecondsTotal == nil || *stats.CPUSecondsTotal < 0 {
			t.Fatal("process CPU time unavailable", stats)
		}
	}
	if stats.Goroutines == 0 || stats.HeapLiveBytes == 0 || stats.MemoryBytes < stats.HeapLiveBytes || stats.GCCycles == 0 || stats.LastGCUnixMilli == 0 {
		t.Fatal("implausible runtime snapshot", stats)
	}
}
