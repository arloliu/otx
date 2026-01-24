package http

import (
	"net"
	"net/http"
	"time"

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

// WithTransport sets a custom base transport options.
// Note: Transport timeouts/settings configured here will override other options if set on this transport.
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(c *clientConfig) {
		c.baseTransport = rt
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
	otelTransport := Transport(transport)

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
	otelTransport := TransportWithProviders(transport, tp, mp, prop)

	return &http.Client{
		Transport: otelTransport,
		Timeout:   config.timeout,
	}
}

// buildTransport configures the underlying transport based on config
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
		// Not an http.Transport (e.g. custom RoundTripper), just return it
		// We can't apply transport-level timeouts to an opaque RoundTripper
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
