"""W3C traceparent propagation over NATS headers for MIO Python SDK.

Uses opentelemetry.propagators.tracecontext.TraceContextTextMapPropagator.
Carrier adapter wraps a dict[str, str] (NATS message headers).

Callers that need W3C tracecontext must register the propagator at startup:
    from opentelemetry.propagators.tracecontext import TraceContextTextMapPropagator
    from opentelemetry import propagate
    propagate.set_global_textmap(TraceContextTextMapPropagator())
"""

from __future__ import annotations

from opentelemetry import trace, context
from opentelemetry.trace import SpanKind, Span, TracerProvider
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

_TRACER_NAME = "github.com/crashchat-ai/mio/sdk-py"

# Singleton propagator — uses global propagator by default.
_PROPAGATOR = TraceContextTextMapPropagator()


class _DictCarrier:
    """OTel TextMapCarrier adapter over a plain dict (NATS headers)."""

    def __init__(self, headers: dict[str, str]) -> None:
        self._headers = headers

    def get(self, key: str, default: str | None = None) -> str | None:
        # OTel >= 1.30 DefaultGetter calls carrier.get(key, default).
        # Accept the optional default to stay compatible with both old and new OTel.
        return self._headers.get(key, default)

    def set(self, key: str, value: str) -> None:
        self._headers[key] = value

    def keys(self) -> list[str]:
        return list(self._headers.keys())


def inject_trace(
    headers: dict[str, str],
    subject: str,
    msg_id: str,
    tracer_provider: TracerProvider | None = None,
) -> tuple[context.Context, Span]:
    """Start a PRODUCER span and inject W3C traceparent into headers.

    Returns (ctx, span). Caller must end the span when publish completes.
    """
    tp = tracer_provider or trace.get_tracer_provider()
    tracer = tp.get_tracer(_TRACER_NAME)

    ctx = context.get_current()
    span = tracer.start_span(
        "mio.publish",
        kind=SpanKind.PRODUCER,
        context=ctx,
        attributes={
            "messaging.system": "nats",
            "messaging.destination": subject,
            "messaging.message_id": msg_id,
        },
    )
    # OTel 1.x API: set_span_in_context replaces the deprecated use_span().
    ctx_with_span = trace.set_span_in_context(span, ctx)

    carrier = _DictCarrier(headers)
    _PROPAGATOR.inject(carrier, context=ctx_with_span)

    return ctx_with_span, span


def extract_trace(
    headers: dict[str, str],
    subject: str,
    tracer_provider: TracerProvider | None = None,
) -> tuple[context.Context, Span]:
    """Extract W3C traceparent from headers and start a CONSUMER span.

    Returns (ctx, span). Caller must end the span when message processing completes.
    """
    carrier = _DictCarrier(headers)
    parent_ctx = _PROPAGATOR.extract(carrier)

    tp = tracer_provider or trace.get_tracer_provider()
    tracer = tp.get_tracer(_TRACER_NAME)

    span = tracer.start_span(
        "mio.consume",
        context=parent_ctx,
        kind=SpanKind.CONSUMER,
        attributes={
            "messaging.system": "nats",
            "messaging.destination": subject,
        },
    )
    ctx = trace.set_span_in_context(span, parent_ctx)
    return ctx, span
