//go:build !windows

package monitor

import (
	"context"
	"time"
)

type unavailableCollector struct{}

func NewCollector(_ string) Collector { return unavailableCollector{} }

func (unavailableCollector) Collect(_ context.Context) []Metric {
	now := time.Now().UTC()
	return []Metric{
		{ID: "host_cpu", Label: "CPU usage", Unit: "percent", Scope: "host", State: "unavailable", SampledAt: now, Reason: "Windows host metrics require the native Windows server"},
		{ID: "host_memory", Label: "Memory used", Unit: "percent", Scope: "host", State: "unavailable", SampledAt: now, Reason: "Windows host metrics require the native Windows server"},
		{ID: "host_data_disk", Label: "Data disk used", Unit: "percent", Scope: "host", State: "unavailable", SampledAt: now, Reason: "Windows host metrics require the native Windows server"},
	}
}
