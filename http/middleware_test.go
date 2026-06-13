package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHandler(t *testing.T) {
	// Setup tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Setup handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := Middleware()(handler)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	// Verify
	assert.Equal(t, http.StatusOK, w.Code)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "http.request", spans[0].Name)
}

func TestMiddlewareWithProviders(t *testing.T) {
	// Setup tracer with explicit provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	mp := noop.NewMeterProvider()
	prop := propagation.TraceContext{}

	// Setup handler with explicit providers
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := MiddlewareWithProviders(tp, mp, prop)(handler)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	// Verify
	assert.Equal(t, http.StatusOK, w.Code)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "http.request", spans[0].Name)
}

func TestHandlerWithProviders(t *testing.T) {
	// Setup tracer with explicit provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	mp := noop.NewMeterProvider()
	prop := propagation.TraceContext{}

	// Setup handler with explicit providers
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := HandlerWithProviders(handler, "test.operation", tp, mp, prop)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	// Verify
	assert.Equal(t, http.StatusOK, w.Code)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "test.operation", spans[0].Name)
}

// TestHandlerCallerSpanNameFormatterWins verifies that the operation-based span
// name otx restores after otelhttp v0.69 is only a default: a caller-supplied
// WithSpanNameFormatter still wins because it is applied after otx's default.
func TestHandlerCallerSpanNameFormatterWins(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := HandlerWithProviders(
		handler, "test.operation", tp, noop.NewMeterProvider(), propagation.TraceContext{},
		otelhttp.WithSpanNameFormatter(func(string, *http.Request) string {
			return "caller.override"
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "caller.override", spans[0].Name)
}

func TestMiddlewareWithNilProviders(t *testing.T) {
	// This test verifies that nil providers fall back to globals correctly
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Setup handler with nil providers (should use globals)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := MiddlewareWithProviders(nil, nil, nil)(handler)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	// Verify
	assert.Equal(t, http.StatusOK, w.Code)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
}
