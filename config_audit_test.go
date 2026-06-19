package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBoolPtr(t *testing.T) {
	p := BoolPtr(true)
	assert.NotNil(t, p)
	assert.True(t, *p)

	q := BoolPtr(false)
	assert.NotNil(t, q)
	assert.False(t, *q)
}

func TestGetOTLPEndpoint_MatchesBuilders(t *testing.T) {
	cases := []struct {
		name string
		cfg  *TelemetryConfig
		want string
	}{
		{
			name: "nil config defaults to http port",
			cfg:  nil,
			want: "localhost:4318",
		},
		{
			name: "traces endpoint wins",
			cfg: &TelemetryConfig{
				Traces: &TracesConfig{Endpoint: "traces:4318"},
				OTLP:   &OTLPConfig{Endpoint: "otlp:4318"},
			},
			want: "traces:4318",
		},
		{
			name: "otlp endpoint used",
			cfg:  &TelemetryConfig{OTLP: &OTLPConfig{Endpoint: "otlp:4318"}},
			want: "otlp:4318",
		},
		{
			// Regression for the accessor/builder divergence: a non-nil but
			// empty OTLP block must NOT fall back to the deprecated Exporter
			// endpoint, because GetOTLPConfig (used by the builders) returns the
			// empty OTLP block and defaults. Accessor and builder must agree.
			name: "non-nil-empty OTLP ignores Exporter fallback",
			cfg: &TelemetryConfig{
				OTLP:     &OTLPConfig{},
				Exporter: &ExporterConfig{Endpoint: "deprecated:4317"},
			},
			want: "localhost:4318",
		},
		{
			// When OTLP is nil, GetOTLPConfig DOES convert the deprecated
			// Exporter block, so the accessor should surface its endpoint.
			name: "nil OTLP falls back to Exporter via GetOTLPConfig",
			cfg: &TelemetryConfig{
				Exporter: &ExporterConfig{Endpoint: "deprecated:4317", Protocol: "grpc"},
			},
			want: "deprecated:4317",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetOTLPEndpoint()
			assert.Equal(t, tt.want, got)

			// The accessor must never diverge from the effective OTLP config the
			// builders consume.
			otlp := tt.cfg.GetOTLPConfig()
			if tt.cfg != nil && tt.cfg.Traces != nil && tt.cfg.Traces.Endpoint != "" {
				return // Traces.Endpoint override is accessor-only by design.
			}
			if otlp.Endpoint != "" {
				assert.Equal(t, otlp.Endpoint, got)
			} else {
				assert.Equal(t, defaultEndpointFor(otlp.Protocol), got)
			}
		})
	}
}
