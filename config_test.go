package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEnabled(t *testing.T) {
	assert.False(t, (*TelemetryConfig)(nil).IsEnabled())
	assert.False(t, (&TelemetryConfig{}).IsEnabled())
	assert.True(t, (&TelemetryConfig{Enabled: boolPtr(true)}).IsEnabled())
}
