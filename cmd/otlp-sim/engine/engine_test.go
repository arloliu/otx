package engine

import (
	"context"
	"testing"
	"time"

	"github.com/arloliu/otx/cmd/otlp-sim/scenario"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestNew_TelemetryConfigEnabled(t *testing.T) {
	// Create engine with minimal config - this should set Enabled to true internally
	// We use "none" exporter to avoid needing a real OTLP endpoint
	ctx := context.Background()
	cfg := Config{
		Endpoint:    "localhost:4317",
		ServiceName: "test-service",
		Insecure:    true,
	}

	e, err := New(ctx, cfg)
	require.NoError(t, err, "New should not return error")
	require.NotNil(t, e, "Engine should not be nil")
	require.NotNil(t, e.tracerProvider, "TracerProvider should be created when Enabled is true")

	// Cleanup
	err = e.Shutdown(ctx)
	require.NoError(t, err, "Shutdown should succeed")
}

func TestToTraceSpanKind(t *testing.T) {
	tests := []struct {
		input    scenario.SpanKind
		expected trace.SpanKind
	}{
		{scenario.SpanKindServer, trace.SpanKindServer},
		{scenario.SpanKindClient, trace.SpanKindClient},
		{scenario.SpanKindProducer, trace.SpanKindProducer},
		{scenario.SpanKindConsumer, trace.SpanKindConsumer},
		{scenario.SpanKindInternal, trace.SpanKindInternal},
		{"UNKNOWN", trace.SpanKindInternal}, // Default case
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := toTraceSpanKind(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToLogSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected string // Compare string representation
	}{
		{"DEBUG", "DEBUG"},
		{"INFO", "INFO"},
		{"WARN", "WARN"},
		{"ERROR", "ERROR"},
		{"UNKNOWN", "INFO"}, // Default case
		{"", "INFO"},        // Empty string
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toLogSeverity(tt.input)
			assert.Equal(t, tt.expected, result.String())
		})
	}
}

func TestParseAttributes_StringValues(t *testing.T) {
	attrs := map[string]string{
		"string.key":  "string-value",
		"another.key": "another-value",
	}

	result := parseAttributes(attrs)

	assert.Len(t, result, 2)

	resultMap := attrMapFromSlice(result)
	assert.Equal(t, "string-value", resultMap["string.key"])
	assert.Equal(t, "another-value", resultMap["another.key"])
}

func TestParseAttributes_IntValues(t *testing.T) {
	attrs := map[string]string{
		"int.key":      "42",
		"negative.key": "-10",
	}

	result := parseAttributes(attrs)

	assert.Len(t, result, 2)

	resultMap := attrMapFromSlice(result)
	assert.Equal(t, int64(42), resultMap["int.key"])
	assert.Equal(t, int64(-10), resultMap["negative.key"])
}

func TestParseAttributes_FloatValues(t *testing.T) {
	attrs := map[string]string{
		"float.key":   "3.14",
		"percent.key": "0.95",
	}

	result := parseAttributes(attrs)

	assert.Len(t, result, 2)

	resultMap := attrMapFromSlice(result)
	assert.Equal(t, 3.14, resultMap["float.key"])
	assert.Equal(t, 0.95, resultMap["percent.key"])
}

func TestParseAttributes_BoolValues(t *testing.T) {
	attrs := map[string]string{
		"true.key":  "true",
		"false.key": "false",
	}

	result := parseAttributes(attrs)

	assert.Len(t, result, 2)

	resultMap := attrMapFromSlice(result)
	assert.Equal(t, true, resultMap["true.key"])
	assert.Equal(t, false, resultMap["false.key"])
}

func TestParseAttributes_MixedValues(t *testing.T) {
	attrs := map[string]string{
		"string": "hello",
		"int":    "100",
		"float":  "1.5",
		"bool":   "true",
	}

	result := parseAttributes(attrs)

	assert.Len(t, result, 4)

	resultMap := attrMapFromSlice(result)
	assert.Equal(t, "hello", resultMap["string"])
	assert.Equal(t, int64(100), resultMap["int"])
	assert.Equal(t, 1.5, resultMap["float"])
	assert.Equal(t, true, resultMap["bool"])
}

func TestParseAttributes_EmptyMap(t *testing.T) {
	result := parseAttributes(map[string]string{})
	assert.Empty(t, result)
}

func TestEngine_ApplyJitter_ZeroPercent(t *testing.T) {
	e := &Engine{jitterPct: 0}
	d := 100 * time.Millisecond

	result := e.applyJitter(d)

	assert.Equal(t, d, result)
}

func TestEngine_ApplyJitter_NegativePercent(t *testing.T) {
	e := &Engine{jitterPct: -10}
	d := 100 * time.Millisecond

	result := e.applyJitter(d)

	assert.Equal(t, d, result)
}

func TestEngine_ApplyJitter_WithJitter(t *testing.T) {
	e := &Engine{jitterPct: 50}
	d := 100 * time.Millisecond

	// Run multiple times to verify jitter is applied
	seenDifferent := false
	for range 100 {
		result := e.applyJitter(d)
		// Result should be within 50% of original
		assert.GreaterOrEqual(t, result, 50*time.Millisecond)
		assert.LessOrEqual(t, result, 150*time.Millisecond)

		if result != d {
			seenDifferent = true
		}
	}

	// Should have seen at least one different value
	assert.True(t, seenDifferent, "jitter should produce varied results")
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}

	// Verify zero values
	assert.Empty(t, cfg.Endpoint)
	assert.Empty(t, cfg.ServiceName)
	assert.False(t, cfg.UseHTTP)
	assert.False(t, cfg.Insecure)
	assert.False(t, cfg.EnableLogs)
	assert.Zero(t, cfg.JitterPct)
}

// Helper to convert attribute slice to map for easier testing.
func attrMapFromSlice(attrs []attribute.KeyValue) map[string]any {
	result := make(map[string]any)
	for _, kv := range attrs {
		result[string(kv.Key)] = kv.Value.AsInterface()
	}

	return result
}
