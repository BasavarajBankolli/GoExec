package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/BasavarajBankolli/goexec/api"
)

type Collector struct {
	totalQueued    uint64
	totalCompleted uint64
	totalCacheHits uint64

	mu       sync.RWMutex
	verdicts map[api.Verdict]uint64

	latMu     sync.Mutex
	latencies []float64
}

func New() *Collector {
	return &Collector{
		verdicts:  make(map[api.Verdict]uint64),
		latencies: make([]float64, 0, 10_000),
	}
}

func (c *Collector) JobQueued() {
	atomic.AddUint64(&c.totalQueued, 1)
}

func (c *Collector) JobCompleted(v api.Verdict, d time.Duration, cached bool) {
	atomic.AddUint64(&c.totalCompleted, 1)
	if cached {
		atomic.AddUint64(&c.totalCacheHits, 1)
	}

	c.mu.Lock()
	c.verdicts[v]++
	c.mu.Unlock()

	ms := float64(d.Milliseconds())
	c.latMu.Lock()
	if len(c.latencies) < 10_000 {
		c.latencies = append(c.latencies, ms)
	} else {
		c.latencies[len(c.latencies)%500] = ms
	}
	c.latMu.Unlock()
}

type Snapshot struct {
	TotalQueued    uint64            `json:"total_queued"`
	TotalCompleted uint64            `json:"total_completed"`
	TotalCacheHits uint64            `json:"total_cache_hits"`
	CacheHitRate   float64           `json:"cache_hit_rate_pct"`
	Verdicts       map[string]uint64 `json:"verdicts"`
	LatencyP50Ms   float64           `json:"latency_p50_ms"`
	LatencyP95Ms   float64           `json:"latency_p95_ms"`
	LatencyP99Ms   float64           `json:"latency_p99_ms"`
}

func (c *Collector) Snapshot() Snapshot {
	completed := atomic.LoadUint64(&c.totalCompleted)
	hits := atomic.LoadUint64(&c.totalCacheHits)

	hitRate := 0.0
	if completed > 0 {
		hitRate = float64(hits) / float64(completed) * 100
	}

	c.mu.RLock()
	verdicts := make(map[string]uint64, len(c.verdicts))
	for k, v := range c.verdicts {
		verdicts[string(k)] = v
	}
	c.mu.RUnlock()

	// percentiles returns a []float64 slice — index into it, do NOT unpack.
	percs := c.percentiles(50, 95, 99)

	return Snapshot{
		TotalQueued:    atomic.LoadUint64(&c.totalQueued),
		TotalCompleted: completed,
		TotalCacheHits: hits,
		CacheHitRate:   hitRate,
		Verdicts:       verdicts,
		LatencyP50Ms:   percs[0],
		LatencyP95Ms:   percs[1],
		LatencyP99Ms:   percs[2],
	}
}

// percentiles returns the requested percentile values from the latency sample.
// It always returns a slice of the same length as pcts, filled with 0 when
// no samples are available.
func (c *Collector) percentiles(pcts ...float64) []float64 {
	c.latMu.Lock()
	cp := make([]float64, len(c.latencies))
	copy(cp, c.latencies)
	c.latMu.Unlock()

	out := make([]float64, len(pcts))
	if len(cp) == 0 {
		return out
	}

	// Insertion sort — fine for ≤10 000 elements.
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j] < cp[j-1]; j-- {
			cp[j], cp[j-1] = cp[j-1], cp[j]
		}
	}

	for i, p := range pcts {
		idx := int(float64(len(cp)-1) * p / 100)
		out[i] = cp[idx]
	}
	return out
}
