package otx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	content := []byte(`
enabled: true
serviceName: "test-service-file"
traces:
  enabled: true
  exporter: "console"
`)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(tmpFile, content, 0o644)
	require.NoError(t, err)

	// Test loading from file
	cfg, err := LoadConfig(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, *cfg.Enabled)
	assert.Equal(t, "test-service-file", cfg.ServiceName)
	assert.Equal(t, "console", cfg.Traces.Exporter)

	// Test environment overrides
	t.Setenv("OTEL_SERVICE_NAME", "override-service")
	cfg, err = LoadConfig(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "override-service", cfg.ServiceName)
}

func TestParseConfig(t *testing.T) {
	yamlData := []byte(`
enabled: true
serviceName: "test-service-bytes"
metrics:
  enabled: true
  interval: 5s
`)
	cfg, err := ParseConfig(yamlData)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, *cfg.Enabled)
	assert.Equal(t, "test-service-bytes", cfg.ServiceName)
	assert.NotNil(t, cfg.Metrics)
	assert.True(t, *cfg.Metrics.Enabled)
}

func TestLoadConfigDefaults(t *testing.T) {
	// Load empty config to check defaults
	cfg, err := ParseConfig([]byte("{}"))
	require.NoError(t, err)

	// Check defaults from struct tags
	// Enabled default is false
	assert.False(t, *cfg.Enabled)
	// Environment default is development
	assert.Equal(t, "development", cfg.Environment)
}

func TestLoadConfig_JSON(t *testing.T) {
	// Create a temporary config file
	content := []byte(`{
		"enabled": true,
		"serviceName": "test-service-json",
		"traces": {
			"enabled": true,
			"exporter": "otlp"
		}
	}`)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(tmpFile, content, 0o644)
	require.NoError(t, err)

	// Test loading from file
	cfg, err := LoadConfig(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, *cfg.Enabled)
	assert.Equal(t, "test-service-json", cfg.ServiceName)
	assert.Equal(t, "otlp", cfg.Traces.Exporter)
}

func TestParseConfig_JSON(t *testing.T) {
	jsonData := []byte(`{
		"enabled": true,
		"serviceName": "test-service-json-bytes",
		"metrics": {
			"enabled": true,
			"interval": "10s"
		}
	}`)
	cfg, err := ParseConfig(jsonData)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, *cfg.Enabled)
	assert.Equal(t, "test-service-json-bytes", cfg.ServiceName)
	assert.NotNil(t, cfg.Metrics)
	assert.True(t, *cfg.Metrics.Enabled)
	assert.Equal(t, "10s", cfg.Metrics.Interval.String())
}
