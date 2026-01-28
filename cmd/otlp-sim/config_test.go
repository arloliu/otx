package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig_Defaults(t *testing.T) {
	cfg := newConfig()

	// Defaults come from fuda struct tags
	assert.Equal(t, "localhost:4317", cfg.Endpoint)
	assert.Equal(t, "payment", cfg.Scenario)
	require.NotNil(t, cfg.Insecure)
	assert.True(t, *cfg.Insecure)
	assert.True(t, cfg.IsInsecure())
	assert.False(t, cfg.UseHTTP)
	assert.Empty(t, cfg.ServiceName)
	assert.Empty(t, cfg.ScenarioFile)
	assert.False(t, cfg.EnableLogs)
	assert.Equal(t, 10, cfg.Count)
	assert.Equal(t, time.Minute, cfg.Duration)
	assert.Equal(t, 1.0, cfg.Rate)
	assert.Equal(t, 20, cfg.Jitter)
}

func TestConfig_ApplyEnvOverrides_Endpoint(t *testing.T) {
	cfg := newConfig()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "custom:4318")
	cfg.applyEnvOverrides()

	assert.Equal(t, "custom:4318", cfg.Endpoint)
}

func TestConfig_ApplyEnvOverrides_Insecure(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{"true", "true", true},
		{"false", "false", false},
		{"1", "1", true},
		{"0", "0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig()
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", tt.envValue)
			cfg.applyEnvOverrides()

			require.NotNil(t, cfg.Insecure)
			assert.Equal(t, tt.expected, *cfg.Insecure)
			assert.Equal(t, tt.expected, cfg.IsInsecure())
		})
	}
}

func TestConfig_ApplyEnvOverrides_ServiceName(t *testing.T) {
	cfg := newConfig()

	t.Setenv("OTEL_SERVICE_NAME", "my-service")
	cfg.applyEnvOverrides()

	assert.Equal(t, "my-service", cfg.ServiceName)
}

func TestConfig_ApplyEnvOverrides_MultipleVars(t *testing.T) {
	cfg := newConfig()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	t.Setenv("OTEL_SERVICE_NAME", "test-app")

	cfg.applyEnvOverrides()

	assert.Equal(t, "collector:4317", cfg.Endpoint)
	require.NotNil(t, cfg.Insecure)
	assert.False(t, *cfg.Insecure)
	assert.Equal(t, "test-app", cfg.ServiceName)
}

func TestConfig_ApplyEnvOverrides_NoEnvVars(t *testing.T) {
	cfg := newConfig()

	cfg.applyEnvOverrides()

	// Should keep defaults
	assert.Equal(t, "localhost:4317", cfg.Endpoint)
	assert.True(t, cfg.IsInsecure())
	assert.False(t, cfg.UseHTTP)
	assert.Empty(t, cfg.ServiceName)
}

func TestConfig_IsInsecure_NilPointer(t *testing.T) {
	cfg := &Config{Insecure: nil}

	// Should default to true when nil
	assert.True(t, cfg.IsInsecure())
}
