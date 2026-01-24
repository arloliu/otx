package nats

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// headerCarrier adapts nats.Header to propagation.TextMapCarrier.
// This enables trace context propagation through NATS message headers.
type headerCarrier nats.Header

// Get returns the value for the given key from the NATS headers.
// Returns empty string if the key doesn't exist.
func (c headerCarrier) Get(key string) string {
	vals := nats.Header(c).Values(key)
	if len(vals) > 0 {
		return vals[0]
	}

	return ""
}

// Set stores the key-value pair in the NATS headers.
func (c headerCarrier) Set(key, value string) {
	nats.Header(c).Set(key, value)
}

// Keys returns all keys in the NATS headers.
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}

	return keys
}

// InjectNATS injects trace context into NATS message headers.
// If msg.Header is nil, it will be initialized to prevent panics.
// Uses the globally registered TextMapPropagator.
func InjectNATS(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	otel.GetTextMapPropagator().Inject(ctx, headerCarrier(msg.Header))
}

// InjectNATSWithPropagator injects trace context using a specific propagator.
// If msg.Header is nil, it will be initialized to prevent panics.
func InjectNATSWithPropagator(ctx context.Context, msg *nats.Msg, prop propagation.TextMapPropagator) {
	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	prop.Inject(ctx, headerCarrier(msg.Header))
}

// ExtractNATS extracts trace context from NATS message headers.
// Returns a new context containing the extracted trace information.
// Uses the globally registered TextMapPropagator.
func ExtractNATS(ctx context.Context, header nats.Header) context.Context {
	if header == nil {
		return ctx
	}

	return otel.GetTextMapPropagator().Extract(ctx, headerCarrier(header))
}

// ExtractNATSWithPropagator extracts trace context using a specific propagator.
// Returns a new context containing the extracted trace information.
func ExtractNATSWithPropagator(
	ctx context.Context,
	header nats.Header,
	prop propagation.TextMapPropagator,
) context.Context {
	if header == nil {
		return ctx
	}

	return prop.Extract(ctx, headerCarrier(header))
}
