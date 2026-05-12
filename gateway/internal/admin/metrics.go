package admin

import (
	"github.com/prometheus/client_golang/prometheus"
)

// AdminMetrics groups the Prometheus instruments exposed by the admin
// server. Constructed once at boot and shared across the Connect interceptor
// + handler bodies. The label set is bounded — no tenant_id / account_id
// labels (would explode cardinality).
type AdminMetrics struct {
	RPCTotal      *prometheus.CounterVec   // {method, outcome}
	RPCDuration   *prometheus.HistogramVec // {method}
	OAuthTotal    *prometheus.CounterVec   // {adapter, outcome}
	TailActive    prometheus.Gauge         // current open TailMessages streams
}

// NewAdminMetrics registers the instruments against r. Idempotent within a
// single process via MustRegister (will panic on double-registration; the
// admin boot only constructs this once).
func NewAdminMetrics(r prometheus.Registerer) *AdminMetrics {
	m := &AdminMetrics{
		RPCTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_admin_rpc_total",
			Help: "Admin RPC count by method and outcome (ok|error).",
		}, []string{"method", "outcome"}),
		RPCDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mio_admin_rpc_duration_seconds",
			Help:    "Admin RPC handler latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),
		OAuthTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_admin_oauth_total",
			Help: "OAuth-dance events by adapter and outcome (started|callback|completed|failed).",
		}, []string{"adapter", "outcome"}),
		TailActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "mio_admin_tail_active_streams",
			Help: "Currently-open TailMessages server-streams.",
		}),
	}
	if r != nil {
		r.MustRegister(m.RPCTotal, m.RPCDuration, m.OAuthTotal, m.TailActive)
	}
	return m
}
