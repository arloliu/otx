package otx

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTracerProvider_TracesDisabledSentinel(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "svc",
		Traces:      &TracesConfig{Enabled: BoolPtr(false)},
	}

	tp, err := NewTracerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, tp)
	// Both the specific sentinel and the generic one must match.
	assert.ErrorIs(t, err, ErrTracesDisabled)
	assert.ErrorIs(t, err, ErrDisabled)
}

func TestNewTracerProvider_WholeTelemetryOffStaysGeneric(t *testing.T) {
	cfg := &TelemetryConfig{Enabled: BoolPtr(false)}

	_, err := NewTracerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDisabled)
	// The whole-off branch must NOT report the traces-specific sentinel.
	assert.False(t, errors.Is(err, ErrTracesDisabled))
}

func TestNewTracerProvider_InvalidEndpointFailsFast(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "svc",
		Traces:      &TracesConfig{Exporter: "none"},
		OTLP:        &OTLPConfig{Endpoint: "grpc://collector:4317"},
	}

	tp, err := NewTracerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, tp)
	assert.Contains(t, err.Error(), "otlp.endpoint")
}

func TestNewMeterProvider_InvalidEndpointFailsFast(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "svc",
		Metrics:     &MetricsConfig{Enabled: BoolPtr(true), Exporter: "none"},
		OTLP:        &OTLPConfig{Endpoint: "grpc://collector:4317"},
	}

	mp, err := NewMeterProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, mp)
	assert.Contains(t, err.Error(), "otlp.endpoint")
}

func TestNewLoggerProvider_InvalidEndpointFailsFast(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "svc",
		Logs:        &LogsConfig{Enabled: BoolPtr(true), Exporter: "none"},
		OTLP:        &OTLPConfig{Endpoint: "grpc://collector:4317"},
	}

	lp, err := NewLoggerProvider(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, lp)
	assert.Contains(t, err.Error(), "otlp.endpoint")
}
