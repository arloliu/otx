package otx

import (
	"context"

	"github.com/arloliu/otx/internal/tracker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Cached WithSpanKind options reused by the StartServer/Client/Internal/
// Producer/Consumer helpers so the common zero-extra-opts call avoids
// re-boxing the option on every span start.
var (
	optSpanKindServer   = trace.WithSpanKind(trace.SpanKindServer)
	optSpanKindClient   = trace.WithSpanKind(trace.SpanKindClient)
	optSpanKindInternal = trace.WithSpanKind(trace.SpanKindInternal)
	optSpanKindProducer = trace.WithSpanKind(trace.SpanKindProducer)
	optSpanKindConsumer = trace.WithSpanKind(trace.SpanKindConsumer)
)

// InitTracing sets up the global tracer and namer. It is typically called once
// during application initialization, but is safe to call multiple times: each
// call atomically replaces the previous tracer and namer (latest wins).
//
// A nil namer is allowed and selects the default namer, which returns operation
// names unchanged (see [DefaultNamer]).
func InitTracing(tracer trace.Tracer, namer SpanNamer) {
	tracker.Set(tracer, namer)
}

// Start begins a new span with the configured namer applied.
func Start(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracker.Start(ctx, operation, opts...)
}

// StartServer begins a new server span (e.g., handling an incoming request).
func StartServer(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if len(opts) == 0 {
		return Start(ctx, operation, optSpanKindServer)
	}

	opts = append([]trace.SpanStartOption{optSpanKindServer}, opts...)

	return Start(ctx, operation, opts...)
}

// StartClient begins a new client span (e.g., making an outgoing request).
func StartClient(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if len(opts) == 0 {
		return Start(ctx, operation, optSpanKindClient)
	}

	opts = append([]trace.SpanStartOption{optSpanKindClient}, opts...)

	return Start(ctx, operation, opts...)
}

// StartInternal begins a new internal span (e.g., local processing).
func StartInternal(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if len(opts) == 0 {
		return Start(ctx, operation, optSpanKindInternal)
	}

	opts = append([]trace.SpanStartOption{optSpanKindInternal}, opts...)

	return Start(ctx, operation, opts...)
}

// StartProducer begins a new producer span (e.g., publishing a message to Kafka/NATS).
func StartProducer(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if len(opts) == 0 {
		return Start(ctx, operation, optSpanKindProducer)
	}

	opts = append([]trace.SpanStartOption{optSpanKindProducer}, opts...)

	return Start(ctx, operation, opts...)
}

// StartConsumer begins a new consumer span (e.g., processing a message from a queue).
func StartConsumer(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if len(opts) == 0 {
		return Start(ctx, operation, optSpanKindConsumer)
	}

	opts = append([]trace.SpanStartOption{optSpanKindConsumer}, opts...)

	return Start(ctx, operation, opts...)
}

// Span returns the current span from context.
//
// Deprecated: use SpanFromContext instead.
func Span(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// TraceID returns the trace ID from context, or empty string if none.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}

	return ""
}

// SpanID returns the span ID from context, or empty string if none.
func SpanID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasSpanID() {
		return sc.SpanID().String()
	}

	return ""
}

// SpanFromContext retrieves the current span from the context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// RecordError records an error on the current span and sets status.
// If err is nil, this is a no-op.
func RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, opts...)
	span.SetStatus(codes.Error, err.Error())
}

// SetSuccess marks the current span as successful.
func SetSuccess(ctx context.Context) {
	trace.SpanFromContext(ctx).SetStatus(codes.Ok, "")
}

// AddEvent adds an event to the current span.
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets attributes on the current span.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}
