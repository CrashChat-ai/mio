package sdk

import (
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"context"
)

const tracerName = "github.com/crashchat-ai/mio/sdk-go"

// natsHeaderCarrier adapts nats.Header to the OTel TextMapCarrier interface.
// W3C traceparent is ASCII-safe and passes through NATS headers unchanged.
type natsHeaderCarrier struct {
	h nats.Header
}

func (c natsHeaderCarrier) Get(key string) string {
	return c.h.Get(key)
}

func (c natsHeaderCarrier) Set(key, val string) {
	c.h.Set(key, val)
}

func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.h))
	for k := range c.h {
		keys = append(keys, k)
	}
	return keys
}

// injectTrace starts a PRODUCER span and injects W3C traceparent into msg headers.
// The returned context carries the active span for the caller to end.
func injectTrace(ctx context.Context, tp trace.TracerProvider, subject, msgID string, header nats.Header) (context.Context, trace.Span) {
	tracer := tp.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "mio.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			attribute.String("messaging.destination", subject),
			attribute.String("messaging.message_id", msgID),
		),
	)
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier{h: header})
	return ctx, span
}

// extractTrace extracts W3C traceparent from msg headers and starts a CONSUMER span.
// The returned context carries the active span for the caller to end.
func extractTrace(ctx context.Context, tp trace.TracerProvider, subject string, header nats.Header) (context.Context, trace.Span) {
	prop := otel.GetTextMapPropagator()
	parentCtx := prop.Extract(ctx, natsHeaderCarrier{h: header})

	tracer := tp.Tracer(tracerName)
	ctx, span := tracer.Start(parentCtx, "mio.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			attribute.String("messaging.destination", subject),
		),
	)
	return ctx, span
}

// endSpanOnError records err on span and sets status; always ends the span.
func endSpanOnError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// noopTracerProvider returns the global tracer provider, falling back to noop
// if none has been registered. Used when the caller omits WithTracerProvider.
func noopTracerProvider() trace.TracerProvider {
	return otel.GetTracerProvider()
}

// propagatorFromContext uses the globally registered TextMapPropagator.
// Callers that need W3C tracecontext must register it at startup:
//
//	otel.SetTextMapPropagator(propagation.TraceContext{})
func propagatorFromContext() propagation.TextMapPropagator {
	return otel.GetTextMapPropagator()
}
