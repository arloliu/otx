package http

import (
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// clientConfig holds configuration for HTTP client creation.
type clientConfig struct {
	// Client-level timeout
	timeout time.Duration

	// Transport-level timeouts
	dialTimeout           time.Duration
	tlsHandshakeTimeout   time.Duration
	responseHeaderTimeout time.Duration
	expectContinueTimeout time.Duration

	// Connection pool settings
	maxIdleConns        int
	maxIdleConnsPerHost int
	maxConnsPerHost     int
	idleConnTimeout     time.Duration

	// Base transport (before OTel wrapping)
	baseTransport http.RoundTripper

	// Options forwarded to the underlying otelhttp.Transport.
	otelOpts []otelhttp.Option
}

// ClientOption configures an HTTP client.
type ClientOption func(*clientConfig)

// WithTimeout sets the request timeout for the client.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.timeout = d
	}
}

// WithDialTimeout sets the timeout for dialing TCP connections.
func WithDialTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.dialTimeout = d
	}
}

// WithTLSHandshakeTimeout sets the timeout for TLS handshakes.
func WithTLSHandshakeTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.tlsHandshakeTimeout = d
	}
}

// WithResponseHeaderTimeout sets the time to wait for response headers after writing the request.
func WithResponseHeaderTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.responseHeaderTimeout = d
	}
}

// WithExpectContinueTimeout sets the max time to wait for a 100-continue response.
func WithExpectContinueTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.expectContinueTimeout = d
	}
}

// WithMaxIdleConns sets the max idle connections across all hosts.
func WithMaxIdleConns(n int) ClientOption {
	return func(c *clientConfig) {
		c.maxIdleConns = n
	}
}

// WithMaxIdleConnsPerHost sets the max idle connections to keep per-host.
func WithMaxIdleConnsPerHost(n int) ClientOption {
	return func(c *clientConfig) {
		c.maxIdleConnsPerHost = n
	}
}

// WithMaxConnsPerHost sets the max total connections per host.
func WithMaxConnsPerHost(n int) ClientOption {
	return func(c *clientConfig) {
		c.maxConnsPerHost = n
	}
}

// WithIdleConnTimeout sets the maximum amount of time an idle (keep-alive)
// connection will remain idle before closing itself.
func WithIdleConnTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.idleConnTimeout = d
	}
}

// WithTransport sets a custom base RoundTripper to wrap with OTel tracing.
//
// Precedence depends on the concrete type of rt:
//   - If rt is an *http.Transport, it is Clone()d and the otx transport
//     options (WithDialTimeout, WithMaxIdleConns, etc.) are applied on top of
//     the clone. The otx options take precedence over any matching field
//     already set on rt.
//   - If rt is any other RoundTripper, it is used as-is and all transport-level
//     otx options are ignored, because they cannot be applied to an opaque
//     RoundTripper.
//
// Parameters:
//   - rt: the base RoundTripper to wrap; nil leaves the default in effect.
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(c *clientConfig) {
		if rt == nil {
			return
		}
		c.baseTransport = rt
	}
}

// WithOTelOptions forwards otelhttp.Option values to the underlying OTel
// transport created by [NewClient] / [NewClientWithProviders].
//
// This makes client-side options such as otelhttp.WithSpanNameFormatter or
// otelhttp.WithFilter reachable from the convenience constructors, matching the
// server-side [Handler] / [Middleware] surface.
//
// Parameters:
//   - opts: otelhttp options applied to the transport in order.
func WithOTelOptions(opts ...otelhttp.Option) ClientOption {
	return func(c *clientConfig) {
		c.otelOpts = append(c.otelOpts, opts...)
	}
}

// NewClient creates an http.Client with OTel tracing enabled.
//
// This client uses the globally registered TracerProvider, MeterProvider, and
// TextMapPropagator. Ensure global providers have been initialized.
//
// If no transport options are provided, it defaults to using http.DefaultTransport settings
// adjusted with any provided timeout options.
//
// Usage:
//
//	client := otxhttp.NewClient(
//	    otxhttp.WithTimeout(30 * time.Second),
//	    otxhttp.WithMaxIdleConnsPerHost(10),
//	)
func NewClient(opts ...ClientOption) *http.Client {
	config := &clientConfig{
		baseTransport: http.DefaultTransport,
	}

	for _, opt := range opts {
		opt(config)
	}

	transport := buildTransport(config)
	otelTransport := Transport(transport, config.otelOpts...)

	return &http.Client{
		Transport: otelTransport,
		Timeout:   config.timeout,
	}
}

// NewClientWithProviders creates an http.Client with OTel tracing enabled
// using explicitly provided TracerProvider, MeterProvider, and TextMapPropagator.
//
// If any provider is nil, the corresponding global provider will be used as fallback.
//
// Usage:
//
//	client := otxhttp.NewClientWithProviders(
//	    tracerProvider,
//	    meterProvider,
//	    propagator,
//	    otxhttp.WithTimeout(10 * time.Second),
//	)
func NewClientWithProviders(
	tp trace.TracerProvider,
	mp metric.MeterProvider,
	prop propagation.TextMapPropagator,
	opts ...ClientOption,
) *http.Client {
	config := &clientConfig{
		baseTransport: http.DefaultTransport,
	}

	for _, opt := range opts {
		opt(config)
	}

	transport := buildTransport(config)
	otelTransport := TransportWithProviders(transport, tp, mp, prop, config.otelOpts...)

	return &http.Client{
		Transport: otelTransport,
		Timeout:   config.timeout,
	}
}

// buildTransport configures the underlying transport based on config.
//
// When the base is an *http.Transport (including the default), it is Clone()d
// and the transport-pool / timeout options are applied on top of the clone.
//
// When the base is any other RoundTripper, it is returned verbatim and all
// transport-level otx options (dial / TLS / response-header / connection-pool
// timeouts) are silently ignored, because they cannot be applied to an opaque
// RoundTripper. Callers needing those settings must supply an *http.Transport
// base.
func buildTransport(c *clientConfig) http.RoundTripper {
	var transport *http.Transport

	if c.baseTransport == http.DefaultTransport {
		t, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return http.DefaultTransport
		}
		transport = t.Clone()
	} else if t, ok := c.baseTransport.(*http.Transport); ok {
		transport = t.Clone()
	} else {
		// Not an *http.Transport (e.g. a custom RoundTripper): return it as-is.
		// Transport-level otx options cannot be applied to an opaque
		// RoundTripper, so they are ignored here (see WithTransport docs).
		return c.baseTransport
	}

	// Apply transport settings
	if c.dialTimeout > 0 {
		transport.DialContext = (&net.Dialer{
			Timeout:   c.dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	if c.tlsHandshakeTimeout > 0 {
		transport.TLSHandshakeTimeout = c.tlsHandshakeTimeout
	}

	if c.responseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = c.responseHeaderTimeout
	}

	if c.expectContinueTimeout > 0 {
		transport.ExpectContinueTimeout = c.expectContinueTimeout
	}

	if c.maxIdleConns > 0 {
		transport.MaxIdleConns = c.maxIdleConns
	}

	if c.maxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = c.maxIdleConnsPerHost
	}

	if c.maxConnsPerHost > 0 {
		transport.MaxConnsPerHost = c.maxConnsPerHost
	}

	if c.idleConnTimeout > 0 {
		transport.IdleConnTimeout = c.idleConnTimeout
	}

	return transport
}
