package zaplog

import (
	"testing"

	"github.com/arloliu/otx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func boolPtr(b bool) *bool { return &b }

// Endpoint precedence reflects GetOTLPConfig's WHOLESALE semantics: an OTLP
// block present means the deprecated Exporter block is ignored entirely; a nil
// OTLP block falls back to deprecated Exporter; Logs.Endpoint overlays on top.
func TestEffectiveEndpoint_Precedence(t *testing.T) {
	tests := []struct {
		name string
		cfg  *otx.TelemetryConfig
		want string
	}{
		{
			name: "logs endpoint overlay wins over OTLP",
			cfg: &otx.TelemetryConfig{
				OTLP: &otx.OTLPConfig{Endpoint: "shared:4317"},
				Logs: &otx.LogsConfig{Endpoint: "logs:4318"},
			},
			want: "logs:4318",
		},
		{
			name: "OTLP block present, deprecated Exporter ignored",
			cfg: &otx.TelemetryConfig{
				OTLP:     &otx.OTLPConfig{Endpoint: "shared:4317"},
				Exporter: &otx.ExporterConfig{Endpoint: "deprecated:4317"},
			},
			want: "shared:4317",
		},
		{
			name: "nil OTLP falls back to deprecated Exporter",
			cfg: &otx.TelemetryConfig{
				Exporter: &otx.ExporterConfig{Endpoint: "deprecated:4317"},
			},
			want: "deprecated:4317",
		},
		{
			name: "empty default",
			cfg:  &otx.TelemetryConfig{},
			want: defaultEndpoint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.cfg.GetOTLPConfig()
			assert.Equal(t, tt.want, effectiveEndpoint(tt.cfg, base))
		})
	}
}

func TestEffectiveProtocol_Precedence(t *testing.T) {
	tests := []struct {
		name string
		cfg  *otx.TelemetryConfig
		want string
	}{
		{
			name: "logs protocol overlay wins",
			cfg: &otx.TelemetryConfig{
				OTLP: &otx.OTLPConfig{Protocol: "grpc"},
				Logs: &otx.LogsConfig{Protocol: "http/protobuf"},
			},
			want: "http/protobuf",
		},
		{
			name: "http alias normalized to http/protobuf",
			cfg: &otx.TelemetryConfig{
				Logs: &otx.LogsConfig{Protocol: "http"},
			},
			want: "http/protobuf",
		},
		{
			name: "OTLP block present, deprecated Exporter ignored",
			cfg: &otx.TelemetryConfig{
				OTLP:     &otx.OTLPConfig{Protocol: "http/protobuf"},
				Exporter: &otx.ExporterConfig{Protocol: "grpc"},
			},
			want: "http/protobuf",
		},
		{
			name: "empty default is http/protobuf (accepted by NewCore)",
			cfg:  &otx.TelemetryConfig{},
			want: defaultProtocol,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.cfg.GetOTLPConfig()
			assert.Equal(t, tt.want, effectiveProtocol(tt.cfg, base))
		})
	}
}

func TestBuildEndpoint_Scheme(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		insecure bool
		want     string
	}{
		{
			name:     "bare host:port + insecure -> http",
			endpoint: "collector:4318", insecure: true, want: "http://collector:4318",
		},
		{
			name:     "bare host:port + secure -> https",
			endpoint: "collector:4318", insecure: false, want: "https://collector:4318",
		},
		{
			name:     "explicit http scheme passes through",
			endpoint: "http://c:4318/v1/logs", insecure: false, want: "http://c:4318/v1/logs",
		},
		{
			name:     "explicit https scheme passes through",
			endpoint: "https://c:4318", insecure: true, want: "https://c:4318",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildEndpoint(tt.endpoint, tt.insecure))
		})
	}
}

func TestResolveLevel(t *testing.T) {
	t.Run("explicit wins over MinLevel", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{Logs: &otx.LogsConfig{MinLevel: "error"}}
		lvl, err := resolveLevel(cfg, zapcore.DebugLevel)
		require.NoError(t, err)
		assert.True(t, lvl.Enabled(zapcore.DebugLevel))
	})

	t.Run("nil falls to MinLevel", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{Logs: &otx.LogsConfig{MinLevel: "warn"}}
		lvl, err := resolveLevel(cfg, nil)
		require.NoError(t, err)
		assert.False(t, lvl.Enabled(zapcore.InfoLevel))
		assert.True(t, lvl.Enabled(zapcore.WarnLevel))
	})

	t.Run("nil + empty MinLevel falls to info", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{Logs: &otx.LogsConfig{}}
		lvl, err := resolveLevel(cfg, nil)
		require.NoError(t, err)
		assert.False(t, lvl.Enabled(zapcore.DebugLevel))
		assert.True(t, lvl.Enabled(zapcore.InfoLevel))
	})

	t.Run("nil + nil Logs falls to info", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{}
		lvl, err := resolveLevel(cfg, nil)
		require.NoError(t, err)
		require.NotNil(t, lvl)
		assert.True(t, lvl.Enabled(zapcore.InfoLevel))
	})

	t.Run("never returns nil", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{}
		lvl, err := resolveLevel(cfg, nil)
		require.NoError(t, err)
		assert.NotNil(t, lvl)
	})
}
