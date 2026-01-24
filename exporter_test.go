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

func TestSplitEndpointURL(t *testing.T) {
	host, path := splitEndpointURL("http://localhost:4318/v1/traces")
	assert.Equal(t, "localhost:4318", host)
	assert.Equal(t, "/v1/traces", path)

	host, path = splitEndpointURL("https://example.com")
	assert.Equal(t, "example.com", host)
	assert.Equal(t, "", path)

	host, path = splitEndpointURL("localhost:4317")
	assert.Equal(t, "", host)
	assert.Equal(t, "", path)
}

func TestBuildHTTPOptions(t *testing.T) {
	params := exporterParams{
		Endpoint:    "http://localhost:4318/v1/logs",
		Headers:     map[string]string{"k": "v"},
		Timeout:     5 * time.Second,
		Insecure:    true,
		Compression: "gzip",
	}

	opts := buildHTTPOptions(
		params,
		func(v string) opt { return opt{kind: "endpoint", val: v} },
		func(v string) opt { return opt{kind: "endpointURL", val: v} },
		func(_ map[string]string) opt { return opt{kind: "headers"} },
		func(d time.Duration) opt { return opt{kind: "timeout", val: d.String()} },
		func() opt { return opt{kind: "insecure"} },
		func() opt { return opt{kind: "compression"} },
	)

	require.NotEmpty(t, opts)
	assert.Equal(t, "endpointURL", opts[0].kind)
	assert.Contains(t, kinds(opts), "headers")
	assert.Contains(t, kinds(opts), "timeout")
	assert.Contains(t, kinds(opts), "insecure")
	assert.Contains(t, kinds(opts), "compression")

	params.Endpoint = "localhost:4317"
	opts = buildHTTPOptions(
		params,
		func(v string) opt { return opt{kind: "endpoint", val: v} },
		func(v string) opt { return opt{kind: "endpointURL", val: v} },
		func(_ map[string]string) opt { return opt{kind: "headers"} },
		func(d time.Duration) opt { return opt{kind: "timeout", val: d.String()} },
		func() opt { return opt{kind: "insecure"} },
		func() opt { return opt{kind: "compression"} },
	)
	assert.Equal(t, "endpoint", opts[0].kind)
}

func TestBuildGRPCOptions(t *testing.T) {
	params := exporterParams{
		Endpoint:    "localhost:4317",
		Headers:     map[string]string{"k": "v"},
		Timeout:     2 * time.Second,
		Insecure:    true,
		Compression: "gzip",
	}

	opts := buildGRPCOptions(
		params,
		func(v string) opt { return opt{kind: "endpoint", val: v} },
		func(_ map[string]string) opt { return opt{kind: "headers"} },
		func(d time.Duration) opt { return opt{kind: "timeout", val: d.String()} },
		func() opt { return opt{kind: "insecure"} },
		func() opt { return opt{kind: "compression"} },
	)

	require.NotEmpty(t, opts)
	assert.Equal(t, "endpoint", opts[0].kind)
	assert.Contains(t, kinds(opts), "headers")
	assert.Contains(t, kinds(opts), "timeout")
	assert.Contains(t, kinds(opts), "insecure")
	assert.Contains(t, kinds(opts), "compression")
}

func kinds(opts []opt) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.kind)
	}

	return out
}
