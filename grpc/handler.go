package grpc

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
)

// ServerHandler returns a gRPC stats.Handler for server-side tracing and metrics.
//
// This handler uses the globally registered TracerProvider, MeterProvider, and
// TextMapPropagator. When using this with the otx package, ensure that
// global providers have been initialized.
//
// For explicit provider injection, use [ServerHandlerWithProviders] instead.
func ServerHandler(opts ...otelgrpc.Option) stats.Handler {
	return otelgrpc.NewServerHandler(opts...)
}

// ServerHandlerWithProviders returns a gRPC stats.Handler for server-side
// tracing and metrics with explicitly provided TracerProvider, MeterProvider,
// and TextMapPropagator.
//
// This is useful when:
//   - You need to use a different provider than the global one
//   - You want explicit dependency injection for testing
//   - You have multiple gRPC servers with different telemetry configurations
//
// If any provider is nil, the corresponding global provider will be used as fallback.
func ServerHandlerWithProviders(
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
	opts ...otelgrpc.Option,
) stats.Handler {
	allOpts := buildProviderOptions(tp, mp, prop)
	allOpts = append(allOpts, opts...)

	return otelgrpc.NewServerHandler(allOpts...)
}

// ClientHandler returns a gRPC stats.Handler for client-side tracing and metrics.
//
// This handler uses the globally registered TracerProvider, MeterProvider, and
// TextMapPropagator. When using this with the otx package, ensure that
// global providers have been initialized.
//
// For explicit provider injection, use [ClientHandlerWithProviders] instead.
func ClientHandler(opts ...otelgrpc.Option) stats.Handler {
	return otelgrpc.NewClientHandler(opts...)
}

// ClientHandlerWithProviders returns a gRPC stats.Handler for client-side
// tracing and metrics with explicitly provided TracerProvider, MeterProvider,
// and TextMapPropagator.
//
// This is useful when:
//   - You need to use a different provider than the global one
//   - You want explicit dependency injection for testing
//   - You have multiple gRPC clients with different telemetry configurations
//
// If any provider is nil, the corresponding global provider will be used as fallback.
func ClientHandlerWithProviders(
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
	opts ...otelgrpc.Option,
) stats.Handler {
	allOpts := buildProviderOptions(tp, mp, prop)
	allOpts = append(allOpts, opts...)

	return otelgrpc.NewClientHandler(allOpts...)
}

// buildProviderOptions creates otelgrpc.Option slice from providers.
// Falls back to global providers when explicit providers are nil.
func buildProviderOptions(
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
) []otelgrpc.Option {
	var opts []otelgrpc.Option

	// TracerProvider: use explicit or fall back to global
	if tp != nil {
		opts = append(opts, otelgrpc.WithTracerProvider(tp))
	} else {
		opts = append(opts, otelgrpc.WithTracerProvider(otel.GetTracerProvider()))
	}

	// MeterProvider: use explicit or fall back to global
	if mp != nil {
		opts = append(opts, otelgrpc.WithMeterProvider(mp))
	} else {
		opts = append(opts, otelgrpc.WithMeterProvider(otel.GetMeterProvider()))
	}

	// Propagator: use explicit or fall back to global
	if prop != nil {
		opts = append(opts, otelgrpc.WithPropagators(prop))
	} else {
		opts = append(opts, otelgrpc.WithPropagators(otel.GetTextMapPropagator()))
	}

	return opts
}
