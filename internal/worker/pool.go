package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BasavarajBankolli/goexec/api"
	"github.com/BasavarajBankolli/goexec/internal/cache"
	"github.com/BasavarajBankolli/goexec/internal/executor"
	"github.com/BasavarajBankolli/goexec/internal/metrics"
)

type Pool struct {
	jobs    chan api.Job
	exec    *executor.Executor
	cache   *cache.Cache
	metrics *metrics.Collector
	wg      sync.WaitGroup

	// pending: jobs still executing — job ID → result channel.
	mu      sync.RWMutex
	pending map[string]chan api.Result

	// completed: finished results kept 10 min so GET /jobs/{id} works
	// even after the worker has cleaned up the pending entry.
	completedMu sync.RWMutex
	completed   map[string]api.Result

	activeWorkers int64
}

func New(workerCount, queueSize int, exec *executor.Executor, c *cache.Cache, m *metrics.Collector) *Pool {
	p := &Pool{
		jobs:      make(chan api.Job, queueSize),
		exec:      exec,
		cache:     c,
		metrics:   m,
		pending:   make(map[string]chan api.Result),
		completed: make(map[string]api.Result),
	}

	for i := 0; i < workerCount; i++ {
		p.wg.Add(1)
		go p.runWorker(i)
	}

	// Evict completed results every 10 minutes.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			p.completedMu.Lock()
			p.completed = make(map[string]api.Result)
			p.completedMu.Unlock()
		}
	}()

	log.Printf("[pool] started %d workers, queue capacity %d", workerCount, queueSize)
	return p
}

func (p *Pool) SubmitJob(job api.Job) error {
	p.mu.Lock()
	p.pending[job.ID] = job.ResultCh
	p.mu.Unlock()

	select {
	case p.jobs <- job:
		p.metrics.JobQueued()
		return nil
	default:
		p.mu.Lock()
		delete(p.pending, job.ID)
		p.mu.Unlock()
		return fmt.Errorf("job queue is full; try again later")
	}
}

// WaitForResult returns a completed result immediately, or blocks until the
// job finishes. Works whether you call it before or after the job completes.
func (p *Pool) WaitForResult(ctx context.Context, jobID string) (api.Result, error) {
	// Fast path: already finished.
	p.completedMu.RLock()
	if result, ok := p.completed[jobID]; ok {
		p.completedMu.RUnlock()
		return result, nil
	}
	p.completedMu.RUnlock()

	// Slow path: still running — wait on channel.
	p.mu.RLock()
	ch, ok := p.pending[jobID]
	p.mu.RUnlock()

	if !ok {
		return api.Result{}, fmt.Errorf("job %s not found (unknown or expired)", jobID)
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return api.Result{}, ctx.Err()
	}
}

func (p *Pool) Shutdown() {
	close(p.jobs)
	p.wg.Wait()
	log.Println("[pool] all workers stopped")
}

func (p *Pool) ActiveWorkers() int64 { return atomic.LoadInt64(&p.activeWorkers) }
func (p *Pool) QueueLen() int        { return len(p.jobs) }

func (p *Pool) runWorker(id int) {
	defer p.wg.Done()
	log.Printf("[worker %d] ready", id)
	for job := range p.jobs {
		atomic.AddInt64(&p.activeWorkers, 1)
		p.executeJob(id, job)
		atomic.AddInt64(&p.activeWorkers, -1)
	}
	log.Printf("[worker %d] stopped", id)
}

func (p *Pool) executeJob(workerID int, job api.Job) {
	start := time.Now()

	if cached, ok := p.cache.Get(job); ok {
		cached.Cached = true
		log.Printf("[worker %d] cache hit for job %s", workerID, job.ID)
		p.metrics.JobCompleted(cached.Verdict, time.Since(start), true)
		p.deliver(job.ID, cached)
		return
	}

	log.Printf("[worker %d] executing job %s (lang=%s)", workerID, job.ID, job.Request.Language)
	result := p.exec.RunJob(job)
	elapsed := time.Since(start)

	// Only cache successful results — errors should be retried fresh.
	if result.Verdict == api.VerdictAccepted {
		p.cache.Set(job, result)
	}
	p.metrics.JobCompleted(result.Verdict, elapsed, false)
	log.Printf("[worker %d] job %s done: %s in %s", workerID, job.ID, result.Verdict, elapsed.Round(time.Millisecond))
	p.deliver(job.ID, result)
}

// deliver saves the result then notifies any blocked WaitForResult caller.
func (p *Pool) deliver(jobID string, result api.Result) {
	// Store before removing from pending to avoid a lookup race.
	p.completedMu.Lock()
	p.completed[jobID] = result
	p.completedMu.Unlock()

	p.mu.Lock()
	ch, ok := p.pending[jobID]
	delete(p.pending, jobID)
	p.mu.Unlock()

	if ok {
		select {
		case ch <- result:
		default:
		}
	}
}