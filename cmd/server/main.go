package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"github.com/BasavarajBankolli/goexec/config"
	"github.com/BasavarajBankolli/goexec/internal/cache"
	"github.com/BasavarajBankolli/goexec/internal/executor"
	"github.com/BasavarajBankolli/goexec/internal/handler"
	"github.com/BasavarajBankolli/goexec/internal/metrics"
	"github.com/BasavarajBankolli/goexec/internal/worker"
	"github.com/BasavarajBankolli/goexec/internal/ws"
)

func main() {
	cfg := config.Load()

	exec, err := executor.New()
	if err != nil {
		log.Fatalf("docker connect failed: %v\nMake sure Docker Desktop is running.", err)
	}

	m := metrics.New()
	c := cache.New(cfg.CacheTTL)
	pool := worker.New(cfg.WorkerCount, cfg.JobQueueSize, exec, c, m)
	h := handler.New(cfg, pool, c, m)
	hub := ws.New(pool)

	r := mux.NewRouter()
	r.HandleFunc("/health", h.Health).Methods(http.MethodGet)
	r.HandleFunc("/jobs", h.SubmitJob).Methods(http.MethodPost)
	r.HandleFunc("/jobs/{id}", h.GetJob).Methods(http.MethodGet)
	r.HandleFunc("/admin/metrics", h.AdminMetrics).Methods(http.MethodGet)
	r.HandleFunc("/ws/jobs/{id}", hub.ServeWS)

	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 65 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("GoExec listening on http://0.0.0.0:%s", cfg.Port)
		log.Printf("  Workers : %d", cfg.WorkerCount)
		log.Printf("  Queue   : %d", cfg.JobQueueSize)
		log.Printf("  Timeout : %s", cfg.DefaultTimeout)
		log.Printf("  Cache   : TTL=%s", cfg.CacheTTL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("forced shutdown: %v", err)
	}
	pool.Shutdown()
	log.Println("GoExec stopped cleanly")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start).Round(time.Millisecond))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
