package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gatewayMetrics holds all Prometheus metrics for the gateway.
// Label set is exactly {channel_type, direction, outcome} — no account_id,
// no conversation_id (cardinality discipline per arch-doc §10).
type gatewayMetrics struct {
	inboundTotal    *prometheus.CounterVec
	inboundLatency  *prometheus.HistogramVec
	dedupTotal      *prometheus.CounterVec
	unroutableTotal *prometheus.CounterVec
}

func newGatewayMetrics(reg prometheus.Registerer) *gatewayMetrics {
	factory := promauto.With(reg)
	return &gatewayMetrics{
		inboundTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_gateway_inbound_total",
			Help: "Total inbound webhook requests by channel_type, direction, outcome.",
		}, []string{"channel_type", "direction", "outcome"}),

		inboundLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mio_gateway_inbound_latency_seconds",
			Help:    "Inbound handler latency from request receipt to 200 response.",
			Buckets: prometheus.DefBuckets,
		}, []string{"channel_type", "direction", "outcome"}),

		dedupTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_idempotency_dedup_total",
			Help: "Total duplicate messages suppressed by (account_id, source_message_id) check.",
		}, []string{"channel_type"}),

		unroutableTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_gateway_unroutable_webhook_total",
			Help: "Webhooks soft-dropped because no account matched and no env identity was set.",
		}, []string{"channel_type"}),
	}
}

func (m *gatewayMetrics) incInbound(channelType, direction, outcome string) {
	m.inboundTotal.WithLabelValues(channelType, direction, outcome).Inc()
}

func (m *gatewayMetrics) observeLatency(channelType, direction, outcome string, secs float64) {
	m.inboundLatency.WithLabelValues(channelType, direction, outcome).Observe(secs)
}

func (m *gatewayMetrics) incDedup(channelType string) {
	m.dedupTotal.WithLabelValues(channelType).Inc()
}

func (m *gatewayMetrics) incUnroutable(channelType string) {
	m.unroutableTotal.WithLabelValues(channelType).Inc()
}
