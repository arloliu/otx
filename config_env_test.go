package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// M5 — OTEL_RESOURCE_ATTRIBUTES / OTEL_EXPORTER_OTLP_HEADERS must accept the
// OTel-standard "key=value" form (fuda's map parser only accepts "key:value").
func TestEnvMap_ResourceAttributes_SpecKeyValue(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=shop,team=platform")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"service.namespace": "shop",
		"team":              "platform",
	}, cfg.ResourceAttributes)
}

func TestEnvMap_ResourceAttributes_LegacyColonStillWorks(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "team:platform,tier:gold")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"team": "platform", "tier": "gold"}, cfg.ResourceAttributes)
}

func TestEnvMap_ResourceAttributes_EnvReplacesFile(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "from=env")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\nresourceAttributes:\n  from: file\n  only: file\n"))
	require.NoError(t, err)
	// Env fully replaces the file map (no merge).
	assert.Equal(t, map[string]string{"from": "env"}, cfg.ResourceAttributes)
}

func TestEnvMap_OTLPHeaders_SpecKeyValue(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "api-key=secret,x-tenant=acme")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\notlp:\n  endpoint: localhost:4317\n"))
	require.NoError(t, err)
	require.NotNil(t, cfg.OTLP)
	assert.Equal(t, map[string]string{"api-key": "secret", "x-tenant": "acme"}, cfg.OTLP.Headers)
}

func TestEnvMap_OTLPHeaders_ValueMayContainEquals(t *testing.T) {
	// Auth headers commonly carry '=' (base64); split must use the FIRST '='.
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer abc==")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\notlp:\n  endpoint: localhost:4317\n"))
	require.NoError(t, err)
	require.NotNil(t, cfg.OTLP)
	assert.Equal(t, map[string]string{"authorization": "Bearer abc=="}, cfg.OTLP.Headers)
}

func TestEnvMap_OTLPHeaders_NoBlockLeavesOTLPNil(t *testing.T) {
	// Preserves prior behavior: headers env does not materialize an OTLP block.
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "api-key=secret")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\n"))
	require.NoError(t, err)
	assert.Nil(t, cfg.OTLP)
}

func TestEnvMap_OTLPHeaders_MalformedNoBlock_NoError(t *testing.T) {
	// With no otlp/exporter block the header env has nowhere to apply, so even a
	// malformed value must not fail loading (it is simply ignored).
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "no-delimiter")

	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\n"))
	require.NoError(t, err)
	assert.Nil(t, cfg.OTLP)
}

func TestEnvMap_Invalid_ReturnsError(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "no-delimiter-here")

	_, err := ParseConfig([]byte("enabled: true\nserviceName: s\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OTEL_RESOURCE_ATTRIBUTES")
}

// Unit coverage of the parser itself.
func TestParseMapEnv(t *testing.T) {
	t.Run("spec key=value", func(t *testing.T) {
		m, err := parseMapEnv("a=1,b=2")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, m)
	})

	t.Run("legacy key:value", func(t *testing.T) {
		m, err := parseMapEnv("a:1,b:2")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, m)
	})

	t.Run("first equals wins for value", func(t *testing.T) {
		m, err := parseMapEnv("k=a=b")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"k": "a=b"}, m)
	})

	t.Run("whitespace trimmed, empty items skipped", func(t *testing.T) {
		m, err := parseMapEnv(" a = 1 , , b = 2 ")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, m)
	})

	t.Run("empty input yields empty non-nil map", func(t *testing.T) {
		m, err := parseMapEnv("   ")
		require.NoError(t, err)
		assert.NotNil(t, m)
		assert.Empty(t, m)
	})

	t.Run("missing delimiter errors", func(t *testing.T) {
		_, err := parseMapEnv("bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key=value")
	})

	t.Run("empty key errors", func(t *testing.T) {
		_, err := parseMapEnv("=value")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key")
	})
}

// M4 — environment variables override file values across scalar, *bool, and
// float fields (only OTEL_SERVICE_NAME was previously exercised).
func TestEnvPrecedence_OverridesFileValues(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "envhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("OTEL_PROPAGATORS", "tracecontext")

	cfg, err := ParseConfig([]byte(`
enabled: true
serviceName: s
otlp:
  endpoint: filehost:4317
  protocol: http
  insecure: true
sampling:
  sampler: always_on
  samplerArg: 1.0
propagation:
  propagators: tracecontext,baggage
`))
	require.NoError(t, err)

	assert.Equal(t, "envhost:4317", cfg.OTLP.Endpoint, "string env wins")
	assert.Equal(t, "grpc", cfg.OTLP.Protocol)
	require.NotNil(t, cfg.OTLP.Insecure)
	assert.False(t, *cfg.OTLP.Insecure, "*bool env wins")
	assert.Equal(t, "always_off", cfg.Sampling.Sampler)
	assert.InDelta(t, 0.25, cfg.Sampling.SamplerArg, 1e-9, "float env wins")
	assert.Equal(t, "tracecontext", cfg.Propagation.Propagators)
}

// M4 — when neither file nor env sets a value, struct-tag defaults fill in.
func TestEnvPrecedence_DefaultsWhenUnset(t *testing.T) {
	cfg, err := ParseConfig([]byte("enabled: true\nserviceName: s\notlp:\n  endpoint: localhost:4317\n"))
	require.NoError(t, err)

	require.NotNil(t, cfg.OTLP.Insecure)
	assert.True(t, *cfg.OTLP.Insecure, "insecure default true")
	assert.Equal(t, "http/protobuf", cfg.OTLP.Protocol, "protocol default")
}
