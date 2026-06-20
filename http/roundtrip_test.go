package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// H5/M11 — an otx client calling an otx-instrumented server over a real
// httptest round-trip produces Client- and Server-kind spans that share a trace
// (server parents under client), and the traceparent reaches the wire.
func TestRoundTrip_HTTP_ClientServerPropagation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	prop := propagation.TraceContext{}

	var wireTraceparent string
	handler := MiddlewareWithProviders(tp, nil, prop)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			wireTraceparent = r.Header.Get("traceparent")
			w.WriteHeader(http.StatusOK)
		},
	))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewClientWithProviders(tp, nil, prop)
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())

	// M11 — outbound traceparent injection observed at the server.
	assert.NotEmpty(t, wireTraceparent, "traceparent must be injected into the request")

	var clientSpan, serverSpan tracetest.SpanStub
	require.Eventually(t, func() bool {
		clientSpan, serverSpan = tracetest.SpanStub{}, tracetest.SpanStub{}
		for _, s := range exporter.GetSpans() {
			switch s.SpanKind {
			case oteltrace.SpanKindClient:
				clientSpan = s
			case oteltrace.SpanKindServer:
				serverSpan = s
			default:
			}
		}

		return clientSpan.SpanContext.IsValid() && serverSpan.SpanContext.IsValid()
	}, 2*time.Second, 10*time.Millisecond, "expected both client and server spans")

	assert.Equal(t, clientSpan.SpanContext.TraceID(), serverSpan.SpanContext.TraceID(), "shared trace id")
	assert.Equal(t, clientSpan.SpanContext.SpanID(), serverSpan.Parent.SpanID(), "server parents under client")
	assert.True(t, serverSpan.Parent.IsRemote(), "server parent must be remote")
}
