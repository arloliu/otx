package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Transport wraps an http.RoundTripper with OTel tracing for client calls.
//
// This transport uses the globally registered TracerProvider, MeterProvider, and
// TextMapPropagator. When using this with the otx package, ensure that
// global providers have been initialized.
//
// For explicit provider injection, use [TransportWithProviders] instead.
//
// If base is nil, http.DefaultTransport is used.
//
// Usage:
//
//	client := &http.Client{
//	    Transport: otxhttp.Transport(http.DefaultTransport),
//	}
func Transport(base http.RoundTripper, opts ...otelhttp.Option) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	return otelhttp.NewTransport(base, opts...)
}

// TransportWithProviders wraps an http.RoundTripper with OTel tracing
// using explicitly provided TracerProvider, MeterProvider, and TextMapPropagator.
//
// This is useful when:
//   - You need to use a different provider than the global one
//   - You want explicit dependency injection for testing
//   - You have multiple HTTP clients with different telemetry configurations
//
// If any provider is nil, the corresponding global provider will be used as fallback.
// If base is nil, http.DefaultTransport is used.
//
// Usage:
//
//	client := &http.Client{
//	    Transport: otxhttp.TransportWithProviders(
//	        http.DefaultTransport,
//	        tracerProvider,
//	        meterProvider,
//	        propagator,
//	    ),
//	}
func TransportWithProviders(
	base http.RoundTripper,
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
	opts ...otelhttp.Option,
) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	allOpts := buildProviderOptions(tp, mp, prop)
	allOpts = append(allOpts, opts...)

	return otelhttp.NewTransport(base, allOpts...)
}
