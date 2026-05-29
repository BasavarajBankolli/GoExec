package ws

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/BasavarajBankolli/goexec/api"
	"github.com/BasavarajBankolli/goexec/internal/worker"
)

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 5 * time.Second,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

type Hub struct {
	pool *worker.Pool
}

func New(pool *worker.Pool) *Hub {
	return &Hub{pool: pool}
}

// ServeWS handles GET /ws/jobs/{id} — upgrades to WebSocket and streams output.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	if jobID == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed for job %s: %v", jobID, err)
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	result, err := h.pool.WaitForResult(ctx, jobID)
	if err != nil {
		writeEvent(conn, api.WSEvent{Type: "error", Payload: err.Error()})
		return
	}

	for _, line := range splitLines(result.Stdout) {
		writeEvent(conn, api.WSEvent{Type: "stdout", Payload: line})
	}
	for _, line := range splitLines(result.Stderr) {
		writeEvent(conn, api.WSEvent{Type: "stderr", Payload: line})
	}
	writeEvent(conn, api.WSEvent{Type: "verdict", Result: &result})
}

func writeEvent(conn *websocket.Conn, ev api.WSEvent) {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(ev); err != nil {
		log.Printf("[ws] write error: %v", err)
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i, ch := range s {
		if ch == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
