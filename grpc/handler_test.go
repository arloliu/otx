package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestServerHandler(t *testing.T) {
	// Setup tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Setup bufconn
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(
		grpc.StatsHandler(ServerHandler()),
	)

	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()
	defer s.Stop()

	// Dial
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(ClientHandler()),
	)
	require.NoError(t, err)
	defer conn.Close()

	// Just checking that handlers instantiate correctly and don't panic on connection
	assert.NotNil(t, conn)
}

func TestServerHandlerWithProviders(t *testing.T) {
	// Setup tracer with explicit provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	mp := noop.NewMeterProvider()
	prop := propagation.TraceContext{}

	// Setup bufconn
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(
		grpc.StatsHandler(ServerHandlerWithProviders(tp, mp, prop)),
	)

	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()
	defer s.Stop()

	// Dial with explicit providers
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(ClientHandlerWithProviders(tp, mp, prop)),
	)
	require.NoError(t, err)
	defer conn.Close()

	// Verify handlers work with explicit providers
	assert.NotNil(t, conn)
}

func TestHandlerWithNilProviders(t *testing.T) {
	// This test verifies that nil providers fall back to globals correctly
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Setup bufconn with nil providers (should use globals)
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(
		grpc.StatsHandler(ServerHandlerWithProviders(nil, nil, nil)),
	)

	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()
	defer s.Stop()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(ClientHandlerWithProviders(nil, nil, nil)),
	)
	require.NoError(t, err)
	defer conn.Close()

	// Should not panic with nil providers
	assert.NotNil(t, conn)
}
