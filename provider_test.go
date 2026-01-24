package otx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func TestNewTracerProvider(t *testing.T) {
	// 1. Disabled - returns ErrDisabled
	cfg := &TelemetryConfig{
		Enabled: boolPtr(false),
	}
	tp, err := NewTracerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDisabled))
	assert.Nil(t, tp)

	// 2. Enabled with defaults (nop exporter check)
	// We use "nop" exporter to avoid actual connection attempts in test
	cfg = &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Exporter:    &ExporterConfig{Type: "nop"},
	}
	tp, err = NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, tp)

	// Verify propagation set
	prop := otel.GetTextMapPropagator()
	assert.NotNil(t, prop)
}

func TestNewLoggerProvider(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Logs: &LogsConfig{
			Enabled:  boolPtr(true),
			Exporter: "none",
		},
	}

	lp, err := NewLoggerProvider(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, lp)

	noLogsCfg := &TelemetryConfig{Enabled: boolPtr(true), ServiceName: "test-service"}
	_, err = NewLoggerProvider(context.Background(), noLogsCfg)
	assert.ErrorIs(t, err, ErrLogsDisabled)
}

func TestNewMeterProvider(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Metrics: &MetricsConfig{
			Enabled:  boolPtr(true),
			Exporter: "none",
			Interval: 500 * time.Millisecond,
		},
	}

	mp, err := NewMeterProvider(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, mp)

	noMetricsCfg := &TelemetryConfig{Enabled: boolPtr(true), ServiceName: "test-service"}
	_, err = NewMeterProvider(context.Background(), noMetricsCfg)
	assert.ErrorIs(t, err, ErrMetricsDisabled)
}

func TestResourceAttributesApplied(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Exporter:    &ExporterConfig{Type: "none"},
		ResourceAttributes: map[string]string{
			"env": "test",
		},
	}

	res, err := buildResource(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, res)

	attrs := res.Attributes()
	assert.True(t, hasAttribute(attrs, attribute.String("env", "test")))
}

func hasAttribute(attrs []attribute.KeyValue, want attribute.KeyValue) bool {
	for _, attr := range attrs {
		if attr.Key == want.Key && attr.Value.AsString() == want.Value.AsString() {
			return true
		}
	}

	return false
}

func TestNewTracerProvider_MissingServiceName(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "", // missing
		Exporter:    &ExporterConfig{Type: "nop"},
	}

	tp, err := NewTracerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, tp)
	assert.ErrorIs(t, err, ErrServiceNameRequired)
}

func TestNewLoggerProvider_MissingServiceName(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "", // missing
		Logs: &LogsConfig{
			Enabled:  boolPtr(true),
			Exporter: "none",
		},
	}

	lp, err := NewLoggerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, lp)
	assert.ErrorIs(t, err, ErrServiceNameRequired)
}

func TestNewMeterProvider_MissingServiceName(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "", // missing
		Metrics: &MetricsConfig{
			Enabled:  boolPtr(true),
			Exporter: "none",
		},
	}

	mp, err := NewMeterProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, mp)
	assert.ErrorIs(t, err, ErrServiceNameRequired)
}
