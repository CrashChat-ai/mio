// Package health implements /healthz and /readyz probe handlers.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

// Handlers holds dependencies for health probe endpoints.
type Handlers struct {
	pg   *pgxpool.Pool
	nc   *nats.Conn
}

// New creates Handlers with the given Postgres pool and NATS connection.
func New(pg *pgxpool.Pool, nc *nats.Conn) *Handlers {
	return &Handlers{pg: pg, nc: nc}
}

// Healthz handles GET /healthz — liveness probe.
// No dependencies; returns 200 as long as the process is alive.
func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Readyz handles GET /readyz — readiness probe.
// Pings Postgres AND flushes NATS with a 2s timeout.
// Returns 503 if either dependency is unreachable.
func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pg.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"error": "postgres unreachable: " + err.Error()})
		return
	}

	if err := h.nc.FlushTimeout(2 * time.Second); err != nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"error": "nats unreachable: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
