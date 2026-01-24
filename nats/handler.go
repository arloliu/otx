package nats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// MessageHandlerWithTracing wraps a handler function to add process spans.
// The returned jetstream.MessageHandler extracts trace context from headers,
// creates a process span with proper OTel messaging attributes, then calls your handler.
//
// The stream name is automatically extracted from message metadata. Use WithStream
// to override if needed.
//
// Example:
//
//	consumer.Consume(nats.MessageHandlerWithTracing(func(msg *nats.TracedMsg) {
//	    ctx := msg.Context()  // Contains span context
//	    processOrder(ctx, msg.Data())
//	    msg.Ack()
//	}))
func MessageHandlerWithTracing(
	handler func(*TracedMsg),
	opts ...Option,
) jetstream.MessageHandler {
	return MessageHandlerWithTracingProviders(handler, nil, nil, opts...)
}

// MessageHandlerWithTracingProviders wraps a handler with explicit providers.
// If tp is nil, the global TracerProvider is used.
// If prop is nil, the global TextMapPropagator is used.
//
// Panics if handler is nil.
func MessageHandlerWithTracingProviders(
	handler func(*TracedMsg),
	tp trace.TracerProvider,
	prop propagation.TextMapPropagator,
	opts ...Option,
) jetstream.MessageHandler {
	if handler == nil {
		panic("otx/nats: handler must not be nil")
	}
	o := applyOptions(opts)

	if prop != nil {
		o.prop = prop
	}

	tracer := getTracer(tp, o)
	propagator := getPropagator(o)

	return func(msg jetstream.Msg) {
		// Extract trace context from message headers
		parentCtx := context.Background()
		if headers := msg.Headers(); headers != nil {
			parentCtx = propagator.Extract(parentCtx, headerCarrier(headers))
		}

		// Extract message metadata for span attributes
		stream := ""
		consumerName := ""
		subject := ""

		if metadata, err := msg.Metadata(); err == nil && metadata != nil {
			stream = metadata.Stream
			consumerName = metadata.Consumer
		}

		if msg.Subject() != "" {
			subject = msg.Subject()
		}

		// Allow stream override via option
		if o.stream != "" {
			stream = o.stream
		}

		// Start process span
		spanName := opTypeProcess + " " + stream
		spanCtx, span := tracer.Start(parentCtx, spanName,
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(processAttributes(stream, consumerName, subject, "", len(msg.Data()))...),
		)

		// Create traced message with span context
		tracedMsg := &TracedMsg{
			Msg: msg,
			ctx: spanCtx,
		}

		// Call handler with deferred span end and panic recovery
		defer func() {
			if r := recover(); r != nil {
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, "panic in handler")
				span.End()
				panic(r) // Re-panic after recording
			}
			span.End()
		}()

		handler(tracedMsg)
	}
}

// Ensure global OTel API is available for default usage.
var _ = otel.GetTracerProvider
