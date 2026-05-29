package cache_test

import (
	"testing"
	"time"

	"github.com/BasavarajBankolli/goexec/api"
	"github.com/BasavarajBankolli/goexec/internal/cache"
)

func makeJob(lang api.Language, code string) api.Job {
	return api.Job{
		ID:       "test",
		Request:  api.SubmitRequest{Language: lang, Code: code},
		ResultCh: make(chan api.Result, 1),
	}
}

func TestCacheHitAndMiss(t *testing.T) {
	c := cache.New(5 * time.Minute)
	job := makeJob(api.LangGo, `package main; func main() {}`)
	result := api.Result{JobID: "test", Verdict: api.VerdictAccepted, Stdout: "hello"}

	if _, ok := c.Get(job); ok {
		t.Fatal("expected miss before Set")
	}
	c.Set(job, result)
	got, ok := c.Get(job)
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if got.Verdict != result.Verdict {
		t.Errorf("verdict: got %q want %q", got.Verdict, result.Verdict)
	}
}

func TestCacheKeyIsolation(t *testing.T) {
	c := cache.New(5 * time.Minute)
	jobA := makeJob(api.LangGo, "code A")
	jobB := makeJob(api.LangGo, "code B")
	jobC := makeJob(api.LangPython, "code A")
	c.Set(jobA, api.Result{Verdict: api.VerdictAccepted})
	if _, ok := c.Get(jobB); ok {
		t.Error("different code should miss")
	}
	if _, ok := c.Get(jobC); ok {
		t.Error("different language should miss")
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	c := cache.New(50 * time.Millisecond)
	job := makeJob(api.LangPython, "print('hi')")
	c.Set(job, api.Result{Verdict: api.VerdictAccepted})
	if _, ok := c.Get(job); !ok {
		t.Fatal("expected hit immediately after Set")
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok := c.Get(job); ok {
		t.Error("expected miss after TTL expiry")
	}
}
