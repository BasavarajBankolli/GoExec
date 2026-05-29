package cache

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/BasavarajBankolli/goexec/api"
)

type entry struct {
	result    api.Result
	expiresAt time.Time
}

type Cache struct {
	mu     sync.RWMutex
	store  map[string]entry
	ttl    time.Duration
	hits   uint64
	misses uint64
}

func New(ttl time.Duration) *Cache {
	c := &Cache{
		store: make(map[string]entry),
		ttl:   ttl,
	}
	go c.janitor()
	return c
}

func (c *Cache) Get(job api.Job) (api.Result, bool) {
	key := cacheKey(job)
	c.mu.RLock()
	e, ok := c.store[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(e.expiresAt) {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return api.Result{}, false
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
	return e.result, true
}

func (c *Cache) Set(job api.Job, result api.Result) {
	key := cacheKey(job)
	c.mu.Lock()
	c.store[key] = entry{result: result, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

func (c *Cache) Stats() (hits, misses uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, e := range c.store {
		if now.Before(e.expiresAt) {
			count++
		}
	}
	return count
}

func (c *Cache) janitor() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, e := range c.store {
			if now.After(e.expiresAt) {
				delete(c.store, k)
			}
		}
		c.mu.Unlock()
	}
}

func cacheKey(job api.Job) string {
	h := sha256.New()
	h.Write([]byte(job.Request.Language))
	h.Write([]byte{0})
	h.Write([]byte(job.Request.Code))
	h.Write([]byte{0})
	h.Write([]byte(job.Request.Stdin))
	return fmt.Sprintf("%x", h.Sum(nil))
}
