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

// contribPropagators maps the known-but-unimplemented propagator names to the
// go.opentelemetry.io/contrib/propagators/* package that supplies them. otx does
// not import these packages, so configuring one of these names installs nothing;
// buildPropagator emits a diagnostic via otel.Handle pointing at the package the
// caller must wire up themselves.
var contribPropagators = map[string]string{
	"b3":      "go.opentelemetry.io/contrib/propagators/b3",
	"b3multi": "go.opentelemetry.io/contrib/propagators/b3",
	"jaeger":  "go.opentelemetry.io/contrib/propagators/jaeger",
	"xray":    "go.opentelemetry.io/contrib/propagators/aws/xray",
	"ottrace": "go.opentelemetry.io/contrib/propagators/ot",
}

// BuildPropagator returns the OTel text map propagator derived from cfg — the
// same propagator NewTracerProvider installs as the global propagator. It is
// exported and side-effect-free (it does not mutate any OTel global) so callers
// that export metrics or logs but not traces can install propagation
// independently via otel.SetTextMapPropagator(otx.BuildPropagator(cfg)).
//
// A nil cfg yields the W3C default (tracecontext + baggage). Unknown propagator
// names and the known-but-unimplemented b3/b3multi/jaeger/xray/ottrace names
// emit a diagnostic via otel.Handle and are otherwise ignored.
func BuildPropagator(cfg *PropConfig) propagation.TextMapPropagator {
	return buildPropagator(cfg)
}

// buildPropagator creates a text map propagator based on configuration.
// Supports OTel standard OTEL_PROPAGATORS values: tracecontext, baggage, b3, b3multi, jaeger, xray, ottrace
// Unknown propagator names are reported via otel.Handle and ignored. The
// b3/b3multi/jaeger/xray/ottrace names are recognized but not installed (they
// require contrib packages otx does not import); each configured one emits a
// diagnostic via otel.Handle and is otherwise ignored.
//
//nolint:revive // confusing-naming: BuildPropagator is the intentional exported wrapper.
func buildPropagator(cfg *PropConfig) propagation.TextMapPropagator {
	if cfg == nil {
		cfg = &PropConfig{Propagators: "tracecontext,baggage"}
	}

	var propagators []propagation.TextMapPropagator

	// Check for unknown and known-but-unimplemented propagators and warn.
	for _, name := range splitPropagators(cfg.Propagators) {
		switch {
		case !knownPropagators[name]:
			otel.Handle(errors.New("otx: unknown propagator \"" + name + "\" in OTEL_PROPAGATORS, ignoring"))
		case contribPropagators[name] != "":
			otel.Handle(errors.New("otx: propagator \"" + name + "\" requires " + contribPropagators[name] +
				"; not installed, ignoring"))
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

// InjectHTTP injects propagation fields into HTTP headers using the global text
// map propagator. Exactly what is carried (trace context, baggage, or both)
// depends on the propagators configured via OTEL_PROPAGATORS or
// [BuildPropagator].
func InjectHTTP(ctx context.Context, headers http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(headers))
}

// ExtractHTTP extracts propagation fields from HTTP headers using the global
// text map propagator. Exactly what is extracted depends on the propagators
// configured via OTEL_PROPAGATORS or [BuildPropagator].
func ExtractHTTP(ctx context.Context, headers http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectGRPC injects propagation fields into gRPC metadata using the global
// text map propagator. Exactly what is carried depends on the propagators
// configured via OTEL_PROPAGATORS or [BuildPropagator].
func InjectGRPC(ctx context.Context, md metadata.MD) {
	otel.GetTextMapPropagator().Inject(ctx, metadataCarrier(md))
}

// ExtractGRPC extracts propagation fields from gRPC metadata using the global
// text map propagator. Exactly what is extracted depends on the propagators
// configured via OTEL_PROPAGATORS or [BuildPropagator].
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
