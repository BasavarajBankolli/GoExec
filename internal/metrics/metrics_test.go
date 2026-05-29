package metrics_test

import (
	"testing"
	"time"

	"github.com/BasavarajBankolli/goexec/api"
	"github.com/BasavarajBankolli/goexec/internal/metrics"
)

func TestMetricsCounters(t *testing.T) {
	m := metrics.New()
	m.JobQueued()
	m.JobQueued()
	m.JobCompleted(api.VerdictAccepted, 800*time.Millisecond, false)
	m.JobCompleted(api.VerdictTimeLimitExceeded, 5*time.Second, false)
	m.JobCompleted(api.VerdictAccepted, 600*time.Millisecond, true)

	snap := m.Snapshot()
	if snap.TotalQueued != 2 {
		t.Errorf("TotalQueued: got %d want 2", snap.TotalQueued)
	}
	if snap.TotalCompleted != 3 {
		t.Errorf("TotalCompleted: got %d want 3", snap.TotalCompleted)
	}
	if snap.TotalCacheHits != 1 {
		t.Errorf("TotalCacheHits: got %d want 1", snap.TotalCacheHits)
	}
	if snap.Verdicts[string(api.VerdictAccepted)] != 2 {
		t.Errorf("Accepted: got %d want 2", snap.Verdicts[string(api.VerdictAccepted)])
	}
}

func TestMetricsPercentiles(t *testing.T) {
	m := metrics.New()
	for i := 1; i <= 100; i++ {
		m.JobCompleted(api.VerdictAccepted, time.Duration(i*10)*time.Millisecond, false)
	}
	snap := m.Snapshot()
	if snap.LatencyP50Ms < 450 || snap.LatencyP50Ms > 550 {
		t.Errorf("P50: got %.0f want ~500ms", snap.LatencyP50Ms)
	}
	if snap.LatencyP99Ms < 900 || snap.LatencyP99Ms > 1010 {
		t.Errorf("P99: got %.0f want ~990ms", snap.LatencyP99Ms)
	}
}
