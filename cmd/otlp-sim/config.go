package main

import (
	"flag"
	"time"

	"github.com/arloliu/fuda"
)

// Config holds all CLI configuration.
// Uses fuda struct tags for defaults and env var binding.
type Config struct {
	// Connection settings
	Endpoint    string `yaml:"endpoint" default:"localhost:4317" env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	UseHTTP     bool   `yaml:"http" default:"false"`
	Insecure    *bool  `yaml:"insecure" default:"true" env:"OTEL_EXPORTER_OTLP_INSECURE"`
	ServiceName string `yaml:"serviceName" env:"OTEL_SERVICE_NAME"`

	// Scenario settings
	Scenario     string `yaml:"scenario" default:"payment"`
	ScenarioFile string `yaml:"scenarioFile"`

	// Signals
	EnableLogs bool `yaml:"logs" default:"false"`

	// Quick mode
	Count int `yaml:"count" default:"10"`

	// Continuous mode
	Duration time.Duration `yaml:"duration" default:"1m"`
	Rate     float64       `yaml:"rate" default:"1"`
	Jitter   int           `yaml:"jitter" default:"20"`
}

// IsInsecure returns the insecure value, defaulting to true if nil.
func (c *Config) IsInsecure() bool {
	if c.Insecure == nil {
		return true
	}

	return *c.Insecure
}

func newConfig() *Config {
	cfg := &Config{}
	// Apply defaults from struct tags (fuda handles time.Duration and *bool parsing)
	_ = fuda.SetDefaults(cfg)

	return cfg
}

func (c *Config) bindCommonFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.Endpoint, "endpoint", c.Endpoint, "OTLP endpoint")
	fs.BoolVar(&c.UseHTTP, "http", c.UseHTTP, "Use HTTP instead of gRPC")
	fs.Func("insecure", "Skip TLS verification (default: true)", func(s string) error {
		val := s == "true" || s == "1"
		c.Insecure = &val

		return nil
	})
	fs.StringVar(&c.ServiceName, "service-name", c.ServiceName, "Override service name")
	fs.StringVar(&c.Scenario, "scenario", c.Scenario, "Scenario name")
	fs.StringVar(&c.ScenarioFile, "scenario-file", c.ScenarioFile, "Custom YAML scenario file")
	fs.BoolVar(&c.EnableLogs, "logs", c.EnableLogs, "Enable log generation")
}

func (c *Config) applyEnvOverrides() {
	// fuda.LoadEnv reads env vars based on struct tags
	// Uses pointers for optional fields so env can override non-zero defaults
	_ = fuda.LoadEnv(c)
}
