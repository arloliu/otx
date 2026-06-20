package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// serveHealth starts a gRPC server with the bundled health service over bufconn
// using the given server stats handler, and returns a dialer for it.
func serveHealth(t *testing.T, sh grpc.ServerOption) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(sh)
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			panic(err)
		}
	}()
	t.Cleanup(s.Stop)

	return lis
}

func dialHealth(t *testing.T, lis *bufconn.Listener, sh grpc.DialOption) healthpb.HealthClient {
	t.Helper()
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		sh,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return healthpb.NewHealthClient(conn)
}

// findSpansByKind polls the exporter until both a client- and server-kind span
// are present (the server span is exported on a separate goroutine), then
// returns them.
func findSpansByKind(t *testing.T, exporter *tracetest.InMemoryExporter) (client, server tracetest.SpanStub) {
	t.Helper()
	require.Eventually(t, func() bool {
		client, server = tracetest.SpanStub{}, tracetest.SpanStub{}
		for _, s := range exporter.GetSpans() {
			switch s.SpanKind {
			case oteltrace.SpanKindClient:
				client = s
			case oteltrace.SpanKindServer:
				server = s
			default:
			}
		}

		return client.SpanContext.IsValid() && server.SpanContext.IsValid()
	}, 2*time.Second, 10*time.Millisecond, "expected both client and server spans")

	return client, server
}

// H4 — invoking an RPC produces Client- and Server-kind spans that share a trace
// and where the server span parents under the client span.
func TestRoundTrip_GRPC_ClientServerPropagation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	prop := propagation.TraceContext{}

	lis := serveHealth(t, grpc.StatsHandler(ServerHandlerWithProviders(tp, nil, prop)))
	client := dialHealth(t, lis, grpc.WithStatsHandler(ClientHandlerWithProviders(tp, nil, prop)))

	resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())

	clientSpan, serverSpan := findSpansByKind(t, exporter)
	assert.Equal(t, clientSpan.SpanContext.TraceID(), serverSpan.SpanContext.TraceID(), "shared trace id")
	assert.Equal(t, clientSpan.SpanContext.SpanID(), serverSpan.Parent.SpanID(), "server parents under client")
	assert.True(t, serverSpan.Parent.IsRemote(), "server parent must be remote")
}

// M10 — with nil providers the handlers fall back to the global TracerProvider,
// and a span actually lands there at runtime (not just at construction).
func TestRoundTrip_GRPC_NilProvidersUseGlobal(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	exporter := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(trace.NewTracerProvider(trace.WithSyncer(exporter)))
	otel.SetTextMapPropagator(propagation.TraceContext{})

	lis := serveHealth(t, grpc.StatsHandler(ServerHandler()))
	client := dialHealth(t, lis, grpc.WithStatsHandler(ClientHandler()))

	_, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		for _, s := range exporter.GetSpans() {
			if s.SpanKind == oteltrace.SpanKindServer {
				return true
			}
		}

		return false
	}, 2*time.Second, 10*time.Millisecond, "a server span must land in the global exporter")
}
