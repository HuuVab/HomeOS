//go:build windows

package monitor

import (
	"context"
	"testing"
	"time"
)

func TestWindowsHostCollector(t *testing.T) {
	collector := NewCollector(t.TempDir())
	time.Sleep(100 * time.Millisecond)
	samples := collector.Collect(context.Background())
	if len(samples) != 3 {
		t.Fatalf("got %d host samples, want 3", len(samples))
	}
	for _, sample := range samples {
		t.Logf("%s: state=%s value=%.1f reason=%s", sample.ID, sample.State, sample.Value, sample.Reason)
		if sample.Scope != "host" || sample.SampledAt.IsZero() {
			t.Errorf("invalid host sample: %+v", sample)
		}
		if sample.State == "available" && (sample.Value < 0 || sample.Value > 100) {
			t.Errorf("out-of-range host percentage: %+v", sample)
		}
		if sample.ID == "host_memory" || sample.ID == "host_data_disk" {
			if sample.State != "available" {
				t.Errorf("expected available %s: %+v", sample.ID, sample)
			}
		}
	}
}
