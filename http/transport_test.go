package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTransport(t *testing.T) {
	// Setup tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Setup client
	client := &http.Client{
		Transport: Transport(nil),
	}

	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Make request
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Verify
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "HTTP GET", spans[0].Name)
}

func TestTransportWithProviders(t *testing.T) {
	// Setup tracer with explicit provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	mp := noop.NewMeterProvider()
	prop := propagation.TraceContext{}

	// Setup client with explicit providers
	client := &http.Client{
		Transport: TransportWithProviders(nil, tp, mp, prop),
	}

	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Make request
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Verify
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "HTTP GET", spans[0].Name)
}
