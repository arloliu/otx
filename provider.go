package otx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// ErrDisabled is returned when telemetry is disabled.
var ErrDisabled = errors.New("otx: telemetry is disabled")

// ErrLogsDisabled is returned when log export is disabled.
var ErrLogsDisabled = errors.New("otx: logs export is disabled")

// ErrMetricsDisabled is returned when metrics export is disabled.
var ErrMetricsDisabled = errors.New("otx: metrics export is disabled")

// ErrServiceNameRequired is returned when ServiceName is empty but telemetry is enabled.
var ErrServiceNameRequired = errors.New("otx: service name is required")

// ErrTracesDisabled is returned by NewTracerProvider when telemetry is enabled
// but the traces signal is explicitly disabled. It wraps ErrDisabled, so callers
// that match either errors.Is(err, ErrTracesDisabled) or
// errors.Is(err, ErrDisabled) succeed.
var ErrTracesDisabled = fmt.Errorf("otx: traces export is disabled: %w", ErrDisabled)

// ============================================================================
// Tracer Provider
// ============================================================================

// NewTracerProvider initializes the OpenTelemetry TracerProvider.
// Returns ErrDisabled if telemetry is not enabled in config, or ErrTracesDisabled
// (which wraps ErrDisabled) if telemetry is enabled but the traces signal is off.
//
// Side effects: on success this mutates OTel globals — it calls
// otel.SetTracerProvider and otel.SetTextMapPropagator, replacing any previously
// installed global tracer provider and propagator. The global propagator is
// installed only by this constructor; services that export metrics or logs but
// not traces should call
// otel.SetTextMapPropagator(otx.BuildPropagator(cfg.Propagation)) to retain
// context propagation.
func NewTracerProvider(ctx context.Context, cfg *TelemetryConfig) (*sdktrace.TracerProvider, error) {
	if !cfg.IsEnabled() {
		return nil, ErrDisabled
	}

	// Check if traces are enabled
	if cfg.Traces != nil && !cfg.Traces.IsEnabled() {
		return nil, ErrTracesDisabled
	}

	// Fail fast on invalid programmatic endpoint config before dialing.
	if err := cfg.ValidateEndpoints(); err != nil {
		return nil, err
	}

	// Build resource
	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Build sampler
	sampler := buildSampler(cfg.GetSamplingConfig())

	// Build exporter using new config structure
	exporter, err := buildTraceExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build trace exporter: %w", err)
	}

	// Create provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
	)

	// Set global provider
	otel.SetTracerProvider(tp)

	// Set global propagator
	otel.SetTextMapPropagator(buildPropagator(cfg.Propagation))

	return tp, nil
}

// ============================================================================
// Logger Provider
// ============================================================================

// NewLoggerProvider initializes the OpenTelemetry LoggerProvider.
// Returns ErrDisabled if telemetry is not enabled, or ErrLogsDisabled if
// telemetry is enabled but the logs signal is off. These errors are distinct
// errors.Is targets: ErrLogsDisabled does not wrap ErrDisabled.
// Use this with shared/logging's WithLoggerProvider integration.
//
// Side effects: on success this mutates the OTel log global — it calls
// global.SetLoggerProvider, replacing any previously installed global logger
// provider. It does not install the global propagator (only NewTracerProvider
// does); see BuildPropagator if you need propagation without traces.
func NewLoggerProvider(ctx context.Context, cfg *TelemetryConfig) (*sdklog.LoggerProvider, error) {
	if !cfg.IsEnabled() {
		return nil, ErrDisabled
	}

	// Check if logs are enabled (opt-in)
	if cfg.Logs == nil || !cfg.Logs.IsEnabled() {
		return nil, ErrLogsDisabled
	}

	// Fail fast on invalid programmatic endpoint config before dialing.
	if err := cfg.ValidateEndpoints(); err != nil {
		return nil, err
	}

	// Build resource
	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Build log exporter
	exporter, err := buildLogExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build log exporter: %w", err)
	}

	// Create provider with batching processor
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	// Set global logger provider
	global.SetLoggerProvider(lp)

	return lp, nil
}

// ============================================================================
// Meter Provider
// ============================================================================

// NewMeterProvider initializes the OpenTelemetry MeterProvider.
// Returns ErrDisabled if telemetry is not enabled, or ErrMetricsDisabled if
// telemetry is enabled but the metrics signal is off. These errors are distinct
// errors.Is targets: ErrMetricsDisabled does not wrap ErrDisabled.
//
// Side effects: on success this mutates the OTel global — it calls
// otel.SetMeterProvider, replacing any previously installed global meter
// provider. It does not install the global propagator (only NewTracerProvider
// does); see BuildPropagator if you need propagation without traces.
func NewMeterProvider(ctx context.Context, cfg *TelemetryConfig) (*sdkmetric.MeterProvider, error) {
	if !cfg.IsEnabled() {
		return nil, ErrDisabled
	}

	// Check if metrics are enabled (opt-in)
	if cfg.Metrics == nil || !cfg.Metrics.IsEnabled() {
		return nil, ErrMetricsDisabled
	}

	// Fail fast on invalid programmatic endpoint config before dialing.
	if err := cfg.ValidateEndpoints(); err != nil {
		return nil, err
	}

	// Build resource
	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Build metric exporter
	exporter, err := buildMetricExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build metric exporter: %w", err)
	}

	// Parse export interval
	interval := normalizeMetricInterval(cfg.Metrics.Interval, 60*time.Second)

	// Create provider with periodic reader
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(interval),
		)),
	)

	// Set global meter provider
	otel.SetMeterProvider(mp)

	return mp, nil
}

// ============================================================================
// Shared Helpers
// ============================================================================

// BuildResource returns the OTel resource derived from cfg — the same resource
// NewTracerProvider, NewLoggerProvider, and NewMeterProvider attach to their
// providers. It is exported for advanced wiring that needs the resource
// identity outside the standard providers (custom SDK components, the otx/zaplog
// adapter), so logs and traces carry identical resource attributes and join in
// the backend.
func BuildResource(ctx context.Context, cfg *TelemetryConfig) (*resource.Resource, error) {
	return buildResource(ctx, cfg)
}

// buildResource creates a common resource for all providers.
//
//nolint:revive // confusing-naming: BuildResource is the intentional exported wrapper.
func buildResource(ctx context.Context, cfg *TelemetryConfig) (*resource.Resource, error) {
	if cfg.ServiceName == "" {
		return nil, ErrServiceNameRequired
	}

	baseAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
		semconv.DeploymentEnvironment(cfg.Environment),
	}
	for key, value := range cfg.ResourceAttributes {
		if key == "" {
			continue
		}
		baseAttrs = append(baseAttrs, attribute.String(key, value))
	}

	attrs := []resource.Option{
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(baseAttrs...),
	}

	res, err := resource.New(ctx, attrs...)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	return res, nil
}

// normalizeMetricInterval treats sub-millisecond values as milliseconds per OTel spec for numeric env vars.
func normalizeMetricInterval(value time.Duration, defaultValue time.Duration) time.Duration {
	if value <= 0 {
		return defaultValue
	}
	if value < time.Millisecond {
		//nolint:durationcheck // required to interpret numeric env values as milliseconds
		return value * time.Millisecond
	}

	return value
}

func buildSampler(cfg *SamplingConfig) sdktrace.Sampler {
	if cfg == nil {
		cfg = &SamplingConfig{Sampler: "parentbased_always_on", SamplerArg: 1.0}
	}

	// OTel standard sampler names per specification
	// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/
	switch cfg.Sampler {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(cfg.SamplerArg)
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerArg))
	default:
		// Default to parentbased_always_on per OTel spec
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}
