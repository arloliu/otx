package otx

import (
	"context"

	"github.com/arloliu/otx/internal/tracker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// InitTracing sets up the global tracer and namer.
// Called once during application initialization.
func InitTracing(tracer trace.Tracer, namer SpanNamer) {
	tracker.Set(tracer, namer)
}

// Start begins a new span with the configured namer applied.
func Start(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracker.Start(ctx, operation, opts...)
}

// StartServer begins a new server span (e.g., handling an incoming request).
func StartServer(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append([]trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindServer)}, opts...)
	return Start(ctx, operation, opts...)
}

// StartClient begins a new client span (e.g., making an outgoing request).
func StartClient(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append([]trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindClient)}, opts...)
	return Start(ctx, operation, opts...)
}

// StartInternal begins a new internal span (e.g., local processing).
func StartInternal(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append([]trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindInternal)}, opts...)
	return Start(ctx, operation, opts...)
}

// StartProducer begins a new producer span (e.g., publishing a message to Kafka/NATS).
func StartProducer(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append([]trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindProducer)}, opts...)
	return Start(ctx, operation, opts...)
}

// StartConsumer begins a new consumer span (e.g., processing a message from a queue).
func StartConsumer(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append([]trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindConsumer)}, opts...)
	return Start(ctx, operation, opts...)
}

// Span returns the current span from context.
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
