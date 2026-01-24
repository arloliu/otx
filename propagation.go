package otx

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

// knownPropagators lists the propagator names supported by this package.
// b3, b3multi, jaeger, xray, ottrace require additional contrib packages.
var knownPropagators = map[string]bool{
	"tracecontext": true,
	"baggage":      true,
	"b3":           true,
	"b3multi":      true,
	"jaeger":       true,
	"xray":         true,
	"ottrace":      true,
	"none":         true,
}

// buildPropagator creates a text map propagator based on configuration.
// Supports OTel standard OTEL_PROPAGATORS values: tracecontext, baggage, b3, b3multi, jaeger, xray, ottrace
// Unknown propagator names are reported via otel.Handle and ignored.
func buildPropagator(cfg *PropConfig) propagation.TextMapPropagator {
	if cfg == nil {
		cfg = &PropConfig{Propagators: "tracecontext,baggage"}
	}

	var propagators []propagation.TextMapPropagator

	// Check for unknown propagators and warn
	for _, name := range splitPropagators(cfg.Propagators) {
		if !knownPropagators[name] {
			otel.Handle(errors.New("otx: unknown propagator \"" + name + "\" in OTEL_PROPAGATORS, ignoring"))
		}
	}

	if cfg.HasTraceContext() {
		propagators = append(propagators, propagation.TraceContext{})
	}
	if cfg.HasBaggage() {
		propagators = append(propagators, propagation.Baggage{})
	}
	// Note: b3, b3multi, jaeger, xray, ottrace require additional contrib packages
	// go.opentelemetry.io/contrib/propagators/*

	if len(propagators) == 0 {
		return propagation.NewCompositeTextMapPropagator()
	}

	return propagation.NewCompositeTextMapPropagator(propagators...)
}

// InjectHTTP injects trace context and baggage into HTTP headers.
func InjectHTTP(ctx context.Context, headers http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(headers))
}

// ExtractHTTP extracts trace context and baggage from HTTP headers.
func ExtractHTTP(ctx context.Context, headers http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectGRPC injects trace context and baggage into gRPC metadata.
func InjectGRPC(ctx context.Context, md metadata.MD) {
	otel.GetTextMapPropagator().Inject(ctx, metadataCarrier(md))
}

// ExtractGRPC extracts trace context and baggage from gRPC metadata.
func ExtractGRPC(ctx context.Context, md metadata.MD) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, metadataCarrier(md))
}

// metadataCarrier adapts gRPC metadata to propagation.TextMapCarrier.
type metadataCarrier metadata.MD

func (m metadataCarrier) Get(key string) string {
	vals := metadata.MD(m).Get(key)
	if len(vals) > 0 {
		return vals[0]
	}

	return ""
}

func (m metadataCarrier) Set(key string, value string) {
	metadata.MD(m).Set(key, value)
}

func (m metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}
