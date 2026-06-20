package otx

import (
	"fmt"
	"os"
	"strings"

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
	// Apply the OTel map-typed env vars otx parses itself (fuda cannot accept the
	// spec key=value form); these override file values.
	if err := applyMapEnvOverrides(&cfg); err != nil {
		return nil, err
	}
	// Post-load endpoint-scheme validation (cannot be expressed as a struct tag).
	if err := cfg.ValidateEndpoints(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ParseConfig parses TelemetryConfig from a byte slice.
// It supports YAML and JSON formats (auto-detected).
// Environment variables are also parsed and override file values.
func ParseConfig(data []byte) (*TelemetryConfig, error) {
	var cfg TelemetryConfig
	// fuda.LoadBytes handles parsing, env vars, defaults, and validation
	if err := fuda.LoadBytes(data, &cfg); err != nil {
		return nil, err
	}
	// Apply the OTel map-typed env vars otx parses itself (fuda cannot accept the
	// spec key=value form); these override file values.
	if err := applyMapEnvOverrides(&cfg); err != nil {
		return nil, err
	}
	// Post-load endpoint-scheme validation (cannot be expressed as a struct tag).
	if err := cfg.ValidateEndpoints(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyMapEnvOverrides parses the OTel map-typed environment variables that otx
// handles itself — OTEL_RESOURCE_ATTRIBUTES and OTEL_EXPORTER_OTLP_HEADERS. The
// underlying loader (fuda) only accepts a colon-delimited map form, which does
// not match the W3C/OTel "key=value,key2=value2" format these variables require;
// parsing them here keeps otx spec-compliant. When set, an env value fully
// replaces the corresponding value loaded from the config file, matching fuda's
// override semantics. OTLP/Exporter headers are applied only when their block is
// present, preserving the prior behavior of not materializing an empty block.
func applyMapEnvOverrides(cfg *TelemetryConfig) error {
	if v, ok := os.LookupEnv("OTEL_RESOURCE_ATTRIBUTES"); ok {
		m, err := parseMapEnv(v)
		if err != nil {
			return fmt.Errorf("OTEL_RESOURCE_ATTRIBUTES: %w", err)
		}
		cfg.ResourceAttributes = m
	}

	// Headers only apply to an OTLP or deprecated Exporter block. When neither is
	// present the value has nowhere to go, so it is not parsed or validated —
	// matching the prior behavior of silently dropping the env in that case (and
	// avoiding a spurious parse error for an unused variable).
	if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_HEADERS"); ok && (cfg.OTLP != nil || cfg.Exporter != nil) {
		m, err := parseMapEnv(v)
		if err != nil {
			return fmt.Errorf("OTEL_EXPORTER_OTLP_HEADERS: %w", err)
		}
		if cfg.OTLP != nil {
			cfg.OTLP.Headers = m
		}
		if cfg.Exporter != nil {
			cfg.Exporter.Headers = m
		}
	}

	return nil
}

// parseMapEnv parses an OTel-style comma-separated map env value. Each item is
// split on its first '=' (the W3C/OTel form); an item without '=' is split on
// its first ':' for backward compatibility. Surrounding whitespace around items,
// keys, and values is trimmed; empty items are skipped. An empty or
// whitespace-only input yields an empty (non-nil) map.
func parseMapEnv(value string) (map[string]string, error) {
	out := make(map[string]string)
	if strings.TrimSpace(value) == "" {
		return out, nil
	}

	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		var key, val string
		if i := strings.IndexByte(item, '='); i >= 0 {
			key, val = item[:i], item[i+1:]
		} else if i := strings.IndexByte(item, ':'); i >= 0 {
			key, val = item[:i], item[i+1:]
		} else {
			return nil, fmt.Errorf("invalid map item %q: want key=value", item)
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			return nil, fmt.Errorf("invalid map item %q: empty key", item)
		}
		out[key] = val
	}

	return out, nil
}
