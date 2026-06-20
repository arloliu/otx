//revive:disable:line-length-limit
package otx

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/arloliu/otx/internal/endpoint"
)

// TelemetryConfig configures the OpenTelemetry system.
// Environment variable names follow the OTel specification:
// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/
type TelemetryConfig struct {
	// Enabled controls whether the OTX telemetry system is active.
	Enabled *bool `yaml:"enabled" json:"enabled" default:"false" env:"OTX_ENABLED"`

	// ServiceName is the name of the service for telemetry identification.
	// Maps to OTEL_SERVICE_NAME.
	ServiceName string `yaml:"serviceName" json:"serviceName" env:"OTEL_SERVICE_NAME" validate:"required_if=Enabled true"`

	// Version is the service version (e.g., git commit or semantic version).
	// Used in service.version resource attribute.
	Version string `yaml:"version" json:"version" env:"OTEL_SERVICE_VERSION"`

	// Environment is the deployment environment (e.g., production, development).
	// Used in deployment.environment resource attribute.
	Environment string `yaml:"environment" json:"environment" env:"OTEL_DEPLOYMENT_ENVIRONMENT" default:"development"`

	// ResourceAttributes contains additional resource attributes as key=value pairs.
	// Maps to OTEL_RESOURCE_ATTRIBUTES (comma-separated key=value pairs, per the
	// OTel spec). The env value is parsed by otx (see LoadConfig/ParseConfig) and,
	// when set, fully replaces any value loaded from the config file. A legacy
	// key:value form is also accepted for backward compatibility.
	ResourceAttributes map[string]string `yaml:"resourceAttributes,omitempty" json:"resourceAttributes,omitempty"`

	// OTLP contains shared OTLP exporter settings used by all signals (traces, logs, metrics).
	// Signal-specific settings can override these.
	OTLP *OTLPConfig `yaml:"otlp,omitempty" json:"otlp,omitempty"`

	// Traces configures the tracing subsystem.
	Traces *TracesConfig `yaml:"traces,omitempty" json:"traces,omitempty"`

	// Logs configures the logging subsystem (OTel log bridge).
	// Used by shared/logging's WithLoggerProvider integration.
	Logs *LogsConfig `yaml:"logs,omitempty" json:"logs,omitempty"`

	// Metrics configures the metrics subsystem.
	// Provides MeterProvider for application metrics.
	Metrics *MetricsConfig `yaml:"metrics,omitempty" json:"metrics,omitempty"`

	// Propagation configures context propagation (W3C TraceContext, Baggage).
	// Maps to OTEL_PROPAGATORS.
	Propagation *PropConfig `yaml:"propagation,omitempty" json:"propagation,omitempty"`

	// Deprecated: Use Traces.Sampling instead. Kept for backward compatibility.
	Sampling *SamplingConfig `yaml:"sampling,omitempty" json:"sampling,omitempty"`

	// Deprecated: Use OTLP or Traces.Exporter instead. Kept for backward compatibility.
	Exporter *ExporterConfig `yaml:"exporter,omitempty" json:"exporter,omitempty"`
}

// OTLPConfig contains shared OTLP exporter settings.
// These settings apply to all signals unless overridden by signal-specific config.
type OTLPConfig struct {
	// Endpoint is the OTLP collector endpoint.
	// Maps to OTEL_EXPORTER_OTLP_ENDPOINT.
	//
	// Accepted forms (both protocols):
	//   - Bare "host:port" (e.g., "localhost:4317" for gRPC, "localhost:4318"
	//     for HTTP) — canonical form; TLS controlled by the Insecure flag.
	//   - Scheme-bearing URL (e.g., "https://collector:4317") — scheme takes
	//     precedence over Insecure; https:// is always TLS, http:// is always
	//     plain. Any other scheme is rejected at load time.
	//
	// The effective default is protocol-aware and applied at exporter/zaplog
	// resolution, not at config load: http/protobuf → localhost:4318,
	// grpc → localhost:4317. Leave empty to use the protocol default.
	Endpoint string `yaml:"endpoint" json:"endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT"`

	// Insecure disables TLS for the OTLP connection.
	// Maps to OTEL_EXPORTER_OTLP_INSECURE.
	//
	// WARNING: the otx default is true (plaintext), which differs from the OTel
	// spec default (false). A bare host:port endpoint with Insecure unset is
	// exported over plaintext — including any credential-bearing Headers. This
	// is safe for the localhost default but a leak against a remote collector;
	// otx emits a one-time diagnostic via otel.Handle in that case. Set this to
	// false, or use an https:// Endpoint (the scheme forces TLS regardless of
	// this flag), when exporting to a remote host.
	Insecure *bool `yaml:"insecure" json:"insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" default:"true"`

	// Headers adds custom headers to OTLP requests.
	// Maps to OTEL_EXPORTER_OTLP_HEADERS (comma-separated key=value pairs, per the
	// OTel spec). The env value is parsed by otx (see LoadConfig/ParseConfig) and,
	// when set, fully replaces any value loaded from the config file. A legacy
	// key:value form is also accepted for backward compatibility.
	// Avoid logging this value, as it may contain sensitive credentials.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Protocol determines the OTLP transport protocol.
	// Maps to OTEL_EXPORTER_OTLP_PROTOCOL.
	// Options: "grpc", "http/protobuf", "http".
	Protocol string `yaml:"protocol" json:"protocol" env:"OTEL_EXPORTER_OTLP_PROTOCOL" default:"http/protobuf" validate:"oneof=grpc http/protobuf http"`

	// Timeout is the timeout for exporter operations.
	// Maps to OTEL_EXPORTER_OTLP_TIMEOUT.
	Timeout time.Duration `yaml:"timeout" json:"timeout" env:"OTEL_EXPORTER_OTLP_TIMEOUT" default:"10s" validate:"gte=0"`

	// Compression sets the compression algorithm for OTLP.
	// Maps to OTEL_EXPORTER_OTLP_COMPRESSION.
	// Options: "gzip", "none".
	Compression string `yaml:"compression,omitempty" json:"compression,omitempty" env:"OTEL_EXPORTER_OTLP_COMPRESSION" validate:"omitempty,oneof=gzip none"`
}

// IsInsecure returns true if insecure (plaintext) connection is enabled.
// It defaults to true when the receiver or the Insecure field is nil — see the
// Insecure field doc for the plaintext-default caveat.
func (c *OTLPConfig) IsInsecure() bool {
	return c == nil || c.Insecure == nil || *c.Insecure
}

// TracesConfig configures the tracing subsystem.
type TracesConfig struct {
	// Enabled controls whether tracing is active. Defaults to true if parent is enabled.
	Enabled *bool `yaml:"enabled" json:"enabled" default:"true"`

	// Exporter determines the trace exporter type.
	// Maps to OTEL_TRACES_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Exporter string `yaml:"exporter" json:"exporter" env:"OTEL_TRACES_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint overrides OTLP.Endpoint for traces.
	// Maps to OTEL_EXPORTER_OTLP_TRACES_ENDPOINT.
	// In most cases, leave this empty and set OTLP.Endpoint instead.
	// Only use this when traces need a different endpoint than other signals.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty" env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"`

	// Sampling configures the trace sampling strategy.
	Sampling *SamplingConfig `yaml:"sampling,omitempty" json:"sampling,omitempty"`
}

// IsEnabled returns true if tracing is enabled.
// Traces default ON: a nil receiver or nil Enabled field reports true, so tracing
// is active whenever the parent telemetry is enabled unless explicitly disabled.
// This is the opposite of LogsConfig/MetricsConfig, which default off.
func (c *TracesConfig) IsEnabled() bool {
	return c == nil || c.Enabled == nil || *c.Enabled
}

// LogsConfig configures the logging subsystem (OTel log bridge).
// This integrates with shared/logging via WithLoggerProvider.
type LogsConfig struct {
	// Enabled controls whether OTel log export is active.
	// Defaults to false (opt-in for logs).
	Enabled *bool `yaml:"enabled" json:"enabled" default:"false"`

	// Exporter determines the log exporter type.
	// Maps to OTEL_LOGS_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Exporter string `yaml:"exporter" json:"exporter" env:"OTEL_LOGS_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint overrides OTLP.Endpoint for logs.
	// Maps to OTEL_EXPORTER_OTLP_LOGS_ENDPOINT.
	// In most cases, leave this empty and set OTLP.Endpoint instead.
	// Only use this when logs need a different endpoint than other signals.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty" env:"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"`

	// Protocol overrides OTLP.Protocol for logs.
	// Maps to OTEL_EXPORTER_OTLP_LOGS_PROTOCOL.
	// Options: "grpc", "http/protobuf", "http".
	// The otx/zaplog adapter routes "grpc" to zapwire's hand-rolled OTLP/gRPC
	// client and anything else to OTLP/HTTP. Default is "http/protobuf" (port 4318).
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" env:"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL" validate:"omitempty,oneof=grpc http/protobuf http"`

	// MinLevel is the minimum zap level the otx/zaplog OTLP core emits.
	// It gates log-export cost (e.g. "warn" ships only warn and above).
	// Defaults to "info" when empty. Ignored by the SDK log pipeline.
	MinLevel string `yaml:"minLevel,omitempty" json:"minLevel,omitempty" validate:"omitempty,oneof=debug info warn error dpanic panic fatal"`

	// DrainTimeout bounds the total time w.Sync()/w.Close() spend draining the
	// zapwire queue at shutdown before dropping the remainder (dropped count
	// accumulates in the writer's DroppedLogs counter). This caps the worst-case
	// shutdown latency a stalled or slow receiver can impose.
	//
	// The bound is soft: an export attempt already in flight still runs to its
	// Timeout deadline, so the effective ceiling is DrainTimeout + at most one
	// in-flight request. A healthy receiver completes the drain long before the
	// bound; the option only trims a hostile or broken receiver's tail.
	//
	// Zero (default) keeps the legacy unbounded behaviour: Sync waits until
	// every queued record is exported or dropped by retry exhaustion. Applies
	// only to the otx/zaplog zapwire data plane; ignored by the SDK log pipeline.
	DrainTimeout time.Duration `yaml:"drainTimeout,omitempty" json:"drainTimeout,omitempty" validate:"gte=0"`
}

// IsEnabled returns true if OTel log export is enabled.
// Logs default OFF: a nil receiver or nil Enabled field reports false, so log
// export is opt-in and stays off unless Enabled is explicitly set to true. This
// is the opposite of TracesConfig, which defaults on.
func (c *LogsConfig) IsEnabled() bool {
	return c != nil && c.Enabled != nil && *c.Enabled
}

// MetricsConfig configures the metrics subsystem.
type MetricsConfig struct {
	// Enabled controls whether metrics collection is active.
	// Defaults to false (opt-in for metrics).
	Enabled *bool `yaml:"enabled" json:"enabled" default:"false"`

	// Exporter determines the metrics exporter type.
	// Maps to OTEL_METRICS_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Exporter string `yaml:"exporter" json:"exporter" env:"OTEL_METRICS_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint overrides OTLP.Endpoint for metrics.
	// Maps to OTEL_EXPORTER_OTLP_METRICS_ENDPOINT.
	// In most cases, leave this empty and set OTLP.Endpoint instead.
	// Only use this when metrics need a different endpoint than other signals.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty" env:"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"`

	// Interval is the export interval for periodic metric reader.
	// Maps to OTEL_METRIC_EXPORT_INTERVAL (milliseconds if numeric).
	// Defaults to 60s.
	Interval time.Duration `yaml:"interval,omitempty" json:"interval,omitempty" env:"OTEL_METRIC_EXPORT_INTERVAL" default:"60s" validate:"omitempty,gt=0"`
}

// IsEnabled returns true if metrics collection is enabled.
// Metrics default OFF: a nil receiver or nil Enabled field reports false, so
// metrics collection is opt-in and stays off unless Enabled is explicitly set to
// true. This is the opposite of TracesConfig, which defaults on.
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
	Sampler string `yaml:"sampler" json:"sampler" env:"OTEL_TRACES_SAMPLER" default:"parentbased_always_on" validate:"oneof=always_on always_off traceidratio parentbased_always_on parentbased_always_off parentbased_traceidratio"`

	// SamplerArg is the argument for ratio-based samplers.
	// Maps to OTEL_TRACES_SAMPLER_ARG.
	// For traceidratio and parentbased_traceidratio: sampling probability 0.0 to 1.0.
	// Values outside [0.0, 1.0] have undefined behavior.
	// Defaults to 1.0 (100%).
	SamplerArg float64 `yaml:"samplerArg" json:"samplerArg" env:"OTEL_TRACES_SAMPLER_ARG" default:"1.0" validate:"gte=0,lte=1"`
}

// ExporterConfig configures the trace exporter.
// Deprecated: Use OTLPConfig for shared settings and TracesConfig.Exporter for type.
// Kept for backward compatibility.
type ExporterConfig struct {
	// Type determines the exporter implementation.
	// Maps to OTEL_TRACES_EXPORTER.
	// Options: "otlp", "console", "stdout", "none".
	Type string `yaml:"type" json:"type" env:"OTEL_TRACES_EXPORTER" default:"otlp" validate:"oneof=otlp console stdout none"`

	// Endpoint is the OTLP collector endpoint.
	Endpoint string `yaml:"endpoint" json:"endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"localhost:4317"`

	// Insecure disables TLS for the OTLP connection.
	Insecure *bool `yaml:"insecure" json:"insecure" env:"OTEL_EXPORTER_OTLP_INSECURE" default:"true"`

	// Headers adds custom headers to OTLP requests.
	// Maps to OTEL_EXPORTER_OTLP_HEADERS; parsed by otx (see LoadConfig/ParseConfig).
	// Avoid logging this value, as it may contain sensitive credentials.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Protocol determines the OTLP transport protocol.
	Protocol string `yaml:"protocol" json:"protocol" env:"OTEL_EXPORTER_OTLP_PROTOCOL" default:"grpc" validate:"omitempty,oneof=grpc http/protobuf http"`

	// Timeout is the timeout for exporter operations.
	Timeout time.Duration `yaml:"timeout" json:"timeout" env:"OTEL_EXPORTER_OTLP_TIMEOUT" default:"10s" validate:"gte=0"`

	// Compression sets the compression algorithm for OTLP.
	Compression string `yaml:"compression,omitempty" json:"compression,omitempty" env:"OTEL_EXPORTER_OTLP_COMPRESSION" validate:"omitempty,oneof=gzip none"`
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
	Propagators string `yaml:"propagators" json:"propagators" env:"OTEL_PROPAGATORS" default:"tracecontext,baggage"`
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
// Telemetry defaults OFF: a nil receiver or nil Enabled field reports false, so
// the whole telemetry system is opt-in and stays off unless Enabled is set to
// true. This is the opposite of TracesConfig, which defaults on once telemetry
// is enabled.
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
// Priority: Traces.Endpoint > the effective OTLP endpoint (GetOTLPConfig, which
// itself falls back to the deprecated Exporter block only when OTLP is nil);
// when none is set, the default follows the effective protocol (grpc →
// localhost:4317, otherwise localhost:4318).
//
// It resolves through GetOTLPConfig so it can never diverge from the exporter
// builders — both consume the same effective OTLP block, including the
// non-nil-but-empty OTLP case (which intentionally does not fall back to
// Exporter.Endpoint).
//
// Deprecated: the exporter builders resolve endpoints internally; this
// accessor is retained for API compatibility only.
func (c *TelemetryConfig) GetOTLPEndpoint() string {
	if c == nil {
		return defaultEndpointFor("")
	}
	if c.Traces != nil && c.Traces.Endpoint != "" {
		return c.Traces.Endpoint
	}

	otlp := c.GetOTLPConfig()
	if otlp.Endpoint != "" {
		return otlp.Endpoint
	}

	return defaultEndpointFor(otlp.Protocol)
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

// validateEndpoint checks that a single endpoint value is valid.
// An empty value is always valid (defaults apply).
// Valid non-empty forms: bare host:port, http:// URL, https:// URL.
// Any other scheme (grpc://, tcp://, unix://, …) is rejected.
func validateEndpoint(fieldName, value string) error {
	if value == "" {
		return nil
	}
	if _, err := endpoint.Classify(value); err != nil {
		return fmt.Errorf("field %s: %w", fieldName, err)
	}

	return nil
}

// ValidateEndpoints validates the endpoint values in the config.
// It is called automatically by LoadConfig and ParseConfig after struct-tag
// validation passes. Endpoint fields use a post-load check because the
// scheme rule (bare / http / https only) cannot be expressed as a built-in
// go-playground/validator tag.
func (c *TelemetryConfig) ValidateEndpoints() error {
	type field struct {
		name  string
		value string
	}

	var fields []field

	if c.OTLP != nil {
		fields = append(fields, field{"otlp.endpoint", c.OTLP.Endpoint})
	}
	if c.Traces != nil {
		fields = append(fields, field{"traces.endpoint", c.Traces.Endpoint})
	}
	if c.Logs != nil {
		fields = append(fields, field{"logs.endpoint", c.Logs.Endpoint})
	}
	if c.Metrics != nil {
		fields = append(fields, field{"metrics.endpoint", c.Metrics.Endpoint})
	}
	if c.Exporter != nil {
		fields = append(fields, field{"exporter.endpoint", c.Exporter.Endpoint})
	}

	for _, f := range fields {
		if err := validateEndpoint(f.name, f.value); err != nil {
			return err
		}
	}

	return nil
}

// BoolPtr returns a pointer to v. It is the exported helper for setting the
// *bool Enabled fields on TelemetryConfig and the per-signal configs when
// building a config programmatically, e.g.
// TelemetryConfig{Enabled: otx.BoolPtr(true)}.
func BoolPtr(v bool) *bool { return &v }

// boolPtr returns a pointer to the given boolean value.
// It is useful for initializing config fields.
//
//nolint:revive // confusing-naming: BoolPtr is the intentional exported wrapper.
func boolPtr(v bool) *bool { return BoolPtr(v) }
