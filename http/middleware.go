package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Handler wraps an http.Handler with OTel tracing and metrics.
//
// This handler uses the globally registered TracerProvider, MeterProvider, and
// TextMapPropagator. When using this with the otx package, ensure that
// global providers have been initialized.
//
// For explicit provider injection, use [HandlerWithProviders] instead.
//
// Usage:
//
//	http.Handle("/api", http.Handler(myHandler, "api.request"))
func Handler(handler http.Handler, operation string, opts ...otelhttp.Option) http.Handler {
	return otelhttp.NewHandler(handler, operation, opts...)
}

// HandlerWithProviders wraps an http.Handler with OTel tracing and metrics
// using explicitly provided TracerProvider, MeterProvider, and TextMapPropagator.
//
// This is useful when:
//   - You need to use a different provider than the global one
//   - You want explicit dependency injection for testing
//   - You have multiple HTTP handlers with different telemetry configurations
//
// If any provider is nil, the corresponding global provider will be used as fallback.
//
// Usage:
//
//	http.Handle("/api", http.HandlerWithProviders(
//	    myHandler,
//	    "api.request",
//	    tracerProvider,
//	    meterProvider,
//	    propagator,
//	))
func HandlerWithProviders(
	handler http.Handler,
	operation string,
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
	opts ...otelhttp.Option,
) http.Handler {
	allOpts := buildProviderOptions(tp, mp, prop)
	allOpts = append(allOpts, opts...)

	return otelhttp.NewHandler(handler, operation, allOpts...)
}

// Middleware returns middleware that traces HTTP requests.
//
// This middleware uses the globally registered TracerProvider, MeterProvider, and
// TextMapPropagator. When using this with the otx package, ensure that
// global providers have been initialized.
//
// For explicit provider injection, use [MiddlewareWithProviders] instead.
//
// Usage:
//
//	http.Handle("/api", http.Middleware()(myHandler))
func Middleware(opts ...otelhttp.Option) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewMiddleware("http.request", opts...)(next)
	}
}

// MiddlewareWithProviders returns middleware that traces HTTP requests
// using explicitly provided TracerProvider, MeterProvider, and TextMapPropagator.
//
// This is useful when:
//   - You need to use a different provider than the global one
//   - You want explicit dependency injection for testing
//   - You have multiple HTTP servers with different telemetry configurations
//
// If any provider is nil, the corresponding global provider will be used as fallback.
//
// Usage:
//
//	http.Handle("/api", http.MiddlewareWithProviders(
//	    tracerProvider,
//	    meterProvider,
//	    propagator,
//	)(myHandler))
func MiddlewareWithProviders(
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
	opts ...otelhttp.Option,
) func(http.Handler) http.Handler {
	allOpts := buildProviderOptions(tp, mp, prop)
	allOpts = append(allOpts, opts...)

	return func(next http.Handler) http.Handler {
		return otelhttp.NewMiddleware("http.request", allOpts...)(next)
	}
}

// buildProviderOptions creates otelhttp.Option slice from providers.
// Falls back to global providers when explicit providers are nil.
func buildProviderOptions(
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
) []otelhttp.Option {
	var opts []otelhttp.Option

	// TracerProvider: use explicit or fall back to global
	if tp != nil {
		opts = append(opts, otelhttp.WithTracerProvider(tp))
	} else {
		opts = append(opts, otelhttp.WithTracerProvider(otel.GetTracerProvider()))
	}

	// MeterProvider: use explicit or fall back to global
	if mp != nil {
		opts = append(opts, otelhttp.WithMeterProvider(mp))
	} else {
		opts = append(opts, otelhttp.WithMeterProvider(otel.GetMeterProvider()))
	}

	// Propagator: use explicit or fall back to global
	if prop != nil {
		opts = append(opts, otelhttp.WithPropagators(prop))
	} else {
		opts = append(opts, otelhttp.WithPropagators(otel.GetTextMapPropagator()))
	}

	return opts
}
