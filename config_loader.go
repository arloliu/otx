package otx

import (
	"github.com/arloliu/fuda"
)

// LoadConfig loads TelemetryConfig from a file path.
// It supports YAML and JSON formats.
// Environment variables are also parsed and override file values.
func LoadConfig(path string) (*TelemetryConfig, error) {
	var cfg TelemetryConfig
	// fuda.LoadFile handles reading, parsing, env vars, defaults, and validation
	if err := fuda.LoadFile(path, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ParseConfig parsers TelemetryConfig from a byte slice.
// It supports YAML and JSON formats (auto-detected).
// Environment variables are also parsed and override file values.
func ParseConfig(data []byte) (*TelemetryConfig, error) {
	var cfg TelemetryConfig
	// fuda.LoadBytes handles parsing, env vars, defaults, and validation
	if err := fuda.LoadBytes(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
