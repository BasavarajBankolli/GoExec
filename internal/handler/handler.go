package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/BasavarajBankolli/goexec/api"
	"github.com/BasavarajBankolli/goexec/config"
	"github.com/BasavarajBankolli/goexec/internal/cache"
	"github.com/BasavarajBankolli/goexec/internal/metrics"
	"github.com/BasavarajBankolli/goexec/internal/worker"
)

type Handler struct {
	cfg     *config.Config
	pool    *worker.Pool
	cache   *cache.Cache
	metrics *metrics.Collector
}

func New(cfg *config.Config, pool *worker.Pool, c *cache.Cache, m *metrics.Collector) *Handler {
	return &Handler{cfg: cfg, pool: pool, cache: c, metrics: m}
}

// SubmitJob handles POST /jobs.
func (h *Handler) SubmitJob(w http.ResponseWriter, r *http.Request) {
	var req api.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	jobID := newID()
	job := api.Job{
		ID:       jobID,
		Request:  req,
		ResultCh: make(chan api.Result, 1),
	}

	if err := h.pool.SubmitJob(job); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	log.Printf("[api] submitted job %s (lang=%s)", jobID, req.Language)
	writeJSON(w, http.StatusAccepted, api.SubmitResponse{
		JobID:   jobID,
		Message: fmt.Sprintf("job enqueued; connect to /jobs/%s for live output", jobID),
	})
}

// GetJob handles GET /jobs/{id} — blocks up to 30 s waiting for the result.
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := h.pool.WaitForResult(ctx, jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found or timed out: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// AdminMetrics handles GET /admin/metrics (bearer-token protected).
func (h *Handler) AdminMetrics(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token != h.cfg.MetricsToken {
		writeError(w, http.StatusUnauthorized, "invalid or missing metrics token")
		return
	}

	snap := h.metrics.Snapshot()
	out := struct {
		metrics.Snapshot
		ActiveWorkers int64 `json:"active_workers"`
		QueueLen      int   `json:"queue_len"`
		CacheSize     int   `json:"cache_size"`
	}{
		Snapshot:      snap,
		ActiveWorkers: h.pool.ActiveWorkers(),
		QueueLen:      h.pool.QueueLen(),
		CacheSize:     h.cache.Size(),
	}
	writeJSON(w, http.StatusOK, out)
}

// Health handles GET /health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func validateRequest(req api.SubmitRequest) error {
	switch req.Language {
	case api.LangGo, api.LangPython, api.LangCpp, api.LangJava:
	default:
		return fmt.Errorf("unsupported language %q; choose from: go, python, cpp, java", req.Language)
	}
	if strings.TrimSpace(req.Code) == "" {
		return fmt.Errorf("code must not be empty")
	}
	if len(req.Code) > 64*1024 {
		return fmt.Errorf("code exceeds 64 KiB limit")
	}
	return nil
}

// newID returns a random 32-char hex string using only stdlib (no uuid dep).
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}
