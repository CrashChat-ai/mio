package sdk

import (
	"github.com/prometheus/client_golang/prometheus"
)

// histogramBuckets are the fixed latency buckets used for both publish and consume.
// IMPORTANT: These must match sdk-py/mio/metrics.py exactly for cross-language
// metric consistency. Do NOT add account_id/tenant_id/conversation_id/message_id
// as labels — that is a cardinality bomb. Only three labels are permitted:
// channel_type, direction, outcome.
var histogramBuckets = []float64{0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1.0}

// Outcome values for the outcome label. Enumerate here to prevent string drift.
const (
	OutcomeSuccess = "success"
	OutcomeError   = "error"
	OutcomeDedup   = "dedup"
	OutcomeTimeout = "timeout"
	OutcomeInvalid = "invalid"
)

// Metrics holds all Prometheus collectors for the MIO SDK.
// All counters carry labels: channel_type, direction, outcome.
// All histograms carry labels: channel_type, direction.
//
// LABEL DISCIPLINE (enforced here and by CI grep):
//   - DO NOT add account_id, tenant_id, conversation_id, message_id as labels.
//   - Only channel_type + direction + outcome on counters.
//   - Only channel_type + direction on histograms.
type Metrics struct {
	publishTotal   *prometheus.CounterVec
	consumeTotal   *prometheus.CounterVec
	publishLatency *prometheus.HistogramVec
	consumeLatency *prometheus.HistogramVec
}

// newMetrics creates and registers Prometheus metrics on the provided registerer.
func newMetrics(reg prometheus.Registerer) (*Metrics, error) {
	publishTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mio_sdk_publish_total",
			Help: "Total publish operations by channel_type, direction, and outcome.",
		},
		[]string{"channel_type", "direction", "outcome"},
	)
	consumeTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mio_sdk_consume_total",
			Help: "Total consume operations by channel_type, direction, and outcome.",
		},
		[]string{"channel_type", "direction", "outcome"},
	)
	publishLatency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mio_sdk_publish_latency_seconds",
			Help:    "Publish latency in seconds by channel_type and direction.",
			Buckets: histogramBuckets,
		},
		[]string{"channel_type", "direction"},
	)
	consumeLatency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mio_sdk_consume_latency_seconds",
			Help:    "Consume processing latency in seconds by channel_type and direction.",
			Buckets: histogramBuckets,
		},
		[]string{"channel_type", "direction"},
	)

	for _, c := range []prometheus.Collector{publishTotal, consumeTotal, publishLatency, consumeLatency} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}

	return &Metrics{
		publishTotal:   publishTotal,
		consumeTotal:   consumeTotal,
		publishLatency: publishLatency,
		consumeLatency: consumeLatency,
	}, nil
}

func (m *Metrics) incPublish(channelType, direction, outcome string) {
	m.publishTotal.WithLabelValues(channelType, direction, outcome).Inc()
}

func (m *Metrics) incConsume(channelType, direction, outcome string) {
	m.consumeTotal.WithLabelValues(channelType, direction, outcome).Inc()
}

func (m *Metrics) observePublish(channelType, direction string, seconds float64) {
	m.publishLatency.WithLabelValues(channelType, direction).Observe(seconds)
}

func (m *Metrics) observeConsume(channelType, direction string, seconds float64) {
	m.consumeLatency.WithLabelValues(channelType, direction).Observe(seconds)
}
