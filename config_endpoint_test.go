package otx

// Tests for endpoint-scheme validation across all five endpoint fields.
//
// Structure:
//   - TestEndpointScheme_AllFields_Valid:    valid forms × 5 fields
//   - TestEndpointScheme_AllFields_Invalid:  invalid forms × 5 fields
//   - TestEndpointScheme_ParseConfig_Integration: round-trip through ParseConfig

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endpointField names the five endpoint fields and provides a constructor that
// builds a minimal TelemetryConfig with that single field set.
type endpointField struct {
	name   string
	setter func(ep string) *TelemetryConfig
}

func allEndpointFields() []endpointField {
	return []endpointField{
		{
			name: "otlp.endpoint",
			setter: func(ep string) *TelemetryConfig {
				return &TelemetryConfig{OTLP: &OTLPConfig{Endpoint: ep}}
			},
		},
		{
			name: "traces.endpoint",
			setter: func(ep string) *TelemetryConfig {
				return &TelemetryConfig{Traces: &TracesConfig{Endpoint: ep}}
			},
		},
		{
			name: "logs.endpoint",
			setter: func(ep string) *TelemetryConfig {
				return &TelemetryConfig{Logs: &LogsConfig{Endpoint: ep}}
			},
		},
		{
			name: "metrics.endpoint",
			setter: func(ep string) *TelemetryConfig {
				return &TelemetryConfig{Metrics: &MetricsConfig{Endpoint: ep}}
			},
		},
		{
			name: "exporter.endpoint",
			setter: func(ep string) *TelemetryConfig {
				return &TelemetryConfig{Exporter: &ExporterConfig{Endpoint: ep}}
			},
		},
	}
}

// validForms lists endpoint strings that must pass validation.
var validForms = []struct {
	label string
	ep    string
}{
	{"bare localhost:port", "localhost:4318"},
	{"bare hostname no port", "collector"},
	{"bare IPv6", "[::1]:4318"},
	{"http URL", "http://h:4318"},
	{"https URL", "https://h:4318"},
	{"http URL with path", "http://h:4318/custom/path"},
	{"http URL with query", "http://h:4318/path?x=1"},
	{"mixed-case HTTP scheme", "HTTP://h:4318"},
	{"mixed-case HTTPS scheme", "HTTPS://h:4318"},
	{"empty (default applies)", ""},
}

// invalidForms lists endpoint strings that must fail validation, along with
// the actionable message fragment and the invalid scheme token.
var invalidForms = []struct {
	label  string
	ep     string
	scheme string // scheme token expected in the error
}{
	{"grpc scheme", "grpc://h:4317", "grpc"},
	{"tcp scheme", "tcp://h:1", "tcp"},
	{"unix scheme", "unix:///x.sock", "unix"},
	{"malformed ://bad", "://bad", ""},
	// Mixed-case invalid scheme: detection is case-insensitive, and the error
	// preserves the original-case scheme token ("GRPC", not "grpc").
	{"mixed-case GRPC scheme", "GRPC://h:4317", "GRPC"},
}

// TestEndpointScheme_AllFields_Valid verifies that every valid endpoint form
// passes ValidateEndpoints for every field (5 fields × 8 forms = 40 cases).
func TestEndpointScheme_AllFields_Valid(t *testing.T) {
	for _, f := range allEndpointFields() {
		for _, v := range validForms {
			name := fmt.Sprintf("%s/%s", f.name, v.label)
			t.Run(name, func(t *testing.T) {
				cfg := f.setter(v.ep)
				assert.NoError(t, cfg.ValidateEndpoints())
			})
		}
	}
}

// TestEndpointScheme_AllFields_Invalid verifies that every invalid endpoint
// form fails ValidateEndpoints for every field and carries an actionable error
// that names the field and the offending scheme
// (5 fields × 4 invalid forms = 20 cases).
func TestEndpointScheme_AllFields_Invalid(t *testing.T) {
	for _, f := range allEndpointFields() {
		for _, inv := range invalidForms {
			name := fmt.Sprintf("%s/%s", f.name, inv.label)
			t.Run(name, func(t *testing.T) {
				cfg := f.setter(inv.ep)
				err := cfg.ValidateEndpoints()
				require.Error(t, err, "expected validation error for %q on field %s", inv.ep, f.name)
				// Must name the field
				assert.Contains(t, err.Error(), f.name,
					"error should name the field; got: %s", err)
				// Must carry the actionable message
				assert.Contains(t, err.Error(), "use host:port or an http(s):// URL",
					"error should carry actionable message; got: %s", err)
				// Must name the scheme (when non-empty)
				if inv.scheme != "" {
					assert.Contains(t, err.Error(), inv.scheme,
						"error should name the offending scheme; got: %s", err)
				}
			})
		}
	}
}

// TestEndpointScheme_ParseConfig_Integration verifies that invalid endpoint
// values are rejected end-to-end through ParseConfig for each field.
func TestEndpointScheme_ParseConfig_Integration(t *testing.T) {
	type yamlCase struct {
		name    string
		yaml    string
		wantErr bool
		errFrag string
	}

	cases := []yamlCase{
		// valid forms pass through ParseConfig
		{
			name: "bare endpoint in otlp block",
			yaml: `enabled: false
serviceName: svc
otlp:
  endpoint: "localhost:4318"
`,
		},
		{
			name: "https URL in otlp block",
			yaml: `enabled: false
serviceName: svc
otlp:
  endpoint: "https://collector:4318"
`,
		},
		{
			name: "http URL with path in traces block",
			yaml: `enabled: false
serviceName: svc
traces:
  endpoint: "http://collector:4318/v1/traces"
`,
		},
		{
			name: "empty traces endpoint",
			yaml: `enabled: false
serviceName: svc
traces:
  endpoint: ""
`,
		},
		// invalid forms: each field
		{
			name: "grpc scheme in otlp block",
			yaml: `enabled: false
serviceName: svc
otlp:
  endpoint: "grpc://collector:4317"
`,
			wantErr: true,
			errFrag: `otlp.endpoint`,
		},
		{
			name: "grpc scheme in traces block",
			yaml: `enabled: false
serviceName: svc
traces:
  endpoint: "grpc://collector:4317"
`,
			wantErr: true,
			errFrag: `traces.endpoint`,
		},
		{
			name: "tcp scheme in logs block",
			yaml: `enabled: false
serviceName: svc
logs:
  endpoint: "tcp://collector:4317"
`,
			wantErr: true,
			errFrag: `logs.endpoint`,
		},
		{
			name: "unix scheme in metrics block",
			yaml: `enabled: false
serviceName: svc
metrics:
  endpoint: "unix:///var/run/collector.sock"
`,
			wantErr: true,
			errFrag: `metrics.endpoint`,
		},
		{
			name: "grpc scheme in deprecated exporter block",
			yaml: `enabled: false
serviceName: svc
exporter:
  endpoint: "grpc://collector:4317"
`,
			wantErr: true,
			errFrag: `exporter.endpoint`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.yaml))
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errFrag)
				assert.Contains(t, err.Error(), "use host:port or an http(s):// URL")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
