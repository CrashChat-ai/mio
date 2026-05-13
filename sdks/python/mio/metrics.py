"""Prometheus metrics for MIO Python SDK.

LABEL DISCIPLINE (enforced here and by CI grep):
  DO NOT add account_id, tenant_id, conversation_id, message_id as labels.
  Only channel_type + direction + outcome on counters.
  Only channel_type + direction on histograms.

Bucket values match sdk-go/metrics.go exactly for cross-language consistency.

Outcome values:
  success, error, dedup, timeout, invalid
"""

from prometheus_client import Counter, Histogram, CollectorRegistry, REGISTRY

# Histogram buckets — must match sdk-go/metrics.go exactly.
HISTOGRAM_BUCKETS = (0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1.0)

# Outcome label constants — use these, never raw strings.
OUTCOME_SUCCESS = "success"
OUTCOME_ERROR = "error"
OUTCOME_DEDUP = "dedup"
OUTCOME_TIMEOUT = "timeout"
OUTCOME_INVALID = "invalid"


class Metrics:
    """Holds all Prometheus collectors for the MIO SDK.

    All counters: labels channel_type, direction, outcome.
    All histograms: labels channel_type, direction.
    """

    def __init__(self, registry: CollectorRegistry | None = None) -> None:
        reg = registry or REGISTRY

        self._publish_total = Counter(
            "mio_sdk_publish_total",
            "Total publish operations by channel_type, direction, and outcome.",
            labelnames=["channel_type", "direction", "outcome"],
            registry=reg,
        )
        self._consume_total = Counter(
            "mio_sdk_consume_total",
            "Total consume operations by channel_type, direction, and outcome.",
            labelnames=["channel_type", "direction", "outcome"],
            registry=reg,
        )
        self._publish_latency = Histogram(
            "mio_sdk_publish_latency_seconds",
            "Publish latency in seconds by channel_type and direction.",
            labelnames=["channel_type", "direction"],
            buckets=HISTOGRAM_BUCKETS,
            registry=reg,
        )
        self._consume_latency = Histogram(
            "mio_sdk_consume_latency_seconds",
            "Consume processing latency in seconds by channel_type and direction.",
            labelnames=["channel_type", "direction"],
            buckets=HISTOGRAM_BUCKETS,
            registry=reg,
        )

    def inc_publish(self, channel_type: str, direction: str, outcome: str) -> None:
        self._publish_total.labels(
            channel_type=channel_type, direction=direction, outcome=outcome
        ).inc()

    def inc_consume(self, channel_type: str, direction: str, outcome: str) -> None:
        self._consume_total.labels(
            channel_type=channel_type, direction=direction, outcome=outcome
        ).inc()

    def observe_publish(
        self, channel_type: str, direction: str, seconds: float
    ) -> None:
        self._publish_latency.labels(
            channel_type=channel_type, direction=direction
        ).observe(seconds)

    def observe_consume(
        self, channel_type: str, direction: str, seconds: float
    ) -> None:
        self._consume_latency.labels(
            channel_type=channel_type, direction=direction
        ).observe(seconds)
