//revive:disable:line-length-limit
package otx

import (
	"slices"
	"strings"
	"time"
)

// TelemetryConfig configures the OpenTelemetry system.
// Environment variable names follow the OTel specification:
// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/
type TelemetryConfig struct {
	// Enabled controls whether the OTX telemetry system is active.
	Enabled *bool `yaml:"enabled" default:"false" env:"OTX_ENABLED"`

	// ServiceName is the name of the service for telemetry identification.
	// Maps to OTEL_SERVICE_NAME.
	ServiceName string `yaml:"serviceName" env:"OTEL_SERVICE_NAME" validate:"required_if=Enabled true"`

	// Version is the service version (e.g., git commit or semantic version).
	// Used in service.version resource attribute.
	Version string `yaml:"version" env:"OTEL_SERVICE_VERSION"`

	// Environment is the deployment environment (e.g., production, development).
	// Used in deployment.environment resource attribute.
	Environment string `yaml:"environment" env:"OTEL_DEPLOYMENT_ENVIRONMENT" default:"development"`

	// ResourceAttributes contains additional resource attributes as key=value pairs.
	// Maps to OTEL_RESOURCE_ATTRIBUTES (comma-separated key=value pairs).
	ResourceAttributes map[string]string `yaml:"resourceAttributes,omitempty" env:"OTEL_RESOURCE_ATTRIBUTES"`

	// OTLP contains shared OTLP exporter settings used by all signals (traces, logs, metrics).
	// Signal-specific settings can override these.
	OTLP *OTLPConfig `yaml:"otlp,omitempty"`

	// Traces configures the tracing subsystem.
	Traces *TracesConfig `yaml:"traces,omitempty"`

	// Logs configures the logging subsystem (OTel log bridge).
	// Used by shared/logging's WithLoggerProvider integration.
	Logs *LogsConfig `yaml:"logs,omitempty"`

	// Metrics configures the metrics subsystem.
	// Provides MeterProvider for application metrics.
	Metrics *MetricsConfig `yaml:"metrics,omitempty"`

	// Propagation configures context propagation (W3C TraceContext, Baggage).
	// Maps to OTEL_PROPAGATORS.
	Propagation *PropConfig `yaml:"propagation,omitempty"`

	// Deprecated: Use Traces.Sampling instead. Kept for backward compatibility.
	Sampling *SamplingConfig `yaml:"sampling,omitempty"`

	// Deprecated: Use OTLP or Traces.Exporter instead. Kept for backward compatibility.
	Exporter *ExporterConfig `yaml:"exporter,omitempty"`
}

// OTLPConfig contains shared OTLP exporter settings.
// These settings apply to all signals unless overridden by signal-specific config.
type OTLPConfig struct {
	// Endpoint is the OTLP collector endpoint.
	// Maps to OTEL_EXPORTER_OTLP_ENDPOINT.
	//
	// Format depends on protocol:
	//   - gRPC: "host:port" (e.g., "localhost:4317"). Do NOT include scheme.
	//   - HTTP: Full URL with scheme (e.g., "http://localhost:4318/v1/traces").
	//
	// Using the wrong format may cause connection failures or unexpected behavior.
	Endpoint string `yaml:"endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"localhost:4317"`

	// Insecure disables TLS for the OTLP connection.
	// Maps to OTEL_EXPORTER_OTLP_INSECURE.
	Insecure *bool `yaml:"insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" default:"true"`

	// Headers adds custom headers to OTLP requests.
	// Maps to OTEL_EXPORTER_OTLP_HEADERS (comma-separated key=value pairs).
	// Avoid logging this value, as it may contain sensitive credentials.
	Headers map[string]string `yaml:"headers,omitempty" env:"OTEL_EXPORTER_OTLP_HEADERS"`

	// Protocol determines the OTLP transport protocol.
	// Maps to OTEL_EXPORTER_OTLP_PROTOCOL.
	// Options: "grpc", "http/protobuf", "http".
	Protocol string `yaml:"protocol" env:"OTEL_EXPORTER_OTLP_PROTOCOL" default:"grpc" validate:"oneof=grpc http/protobuf http"`

	// Timeout is the timeout for exporter operations.
	// Maps to OTEL_EXPORTER_OTLP_TIMEOUT.
	Timeout time.Duration `yaml:"timeout" env:"OTEL_EXPORTER_OTLP_TIMEOUT" default:"10s" validate:"gte=0"`

	// Compression sets the compression algorithm for OTLP.
	// Maps to OTEL_EXPORTER_OTLP_COMPRESSION.
	// Options: "gzip", "none".
	Compression string `yaml:"compression,omitempty" env:"OTEL_EXPORTER_OTLP_COMPRESSION" validate:"omitempty,oneof=gzip none"`
}

// IsInsecure returns true if insecure connection is enabled.
func (c *OTLPConfig) IsInsecure() bool {
	return c == nil || c.Insecure == nil || *c.Insecure
}

// TracesConfig configures the tracing subsystem.
type TracesConfig struct {
	// Enabled controls whether tracing is active. Defaults to true if parent is enabled.
	Enabled *bool `yaml:"enabled" default:"true"`

	// Exporter determines the trace exporter type.
	// Maps to OTEL_TRACES_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Exporter string `yaml:"exporter" env:"OTEL_TRACES_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint overrides OTLP.Endpoint for traces.
	// Maps to OTEL_EXPORTER_OTLP_TRACES_ENDPOINT.
	// In most cases, leave this empty and set OTLP.Endpoint instead.
	// Only use this when traces need a different endpoint than other signals.
	Endpoint string `yaml:"endpoint,omitempty" env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"`

	// Sampling configures the trace sampling strategy.
	Sampling *SamplingConfig `yaml:"sampling,omitempty"`
}

// IsEnabled returns true if tracing is enabled.
func (c *TracesConfig) IsEnabled() bool {
	return c == nil || c.Enabled == nil || *c.Enabled
}

// LogsConfig configures the logging subsystem (OTel log bridge).
// This integrates with shared/logging via WithLoggerProvider.
type LogsConfig struct {
	// Enabled controls whether OTel log export is active.
	// Defaults to false (opt-in for logs).
	Enabled *bool `yaml:"enabled" default:"false"`

	// Exporter determines the log exporter type.
	// Maps to OTEL_LOGS_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Exporter string `yaml:"exporter" env:"OTEL_LOGS_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint overrides OTLP.Endpoint for logs.
	// Maps to OTEL_EXPORTER_OTLP_LOGS_ENDPOINT.
	// In most cases, leave this empty and set OTLP.Endpoint instead.
	// Only use this when logs need a different endpoint than other signals.
	Endpoint string `yaml:"endpoint,omitempty" env:"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"`
}

// IsEnabled returns true if OTel log export is enabled.
func (c *LogsConfig) IsEnabled() bool {
	return c != nil && c.Enabled != nil && *c.Enabled
}

// MetricsConfig configures the metrics subsystem.
type MetricsConfig struct {
	// Enabled controls whether metrics collection is active.
	// Defaults to false (opt-in for metrics).
	Enabled *bool `yaml:"enabled" default:"false"`

	// Exporter determines the metrics exporter type.
	// Maps to OTEL_METRICS_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Exporter string `yaml:"exporter" env:"OTEL_METRICS_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint overrides OTLP.Endpoint for metrics.
	// Maps to OTEL_EXPORTER_OTLP_METRICS_ENDPOINT.
	// In most cases, leave this empty and set OTLP.Endpoint instead.
	// Only use this when metrics need a different endpoint than other signals.
	Endpoint string `yaml:"endpoint,omitempty" env:"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"`

	// Interval is the export interval for periodic metric reader.
	// Maps to OTEL_METRIC_EXPORT_INTERVAL (milliseconds if numeric).
	// Defaults to 60s.
	Interval time.Duration `yaml:"interval,omitempty" env:"OTEL_METRIC_EXPORT_INTERVAL" default:"60s" validate:"omitempty,gt=0"`
}

// IsEnabled returns true if metrics collection is enabled.
func (c *MetricsConfig) IsEnabled() bool {
	return c != nil && c.Enabled != nil && *c.Enabled
}

// SamplingConfig configures the trace sampling strategy.
// Maps to OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG.
type SamplingConfig struct {
	// Sampler determines which sampler to use.
	// Maps to OTEL_TRACES_SAMPLER.
	// Options: "always_on", "always_off", "traceidratio",
	// "parentbased_always_on", "parentbased_always_off", "parentbased_traceidratio".
	// Defaults to "parentbased_always_on" (OTel default).
	Sampler string `yaml:"sampler" env:"OTEL_TRACES_SAMPLER" default:"parentbased_always_on" validate:"oneof=always_on always_off traceidratio parentbased_always_on parentbased_always_off parentbased_traceidratio"`

	// SamplerArg is the argument for ratio-based samplers.
	// Maps to OTEL_TRACES_SAMPLER_ARG.
	// For traceidratio and parentbased_traceidratio: sampling probability 0.0 to 1.0.
	// Values outside [0.0, 1.0] have undefined behavior.
	// Defaults to 1.0 (100%).
	SamplerArg float64 `yaml:"samplerArg" env:"OTEL_TRACES_SAMPLER_ARG" default:"1.0" validate:"gte=0,lte=1"`
}

// ExporterConfig configures the trace exporter.
// Deprecated: Use OTLPConfig for shared settings and TracesConfig.Exporter for type.
// Kept for backward compatibility.
type ExporterConfig struct {
	// Type determines the exporter implementation.
	// Maps to OTEL_TRACES_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Type string `yaml:"type" env:"OTEL_TRACES_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint is the OTLP collector endpoint.
	Endpoint string `yaml:"endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"localhost:4317"`

	// Insecure disables TLS for the OTLP connection.
	Insecure *bool `yaml:"insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" default:"true"`

	// Headers adds custom headers to OTLP requests.
	// Avoid logging this value, as it may contain sensitive credentials.
	Headers map[string]string `yaml:"headers,omitempty" env:"OTEL_EXPORTER_OTLP_HEADERS"`

	// Protocol determines the OTLP transport protocol.
	Protocol string `yaml:"protocol" env:"OTEL_EXPORTER_OTLP_PROTOCOL" default:"grpc" validate:"omitempty,oneof=grpc http/protobuf http"`

	// Timeout is the timeout for exporter operations.
	Timeout time.Duration `yaml:"timeout" env:"OTEL_EXPORTER_OTLP_TIMEOUT" default:"10s" validate:"gte=0"`

	// Compression sets the compression algorithm for OTLP.
	Compression string `yaml:"compression,omitempty" env:"OTEL_EXPORTER_OTLP_COMPRESSION" validate:"omitempty,oneof=gzip none"`
}

// IsInsecure returns true if insecure connection is enabled.
// Defaults to true if nil.
func (c *ExporterConfig) IsInsecure() bool {
	return c == nil || c.Insecure == nil || *c.Insecure
}

// PropConfig configures context propagation.
// Maps to OTEL_PROPAGATORS.
type PropConfig struct {
	// Propagators specifies which propagators to use.
	// Maps to OTEL_PROPAGATORS (comma-separated list).
	// Known values: "tracecontext", "baggage", "b3", "b3multi", "jaeger", "xray", "none".
	// Defaults to "tracecontext,baggage" (W3C standards).
	Propagators string `yaml:"propagators" env:"OTEL_PROPAGATORS" default:"tracecontext,baggage"`
}

// HasTraceContext returns true if tracecontext propagator is enabled.
func (c *PropConfig) HasTraceContext() bool {
	if c == nil || c.Propagators == "" {
		return true // default includes tracecontext
	}

	return containsPropagator(c.Propagators, "tracecontext")
}

// HasBaggage returns true if baggage propagator is enabled.
func (c *PropConfig) HasBaggage() bool {
	if c == nil || c.Propagators == "" {
		return true // default includes baggage
	}

	return containsPropagator(c.Propagators, "baggage")
}

// containsPropagator checks if a propagator is in the comma-separated list.
func containsPropagator(propagators, name string) bool {
	return slices.Contains(splitPropagators(propagators), name)
}

// splitPropagators splits a comma-separated propagator list.
func splitPropagators(propagators string) []string {
	if propagators == "" {
		return nil
	}

	var result []string
	for p := range strings.SplitSeq(propagators, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result
}

// IsEnabled returns true if telemetry is enabled.
// Defaults to false if nil.
func (c *TelemetryConfig) IsEnabled() bool {
	return c != nil && c.Enabled != nil && *c.Enabled
}

// GetSamplingConfig returns the effective sampling config.
// Prefers Traces.Sampling, falls back to deprecated Sampling field.
func (c *TelemetryConfig) GetSamplingConfig() *SamplingConfig {
	if c == nil {
		return nil
	}
	if c.Traces != nil && c.Traces.Sampling != nil {
		return c.Traces.Sampling
	}

	return c.Sampling // backward compatibility
}

// GetTracesExporter returns the effective traces exporter type.
// Prefers Traces.Exporter, falls back to deprecated Exporter.Type.
func (c *TelemetryConfig) GetTracesExporter() string {
	if c == nil {
		return "otlp"
	}
	if c.Traces != nil && c.Traces.Exporter != "" {
		return c.Traces.Exporter
	}
	if c.Exporter != nil && c.Exporter.Type != "" {
		return c.Exporter.Type
	}

	return "otlp"
}

// GetOTLPEndpoint returns the effective OTLP endpoint for traces.
// Priority: Traces.Endpoint > OTLP.Endpoint > Exporter.Endpoint (deprecated).
func (c *TelemetryConfig) GetOTLPEndpoint() string {
	if c == nil {
		return "localhost:4317"
	}
	if c.Traces != nil && c.Traces.Endpoint != "" {
		return c.Traces.Endpoint
	}
	if c.OTLP != nil && c.OTLP.Endpoint != "" {
		return c.OTLP.Endpoint
	}
	if c.Exporter != nil && c.Exporter.Endpoint != "" {
		return c.Exporter.Endpoint
	}

	return "localhost:4317"
}

// GetOTLPConfig returns the effective OTLP config.
// Falls back to deprecated Exporter fields for backward compatibility.
func (c *TelemetryConfig) GetOTLPConfig() *OTLPConfig {
	if c == nil {
		return &OTLPConfig{}
	}
	if c.OTLP != nil {
		return c.OTLP
	}
	// Convert deprecated Exporter to OTLPConfig
	if c.Exporter != nil {
		return &OTLPConfig{
			Endpoint:    c.Exporter.Endpoint,
			Insecure:    c.Exporter.Insecure,
			Headers:     c.Exporter.Headers,
			Protocol:    c.Exporter.Protocol,
			Timeout:     c.Exporter.Timeout,
			Compression: c.Exporter.Compression,
		}
	}

	return &OTLPConfig{}
}

// boolPtr returns a pointer to the given boolean value.
// It is useful for initializing config fields.
func boolPtr(v bool) *bool { return &v }
