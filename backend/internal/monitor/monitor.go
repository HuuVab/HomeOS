package monitor

import (
	"context"
	"time"
)

// Metric describes what was measured and where. Unavailable samples keep
// their identity so the dashboard can show a reason instead of a false zero.
type Metric struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Scope     string    `json:"scope"`
	State     string    `json:"state"`
	SampledAt time.Time `json:"sampledAt"`
	Detail    string    `json:"detail,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

type Collector interface {
	Collect(context.Context) []Metric
}
