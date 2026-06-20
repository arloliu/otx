package otx

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// L8 — malformed input and missing-file paths must return errors, not panic or
// silently succeed.

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Error(t, err)
}

func TestParseConfig_MalformedYAML(t *testing.T) {
	// Unterminated/"broken" YAML that cannot parse.
	_, err := ParseConfig([]byte("enabled: true\n\tbad: : :\n"))
	require.Error(t, err)
}

func TestParseConfig_WrongScalarType(t *testing.T) {
	// enabled is a *bool; a non-boolean scalar must fail to parse.
	_, err := ParseConfig([]byte("enabled: not-a-bool\nserviceName: s\n"))
	require.Error(t, err)
}

func TestParseConfig_EmptyInputUsesDefaults(t *testing.T) {
	// Boundary: empty document is valid and yields defaults (telemetry disabled).
	cfg, err := ParseConfig([]byte("{}"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Enabled)
	assert.False(t, *cfg.Enabled)
}
