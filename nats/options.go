package nats

import (
	"github.com/arloliu/otx/internal/tracker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "otx/nats"

// options holds configuration for tracing wrappers.
type options struct {
	tracerName   string
	prop         propagation.TextMapPropagator
	processSpans bool   // Enable per-message process spans
	asyncSpans   bool   // Enable spans for async publish operations
	stream       string // Override stream name for spans
}

// defaultOptions returns the default configuration.
func defaultOptions() options {
	return options{
		tracerName:   instrumentationName,
		prop:         nil, // Will use global propagator
		processSpans: true,
		asyncSpans:   true,
	}
}

// Option configures tracing behavior.
type Option func(*options)

// WithTracerName sets a custom tracer name.
// Default is the package import path.
func WithTracerName(name string) Option {
	return func(o *options) {
		o.tracerName = name
	}
}

// WithPropagator sets a custom propagator for context injection/extraction.
// If not set, the global propagator is used.
func WithPropagator(prop propagation.TextMapPropagator) Option {
	return func(o *options) {
		o.prop = prop
	}
}

// WithProcessSpans enables or disables individual message processing spans.
// When disabled, only receive spans are created for batch operations.
// Default is true.
func WithProcessSpans(enabled bool) Option {
	return func(o *options) {
		o.processSpans = enabled
	}
}

// WithAsyncSpans enables or disables spans and header injection for PublishAsync operations.
// When disabled, PublishAsync calls create no spans and do not inject trace headers.
// Default is true.
func WithAsyncSpans(enabled bool) Option {
	return func(o *options) {
		o.asyncSpans = enabled
	}
}

// WithStream sets an explicit stream name for span naming and attributes.
// Use this when the stream name cannot be determined from message metadata,
// or to override the auto-detected stream name.
func WithStream(stream string) Option {
	return func(o *options) {
		o.stream = stream
	}
}

// applyOptions applies option functions to the default options.
func applyOptions(opts []Option) options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// getTracer returns a tracer from the provider with the configured name.
func getTracer(tp trace.TracerProvider, opts options) trace.Tracer {
	if opts.tracerName != instrumentationName {
		if tp == nil {
			tp = otel.GetTracerProvider()
		}

		return tp.Tracer(opts.tracerName)
	}

	// Use global tracer if configured
	if t := tracker.Tracer(); t != nil {
		return t
	}

	// Fallback to default tracer if no provider is provided
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	// Use tracer with instrumentation name
	return tp.Tracer(opts.tracerName)
}

// getPropagator returns the configured or global propagator.
func getPropagator(opts options) propagation.TextMapPropagator {
	if opts.prop != nil {
		return opts.prop
	}

	return otel.GetTextMapPropagator()
}
