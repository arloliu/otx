package otx

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestIsEnabled(t *testing.T) {
	assert.False(t, (*TelemetryConfig)(nil).IsEnabled())
	assert.False(t, (&TelemetryConfig{}).IsEnabled())
	assert.True(t, (&TelemetryConfig{Enabled: boolPtr(true)}).IsEnabled())
}

func TestTelemetryConfig_Marshaling(t *testing.T) {
	expected := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "test-service",
		Version:     "1.2.3",
		Environment: "staging",
		ResourceAttributes: map[string]string{
			"key1": "value1",
		},
		OTLP: &OTLPConfig{
			Endpoint:    "localhost:4317",
			Insecure:    boolPtr(false),
			Protocol:    "grpc",
			Timeout:     5 * time.Second,
			Compression: "gzip",
			Headers: map[string]string{
				"auth": "token",
			},
		},
		Traces: &TracesConfig{
			Enabled:  boolPtr(true),
			Exporter: "otlp",
			Sampling: &SamplingConfig{
				Sampler:    "traceidratio",
				SamplerArg: 0.5,
			},
		},
		Logs: &LogsConfig{
			Enabled:  boolPtr(true),
			Exporter: "console",
		},
		Metrics: &MetricsConfig{
			Enabled:  boolPtr(true),
			Exporter: "otlp",
			Interval: 30 * time.Second,
		},
		Propagation: &PropConfig{
			Propagators: "b3,jaeger",
		},
	}

	t.Run("YAML", func(t *testing.T) {
		data, err := yaml.Marshal(expected)
		require.NoError(t, err)

		var actual TelemetryConfig
		err = yaml.Unmarshal(data, &actual)
		require.NoError(t, err)

		assert.Equal(t, expected, &actual)
	})

	t.Run("JSON", func(t *testing.T) {
		data, err := json.Marshal(expected)
		require.NoError(t, err)

		var actual TelemetryConfig
		err = json.Unmarshal(data, &actual)
		require.NoError(t, err)

		assert.Equal(t, expected, &actual)
	})
}
