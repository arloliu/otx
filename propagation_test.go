package otx

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

// useGlobalPropagator installs the propagator built from cfg as the OTel global
// for the duration of the test and restores the previous one on cleanup. The
// exported Inject*/Extract* helpers read otel.GetTextMapPropagator(), so a
// global must be installed for them to do anything.
func useGlobalPropagator(t *testing.T, cfg *PropConfig) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(BuildPropagator(cfg))
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

// sampledSpanContext returns a remote, sampled SpanContext with deterministic
// IDs so round-trip assertions can compare exact values.
func sampledSpanContext(t *testing.T) trace.SpanContext {
	t.Helper()
	tid, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex("0123456789abcdef")
	require.NoError(t, err)

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
}

// M6 — InjectHTTP/ExtractHTTP must round-trip the W3C SpanContext through a
// real http.Header.
func TestInjectExtractHTTP_RoundTrip(t *testing.T) {
	useGlobalPropagator(t, nil) // tracecontext + baggage

	sc := sampledSpanContext(t)
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	h := http.Header{}
	InjectHTTP(ctx, h)
	require.NotEmpty(t, h.Get("traceparent"), "traceparent must be injected")

	got := trace.SpanContextFromContext(ExtractHTTP(context.Background(), h))
	assert.Equal(t, sc.TraceID(), got.TraceID())
	assert.Equal(t, sc.SpanID(), got.SpanID())
	assert.True(t, got.IsSampled(), "sampled flag must survive")
	assert.True(t, got.IsRemote(), "extracted context must be marked remote")
}

// H6 — InjectGRPC/ExtractGRPC must round-trip the W3C SpanContext through real
// gRPC metadata via metadataCarrier.
func TestInjectExtractGRPC_RoundTrip(t *testing.T) {
	useGlobalPropagator(t, nil)

	sc := sampledSpanContext(t)
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	md := metadata.MD{}
	InjectGRPC(ctx, md)
	require.NotEmpty(t, md.Get("traceparent"), "traceparent must be injected into metadata")

	got := trace.SpanContextFromContext(ExtractGRPC(context.Background(), md))
	assert.Equal(t, sc.TraceID(), got.TraceID())
	assert.Equal(t, sc.SpanID(), got.SpanID())
	assert.True(t, got.IsSampled())
	assert.True(t, got.IsRemote())
}

// H6 — direct coverage of the metadataCarrier adapter that InjectGRPC/ExtractGRPC
// rely on. A regression in Get/Set/Keys would silently break gRPC propagation.
func TestMetadataCarrier(t *testing.T) {
	t.Run("Set then Get", func(t *testing.T) {
		md := metadata.MD{}
		c := metadataCarrier(md)
		c.Set("traceparent", "value-1")
		assert.Equal(t, "value-1", c.Get("traceparent"))
	})

	t.Run("Get returns first of multiple values", func(t *testing.T) {
		md := metadata.MD{}
		md.Append("key", "first", "second")
		c := metadataCarrier(md)
		assert.Equal(t, "first", c.Get("key"))
	})

	t.Run("Get absent key returns empty", func(t *testing.T) {
		c := metadataCarrier(metadata.MD{})
		assert.Empty(t, c.Get("missing"))
	})

	t.Run("Keys lists every metadata key", func(t *testing.T) {
		md := metadata.MD{}
		c := metadataCarrier(md)
		c.Set("traceparent", "v1")
		c.Set("baggage", "v2")
		assert.ElementsMatch(t, []string{"traceparent", "baggage"}, c.Keys())
	})

	t.Run("Keys of empty carrier is empty", func(t *testing.T) {
		c := metadataCarrier(metadata.MD{})
		assert.Empty(t, c.Keys())
	})
}

// H7 — baggage must survive an Inject -> carrier -> Extract boundary, not just
// live on a single in-process context.
func TestBaggage_SurvivesHTTPCarrier(t *testing.T) {
	useGlobalPropagator(t, nil) // tracecontext + baggage

	ctx := MustSetBaggage(context.Background(), "tenant", "acme")
	ctx = MustSetBaggage(ctx, "region", "us-east-1")

	h := http.Header{}
	InjectHTTP(ctx, h)
	require.NotEmpty(t, h.Get("baggage"), "baggage header must be injected")

	out := ExtractHTTP(context.Background(), h)
	assert.Equal(t, "acme", GetBaggage(out, "tenant"))
	assert.Equal(t, "us-east-1", GetBaggage(out, "region"))
	assert.Equal(t, map[string]string{"tenant": "acme", "region": "us-east-1"}, AllBaggage(out))
}

// H7 — when only tracecontext is configured, baggage must NOT cross the carrier.
func TestBaggage_DroppedWhenOnlyTraceContextConfigured(t *testing.T) {
	useGlobalPropagator(t, &PropConfig{Propagators: "tracecontext"}) // no baggage

	ctx := MustSetBaggage(context.Background(), "tenant", "acme")

	h := http.Header{}
	InjectHTTP(ctx, h)
	assert.Empty(t, h.Get("baggage"), "baggage must not be injected when not configured")

	out := ExtractHTTP(context.Background(), h)
	_, ok := LookupBaggage(out, "tenant")
	assert.False(t, ok, "baggage must not be recovered when propagator excludes it")
}
