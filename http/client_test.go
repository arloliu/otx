package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewClient(t *testing.T) {
	// Setup tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Test default client
	client := NewClient()
	assert.NotNil(t, client)
	assert.NotNil(t, client.Transport)

	// Make a request to verify tracing
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Verify span created
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "HTTP GET", spans[0].Name)
}

func TestNewClientWithOptions(t *testing.T) {
	// Setup custom transport to check if options are applied
	customBase := &http.Transport{}
	timeout := 5 * time.Second

	client := NewClient(
		WithTimeout(timeout),
		WithTransport(customBase),
		WithMaxIdleConns(10),
		WithMaxConnsPerHost(5),
	)

	assert.Equal(t, timeout, client.Timeout)

	// Unwrap otel transport to check underlying transport
	assert.NotNil(t, client.Transport)
}

func TestNewClientWithProviders(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	mp := noop.NewMeterProvider()
	prop := propagation.TraceContext{}

	client := NewClientWithProviders(
		tp, mp, prop,
		WithTimeout(10*time.Second),
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
}

func TestDefaultValuesPreserved(t *testing.T) {
	// Verify that if we don't provide options, we get the defaults from http.DefaultTransport
	client := NewClient()
	_ = client.Transport

	cfg := &clientConfig{
		baseTransport: http.DefaultTransport,
	}
	tr, ok := buildTransport(cfg).(*http.Transport)
	require.True(t, ok)
	transport := tr
	dt, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	defaultTransport := dt

	// Verify key defaults are preserved
	assert.Equal(t, defaultTransport.MaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, defaultTransport.IdleConnTimeout, transport.IdleConnTimeout)
	assert.Equal(t, defaultTransport.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	assert.Equal(t, defaultTransport.ExpectContinueTimeout, transport.ExpectContinueTimeout)

	// Verify DialContext is preserved
	assert.NotNil(t, transport.DialContext)
}

func TestBuildTransport(t *testing.T) {
	cfg := &clientConfig{
		baseTransport:         http.DefaultTransport,
		dialTimeout:           1 * time.Second,
		tlsHandshakeTimeout:   2 * time.Second,
		responseHeaderTimeout: 3 * time.Second,
		expectContinueTimeout: 4 * time.Second,
		maxIdleConns:          10,
		maxIdleConnsPerHost:   5,
		maxConnsPerHost:       20,
		idleConnTimeout:       30 * time.Second,
	}

	tr, ok := buildTransport(cfg).(*http.Transport)
	require.True(t, ok)
	transport := tr

	assert.Equal(t, 2*time.Second, transport.TLSHandshakeTimeout)
	assert.Equal(t, 3*time.Second, transport.ResponseHeaderTimeout)
	assert.Equal(t, 4*time.Second, transport.ExpectContinueTimeout)
	assert.Equal(t, 10, transport.MaxIdleConns)
	assert.Equal(t, 5, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 20, transport.MaxConnsPerHost)
	assert.Equal(t, 30*time.Second, transport.IdleConnTimeout)

	// Check dialer timeout (indirectly)
	assert.NotNil(t, transport.DialContext)
}
