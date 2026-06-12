package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsConfig_ProtocolMinLevel_Defaults(t *testing.T) {
	// Loaded config without logs.protocol/minLevel leaves both empty;
	// callers (zaplog) treat empty protocol as their own default
	// (http/protobuf, accepted) and empty minLevel as info. fuda applies no
	// struct default to these omitempty fields.
	cfg, err := ParseConfig([]byte(`
enabled: true
serviceName: "svc"
logs:
  enabled: true
`))
	require.NoError(t, err)
	require.NotNil(t, cfg.Logs)
	assert.Empty(t, cfg.Logs.Protocol)
	assert.Empty(t, cfg.Logs.MinLevel)
}

func TestLogsConfig_ProtocolMinLevel_FromYAML(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
enabled: true
serviceName: "svc"
logs:
  enabled: true
  protocol: "http/protobuf"
  minLevel: "warn"
`))
	require.NoError(t, err)
	require.NotNil(t, cfg.Logs)
	assert.Equal(t, "http/protobuf", cfg.Logs.Protocol)
	assert.Equal(t, "warn", cfg.Logs.MinLevel)
}

func TestLogsConfig_Protocol_FromEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http")
	cfg, err := ParseConfig([]byte(`
enabled: true
serviceName: "svc"
logs:
  enabled: true
`))
	require.NoError(t, err)
	require.NotNil(t, cfg.Logs)
	assert.Equal(t, "http", cfg.Logs.Protocol)
}

func TestLogsConfig_Protocol_InvalidFailsValidation(t *testing.T) {
	_, err := ParseConfig([]byte(`
enabled: true
serviceName: "svc"
logs:
  enabled: true
  protocol: "tcp"
`))
	require.Error(t, err)
}

func TestLogsConfig_MinLevel_InvalidFailsValidation(t *testing.T) {
	_, err := ParseConfig([]byte(`
enabled: true
serviceName: "svc"
logs:
  enabled: true
  minLevel: "verbose"
`))
	require.Error(t, err)
}

func TestResolveLogExporterParams_ProtocolOverride(t *testing.T) {
	// Shared OTLP protocol is grpc; the logs-specific override wins.
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP:        &OTLPConfig{Endpoint: "collector:4317", Protocol: "grpc"},
		Logs:        &LogsConfig{Enabled: boolPtr(true), Protocol: "http/protobuf"},
	}

	params := resolveLogExporterParams(cfg)
	assert.Equal(t, "http/protobuf", params.Protocol)
	// Endpoint still comes from the shared OTLP block (no logs override here).
	assert.Equal(t, "collector:4317", params.Endpoint)
}

func TestResolveLogExporterParams_NoOverrideKeepsBase(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP:        &OTLPConfig{Endpoint: "collector:4317", Protocol: "grpc"},
		Logs:        &LogsConfig{Enabled: boolPtr(true)},
	}

	params := resolveLogExporterParams(cfg)
	assert.Equal(t, "grpc", params.Protocol)
}
