package otx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestSpanHelpers(t *testing.T) {
	// Setup global provider for testing helpers
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Exporter:    &ExporterConfig{Type: "nop"},
		Sampling:    &SamplingConfig{Sampler: "always_on"},
	}
	tp, err := NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	otel.SetTracerProvider(tp)

	// Ensure global tracer is set for Start() helper
	InitTracing(tp.Tracer("otx"), DefaultNamer{})

	ctx := context.Background()
	ctx, span := Start(ctx, "test-op")
	defer span.End()

	assert.NotNil(t, span)
	assert.True(t, span.IsRecording())

	// Test span kind helpers
	ctx2, serverSpan := StartServer(ctx, "server-op")
	assert.NotNil(t, serverSpan)
	serverSpan.End()

	ctx3, clientSpan := StartClient(ctx2, "client-op")
	assert.NotNil(t, clientSpan)
	clientSpan.End()

	_, internalSpan := StartInternal(ctx3, "internal-op")
	assert.NotNil(t, internalSpan)
	internalSpan.End()

	// Test producer and consumer spans
	ctx4, producerSpan := StartProducer(ctx3, "producer-op")
	assert.NotNil(t, producerSpan)
	producerSpan.End()

	_, consumerSpan := StartConsumer(ctx4, "consumer-op")
	assert.NotNil(t, consumerSpan)
	consumerSpan.End()

	// Test Span() helper
	currentSpan := Span(ctx)
	assert.NotNil(t, currentSpan)

	// Test RecordError with nil (should not panic)
	RecordError(ctx, nil)

	// Test SetSuccess
	SetSuccess(ctx)
}

func TestInitTracing_NilNamer(t *testing.T) {
	// Setup provider
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Exporter:    &ExporterConfig{Type: "nop"},
	}
	tp, err := NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	// InitTracing with nil namer should not panic
	// and should use default namer internally
	assert.NotPanics(t, func() {
		InitTracing(tp.Tracer("otx"), nil)
	})

	// Start should work without panic
	ctx, span := Start(context.Background(), "test-op")
	assert.NotNil(t, span)
	span.End()
	_ = ctx
}

func TestInitTracing_NilTracer(t *testing.T) {
	// InitTracing with nil tracer should not panic
	assert.NotPanics(t, func() {
		InitTracing(nil, DefaultNamer{})
	})

	// Start with nil tracer returns current span from context (no-op)
	ctx := context.Background()
	ctx2, span := Start(ctx, "test-op")
	assert.NotNil(t, span)
	// With nil tracer, Start returns the span from context (which is a no-op span)
	assert.Equal(t, ctx, ctx2) // context unchanged when tracer is nil
}
