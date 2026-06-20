package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// M13 — struct-tag validation fires through ParseConfig. The audit originally
// suspected required_if on ServiceName was enforced only at build time; these
// tests pin that it (and the other oneof/range tags) are enforced at parse.

func TestValidation_RequiredIfServiceName(t *testing.T) {
	t.Run("enabled true without serviceName fails", func(t *testing.T) {
		_, err := ParseConfig([]byte("enabled: true\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ServiceName")
		assert.Contains(t, err.Error(), "required_if")
	})

	t.Run("enabled false without serviceName is allowed", func(t *testing.T) {
		cfg, err := ParseConfig([]byte("enabled: false\n"))
		require.NoError(t, err)
		assert.Empty(t, cfg.ServiceName)
	})
}

func TestValidation_SamplerArgRange(t *testing.T) {
	cases := []struct {
		name string
		arg  string
		ok   bool
	}{
		{name: "below range", arg: "-0.1", ok: false},
		{name: "lower bound", arg: "0", ok: true},
		{name: "upper bound", arg: "1", ok: true},
		{name: "above range", arg: "1.5", ok: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "enabled: true\nserviceName: s\nsampling:\n  sampler: traceidratio\n  samplerArg: " + tt.arg + "\n"
			cfg, err := ParseConfig([]byte(yaml))
			if tt.ok {
				require.NoError(t, err)
				require.NotNil(t, cfg.Sampling)

				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "SamplerArg")
		})
	}
}

func TestValidation_OTLPProtocolOneof(t *testing.T) {
	_, err := ParseConfig([]byte("enabled: true\nserviceName: s\notlp:\n  protocol: bogus\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Protocol")
	assert.Contains(t, err.Error(), "oneof")
}

func TestValidation_OTLPTimeoutGte(t *testing.T) {
	_, err := ParseConfig([]byte("enabled: true\nserviceName: s\notlp:\n  timeout: -1s\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Timeout")
}

func TestValidation_TracesExporterOneof(t *testing.T) {
	_, err := ParseConfig([]byte("enabled: true\nserviceName: s\ntraces:\n  exporter: bogus\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Exporter")
	assert.Contains(t, err.Error(), "oneof")
}
