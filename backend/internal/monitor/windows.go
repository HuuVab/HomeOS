//go:build windows

package monitor

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

type windowsCollector struct {
	dataDir string
	mu      sync.Mutex
	lastCPU *cpu.TimesStat
}

func NewCollector(dataDir string) Collector {
	absPath, err := filepath.Abs(dataDir)
	if err != nil {
		absPath = dataDir
	}
	c := &windowsCollector{dataDir: absPath}
	if times, err := cpu.Times(false); err == nil && len(times) > 0 {
		c.lastCPU = &times[0]
	}
	return c
}

func (c *windowsCollector) Collect(ctx context.Context) []Metric {
	now := time.Now().UTC()
	samples := make([]Metric, 0, 3)
	samples = append(samples, c.cpuMetric(ctx, now))
	samples = append(samples, c.memoryMetric(ctx, now))
	samples = append(samples, c.diskMetric(ctx, now))
	return samples
}

func (c *windowsCollector) cpuMetric(ctx context.Context, now time.Time) Metric {
	sample := Metric{ID: "host_cpu", Label: "CPU usage", Unit: "percent", Scope: "host", State: "unavailable", SampledAt: now}
	times, err := cpu.TimesWithContext(ctx, false)
	if err != nil || len(times) == 0 {
		sample.Reason = "CPU counters could not be read"
		return sample
	}
	c.mu.Lock()
	previous := c.lastCPU
	current := times[0]
	c.lastCPU = &current
	c.mu.Unlock()
	if previous == nil {
		sample.Reason = "Waiting for a second CPU sample"
		return sample
	}
	busy := (current.User - previous.User) + (current.System - previous.System)
	idle := current.Idle - previous.Idle
	if busy < 0 || idle < 0 || busy+idle <= 0 {
		sample.Reason = "CPU counters are not ready"
		return sample
	}
	sample.Value = math.Min(100, math.Max(0, 100*busy/(busy+idle)))
	sample.State = "available"
	return sample
}

func (c *windowsCollector) memoryMetric(ctx context.Context, now time.Time) Metric {
	sample := Metric{ID: "host_memory", Label: "Memory used", Unit: "percent", Scope: "host", State: "unavailable", SampledAt: now}
	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil || memory == nil || memory.Total == 0 {
		sample.Reason = "Memory information could not be read"
		return sample
	}
	used := memory.Total - memory.Available
	sample.Value = 100 * float64(used) / float64(memory.Total)
	sample.Detail = fmt.Sprintf("%s of %s", gib(used), gib(memory.Total))
	sample.State = "available"
	return sample
}

func (c *windowsCollector) diskMetric(ctx context.Context, now time.Time) Metric {
	sample := Metric{ID: "host_data_disk", Label: "Data disk used", Unit: "percent", Scope: "host", State: "unavailable", SampledAt: now}
	usage, err := disk.UsageWithContext(ctx, c.dataDir)
	if err != nil || usage == nil || usage.Total == 0 {
		sample.Reason = "Data disk information could not be read"
		return sample
	}
	sample.Value = usage.UsedPercent
	sample.Detail = fmt.Sprintf("%s of %s", gib(usage.Used), gib(usage.Total))
	sample.State = "available"
	return sample
}

func gib(bytes uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/(1024*1024*1024))
}
