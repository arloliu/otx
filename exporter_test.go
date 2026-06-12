package otx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type opt struct {
	kind string
	val  string
}

func TestNormalizeExporterType(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "otlp"},
		{name: "stdout", input: "stdout", want: "console"},
		{name: "noop", input: "noop", want: "nop"},
		{name: "mixed case", input: "OTLP", want: "otlp"},
		{name: "passthrough", input: "console", want: "console"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeExporterType(tt.input))
		})
	}
}

// callBuildHTTPOptions exercises the generic helper with mock constructors that
// record which option was selected. This is the single inspectable seam shared
// by the trace, log, and metric HTTP exporters (all three route through
// buildHTTPOptions): the helper decides endpoint-vs-endpointURL and whether to
// append WithInsecure. The actual scheme→TLS flip happens inside the SDK's
// WithEndpointURL (otlploghttp/otlpmetrichttp insecureFromScheme;
// otlptracehttp Insecure = scheme != "https"), which the helper cannot observe
// — so we pin the option *shape* the SDK then resolves.
func callBuildHTTPOptions(params exporterParams) []opt {
	return buildHTTPOptions(
		params,
		func(v string) opt { return opt{kind: "endpoint", val: v} },
		func(v string) opt { return opt{kind: "endpointURL", val: v} },
		func(_ map[string]string) opt { return opt{kind: "headers"} },
		func(d time.Duration) opt { return opt{kind: "timeout", val: d.String()} },
		func() opt { return opt{kind: "insecure"} },
		func() opt { return opt{kind: "compression"} },
	)
}

// TestBuildHTTPOptions_SchemePrecedence pins the scheme-overrides-Insecure rule
// on the SDK HTTP path for all three signals (they share this helper):
//   - scheme-bearing URL → WithEndpointURL chosen, WithInsecure NEVER appended
//     (the scheme alone decides TLS via the SDK; a later WithInsecure would
//     override it because options apply in order). https+Insecure=true stays
//     secure; http+Insecure=false stays insecure.
//   - bare host:port → WithEndpoint chosen, WithInsecure appended iff Insecure.
func TestBuildHTTPOptions_SchemePrecedence(t *testing.T) {
	cases := []struct {
		name         string
		endpoint     string
		insecure     bool
		wantFirst    string // "endpoint" or "endpointURL"
		wantInsecure bool   // whether WithInsecure must be present
	}{
		{
			name:      "https URL with Insecure=true stays secure (no WithInsecure)",
			endpoint:  "https://collector:4318/v1/logs",
			insecure:  true,
			wantFirst: "endpointURL", wantInsecure: false,
		},
		{
			name:      "http URL with Insecure=false stays insecure (no WithInsecure, scheme decides)",
			endpoint:  "http://collector:4318/v1/logs",
			insecure:  false,
			wantFirst: "endpointURL", wantInsecure: false,
		},
		{
			name:      "bare host:port with Insecure=true appends WithInsecure",
			endpoint:  "collector:4318",
			insecure:  true,
			wantFirst: "endpoint", wantInsecure: true,
		},
		{
			name:      "bare host:port with Insecure=false omits WithInsecure",
			endpoint:  "collector:4318",
			insecure:  false,
			wantFirst: "endpoint", wantInsecure: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			opts := callBuildHTTPOptions(exporterParams{
				Endpoint: tt.endpoint,
				Insecure: tt.insecure,
			})

			require.NotEmpty(t, opts)
			assert.Equal(t, tt.wantFirst, opts[0].kind, "endpoint selector")
			if tt.wantInsecure {
				assert.Contains(t, kinds(opts), "insecure",
					"bare endpoint must carry WithInsecure when Insecure=true")
			} else {
				assert.NotContains(t, kinds(opts), "insecure",
					"URL endpoints (or secure bare) must not append WithInsecure")
			}
		})
	}
}

// TestBuildHTTPOptions_MixedCaseScheme pins that a case-variant URL scheme is
// still treated as a URL (endpointURL chosen, no WithInsecure) — the classifier
// is case-insensitive.
func TestBuildHTTPOptions_MixedCaseScheme(t *testing.T) {
	opts := callBuildHTTPOptions(exporterParams{
		Endpoint: "HTTPS://collector:4318/v1/logs",
		Insecure: true,
	})
	require.NotEmpty(t, opts)
	assert.Equal(t, "endpointURL", opts[0].kind)
	assert.NotContains(t, kinds(opts), "insecure")
}

func TestBuildHTTPOptions_PassesThroughHeadersTimeoutCompression(t *testing.T) {
	opts := callBuildHTTPOptions(exporterParams{
		Endpoint:    "http://localhost:4318/v1/logs",
		Headers:     map[string]string{"k": "v"},
		Timeout:     5 * time.Second,
		Insecure:    true,
		Compression: "gzip",
	})

	require.NotEmpty(t, opts)
	assert.Equal(t, "endpointURL", opts[0].kind)
	assert.Contains(t, kinds(opts), "headers")
	assert.Contains(t, kinds(opts), "timeout")
	assert.Contains(t, kinds(opts), "compression")
	// URL endpoint: scheme decides TLS, so no WithInsecure even with Insecure=true.
	assert.NotContains(t, kinds(opts), "insecure")
}

// callBuildGRPCOptions exercises the generic helper with mock constructors that
// record which option was selected. This is the single inspectable seam shared
// by the trace, log, and metric gRPC exporters (all three route through
// buildGRPCOptions): the helper decides endpoint-vs-endpointURL and whether to
// append WithInsecure. The actual scheme→TLS flip happens inside the SDK's
// WithEndpointURL, which the helper cannot observe — so we pin the option
// *shape* the SDK then resolves.
func callBuildGRPCOptions(params exporterParams) []opt {
	return buildGRPCOptions(
		params,
		func(v string) opt { return opt{kind: "endpoint", val: v} },
		func(v string) opt { return opt{kind: "endpointURL", val: v} },
		func(_ map[string]string) opt { return opt{kind: "headers"} },
		func(d time.Duration) opt { return opt{kind: "timeout", val: d.String()} },
		func() opt { return opt{kind: "insecure"} },
		func() opt { return opt{kind: "compression"} },
	)
}

func TestBuildGRPCOptions(t *testing.T) {
	params := exporterParams{
		Endpoint:    "localhost:4317",
		Headers:     map[string]string{"k": "v"},
		Timeout:     2 * time.Second,
		Insecure:    true,
		Compression: "gzip",
	}

	opts := callBuildGRPCOptions(params)

	require.NotEmpty(t, opts)
	assert.Equal(t, "endpoint", opts[0].kind)
	assert.Contains(t, kinds(opts), "headers")
	assert.Contains(t, kinds(opts), "timeout")
	assert.Contains(t, kinds(opts), "insecure")
	assert.Contains(t, kinds(opts), "compression")
}

// TestBuildGRPCOptions_SchemePrecedence pins the scheme-overrides-Insecure rule
// on the SDK gRPC path for all three signals (they share this helper):
//   - scheme-bearing URL → WithEndpointURL chosen, WithInsecure NEVER appended
//     (the scheme alone decides TLS via the SDK; a later WithInsecure would
//     override it because options apply in order). https+Insecure=true stays
//     secure; http+Insecure=false stays insecure.
//   - bare host:port → WithEndpoint chosen, WithInsecure appended iff Insecure.
func TestBuildGRPCOptions_SchemePrecedence(t *testing.T) {
	cases := []struct {
		name         string
		endpoint     string
		insecure     bool
		wantFirst    string // "endpoint" or "endpointURL"
		wantInsecure bool   // whether WithInsecure must be present
	}{
		{
			name:      "https URL with Insecure=true stays secure (no WithInsecure)",
			endpoint:  "https://collector:4317",
			insecure:  true,
			wantFirst: "endpointURL", wantInsecure: false,
		},
		{
			name:      "http URL with Insecure=false stays insecure (no WithInsecure, scheme decides)",
			endpoint:  "http://collector:4317",
			insecure:  false,
			wantFirst: "endpointURL", wantInsecure: false,
		},
		{
			name:      "bare host:port with Insecure=true appends WithInsecure",
			endpoint:  "collector:4317",
			insecure:  true,
			wantFirst: "endpoint", wantInsecure: true,
		},
		{
			name:      "bare host:port with Insecure=false omits WithInsecure",
			endpoint:  "collector:4317",
			insecure:  false,
			wantFirst: "endpoint", wantInsecure: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			opts := callBuildGRPCOptions(exporterParams{
				Endpoint: tt.endpoint,
				Insecure: tt.insecure,
			})

			require.NotEmpty(t, opts)
			assert.Equal(t, tt.wantFirst, opts[0].kind, "endpoint selector")
			if tt.wantInsecure {
				assert.Contains(t, kinds(opts), "insecure",
					"bare endpoint must carry WithInsecure when Insecure=true")
			} else {
				assert.NotContains(t, kinds(opts), "insecure",
					"URL endpoints (or secure bare) must not append WithInsecure")
			}
		})
	}
}

func kinds(opts []opt) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.kind)
	}

	return out
}
