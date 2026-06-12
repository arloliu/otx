package zaplog

import (
	"testing"

	"github.com/arloliu/otx"
	"github.com/arloliu/otx/internal/endpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func boolPtr(b bool) *bool { return &b }

// Endpoint precedence reflects GetOTLPConfig's WHOLESALE semantics: an OTLP
// block present means the deprecated Exporter block is ignored entirely; a nil
// OTLP block falls back to deprecated Exporter; Logs.Endpoint overlays on top.
// The default also varies by protocol: grpc → localhost:4317, http → localhost:4318.
func TestEffectiveEndpoint_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *otx.TelemetryConfig
		protocol string
		want     string
	}{
		{
			name: "logs endpoint overlay wins over OTLP",
			cfg: &otx.TelemetryConfig{
				OTLP: &otx.OTLPConfig{Endpoint: "shared:4317"},
				Logs: &otx.LogsConfig{Endpoint: "logs:4318"},
			},
			protocol: protocolHTTPProtobuf,
			want:     "logs:4318",
		},
		{
			name: "OTLP block present, deprecated Exporter ignored",
			cfg: &otx.TelemetryConfig{
				OTLP:     &otx.OTLPConfig{Endpoint: "shared:4317"},
				Exporter: &otx.ExporterConfig{Endpoint: "deprecated:4317"},
			},
			protocol: protocolHTTPProtobuf,
			want:     "shared:4317",
		},
		{
			name: "nil OTLP falls back to deprecated Exporter",
			cfg: &otx.TelemetryConfig{
				Exporter: &otx.ExporterConfig{Endpoint: "deprecated:4317"},
			},
			protocol: protocolHTTPProtobuf,
			want:     "deprecated:4317",
		},
		{
			name:     "empty default for http/protobuf is localhost:4318",
			cfg:      &otx.TelemetryConfig{},
			protocol: protocolHTTPProtobuf,
			want:     defaultEndpointHTTP,
		},
		{
			name:     "empty default for grpc is localhost:4317",
			cfg:      &otx.TelemetryConfig{},
			protocol: protocolGRPC,
			want:     defaultEndpointGRPC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.cfg.GetOTLPConfig()
			assert.Equal(t, tt.want, effectiveEndpoint(tt.cfg, base, tt.protocol))
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
			got, err := buildEndpoint(tt.endpoint, tt.insecure, "4318")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildEndpoint_HostOnly verifies that a bare host without a port (e.g.
// "collector") receives the protocol default port before the scheme is prefixed.
// Without this, "collector" + grpc + insecure would become "http://collector"
// and the HTTP client would target port 80 instead of 4317.
func TestBuildEndpoint_HostOnly(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		insecure    bool
		defaultPort string
		want        string
	}{
		{
			name:        "host-only + grpc + insecure gets 4317",
			endpoint:    "collector",
			insecure:    true,
			defaultPort: "4317",
			want:        "http://collector:4317",
		},
		{
			name:        "host-only + http/protobuf + secure gets 4318",
			endpoint:    "collector",
			insecure:    false,
			defaultPort: "4318",
			want:        "https://collector:4318",
		},
		{
			name:        "host:port existing port unchanged",
			endpoint:    "collector:9999",
			insecure:    true,
			defaultPort: "4317",
			want:        "http://collector:9999",
		},
		{
			name:        "IPv6 with brackets and port unchanged",
			endpoint:    "[::1]:4317",
			insecure:    true,
			defaultPort: "4317",
			want:        "http://[::1]:4317",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildEndpoint(tt.endpoint, tt.insecure, tt.defaultPort)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildEndpoint_SchemeOverridesInsecure pins the scheme-overrides-insecure
// precedence rule from the design spec.
func TestBuildEndpoint_SchemeOverridesInsecure(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		insecure bool
		want     string
	}{
		// bare + Insecure=true  → http://
		{name: "bare + insecure=true -> http", endpoint: "h:4318", insecure: true, want: "http://h:4318"},
		// bare + Insecure=false → https://
		{name: "bare + insecure=false -> https", endpoint: "h:4318", insecure: false, want: "https://h:4318"},
		// https:// + Insecure=true → scheme wins (stays https)
		{
			name:     "https + insecure=true -> stays https",
			endpoint: "https://h:4318", insecure: true, want: "https://h:4318",
		},
		// http:// + Insecure=false → scheme wins (stays http)
		{
			name:     "http + insecure=false -> stays http",
			endpoint: "http://h:4318", insecure: false, want: "http://h:4318",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildEndpoint(tt.endpoint, tt.insecure, "4318")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildEndpoint_InvalidScheme pins that an invalid endpoint scheme surfaces
// the classifier's actionable error instead of being prefixed into garbage
// (e.g. "https://grpc://h"). This is the programmatic-config validation seam
// (NewCore propagates this error; design finding P0 #2).
func TestBuildEndpoint_InvalidScheme(t *testing.T) {
	for _, ep := range []string{"grpc://h:4317", "tcp://h:1", "GRPC://h:4317"} {
		t.Run(ep, func(t *testing.T) {
			got, err := buildEndpoint(ep, true, "4318")
			require.Error(t, err)
			assert.Empty(t, got)
			assert.Contains(t, err.Error(), "invalid endpoint scheme")
			assert.Contains(t, err.Error(), "transport is selected by protocol")
		})
	}
}

// TestSharedClassifier_Regression pins that the shared endpoint.Classify helper
// agrees with buildEndpoint's classification for bare vs URL inputs, ensuring
// no divergence between exporter.go, zaplog, and the new validation path.
func TestSharedClassifier_Regression(t *testing.T) {
	tests := []struct {
		name    string
		ep      string
		wantURL bool // endpoint.IsHTTP expected result
	}{
		{name: "bare localhost", ep: "localhost:4318", wantURL: false},
		{name: "bare IPv6", ep: "[::1]:4318", wantURL: false},
		{name: "http URL", ep: "http://h:4318", wantURL: true},
		{name: "https URL", ep: "https://h:4318", wantURL: true},
		{name: "http URL with path", ep: "http://h:4318/v1/logs", wantURL: true},
		{name: "empty", ep: "", wantURL: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantURL, endpoint.IsHTTP(tt.ep),
				"endpoint.IsHTTP(%q) disagreed with expected value", tt.ep)
		})
	}
}

// TestResolveExporterParams_EndpointClassification verifies that the SDK-level
// exporter param resolution (logs, traces, metrics) classifies bare vs URL
// endpoints the same way the shared classifier does — pinning that refactoring
// the internals to use the helper cannot silently diverge.
func TestResolveExporterParams_EndpointClassification(t *testing.T) {
	// We test the observable: effectiveEndpoint returns the value as-is, and
	// buildEndpoint prepends a scheme iff endpoint.IsHTTP is false.
	type want struct {
		endpoint string
		isURL    bool
	}
	tests := []struct {
		name     string
		logsEp   string
		insecure bool
		want     want
	}{
		{
			name:     "bare endpoint insecure -> http://",
			logsEp:   "collector:4318",
			insecure: true,
			want:     want{"http://collector:4318", false},
		},
		{
			name:     "bare endpoint secure -> https://",
			logsEp:   "collector:4318",
			insecure: false,
			want:     want{"https://collector:4318", false},
		},
		{
			name:     "http URL passes through",
			logsEp:   "http://collector:4318/v1/logs",
			insecure: false,
			want:     want{"http://collector:4318/v1/logs", true},
		},
		{
			name:     "https URL passes through (insecure ignored)",
			logsEp:   "https://collector:4318",
			insecure: true,
			want:     want{"https://collector:4318", true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &otx.TelemetryConfig{
				OTLP: &otx.OTLPConfig{
					Endpoint: tt.logsEp,
					Insecure: boolPtr(tt.insecure),
				},
			}
			base := cfg.GetOTLPConfig()
			ep := effectiveEndpoint(cfg, base, protocolHTTPProtobuf)
			// The shared classifier agrees with what buildEndpoint will do.
			assert.Equal(t, tt.want.isURL, endpoint.IsHTTP(ep),
				"classifier disagreed for %q", ep)
			// buildEndpoint produces the expected final URL.
			built, err := buildEndpoint(ep, base.IsInsecure(), "4318")
			require.NoError(t, err)
			assert.Equal(t, tt.want.endpoint, built)
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
