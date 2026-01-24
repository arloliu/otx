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

// ============================================================================
// Tracer Provider
// ============================================================================

// NewTracerProvider initializes the OpenTelemetry TracerProvider.
// Returns ErrDisabled if telemetry is not enabled in config.
func NewTracerProvider(ctx context.Context, cfg *TelemetryConfig) (*sdktrace.TracerProvider, error) {
	if !cfg.IsEnabled() {
		return nil, ErrDisabled
	}

	// Check if traces are enabled
	if cfg.Traces != nil && !cfg.Traces.IsEnabled() {
		return nil, ErrDisabled
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
// Returns ErrLogsDisabled if logs export is not enabled in config.
// Use this with shared/logging's WithLoggerProvider integration.
func NewLoggerProvider(ctx context.Context, cfg *TelemetryConfig) (*sdklog.LoggerProvider, error) {
	if !cfg.IsEnabled() {
		return nil, ErrDisabled
	}

	// Check if logs are enabled (opt-in)
	if cfg.Logs == nil || !cfg.Logs.IsEnabled() {
		return nil, ErrLogsDisabled
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
// Returns ErrMetricsDisabled if metrics export is not enabled in config.
func NewMeterProvider(ctx context.Context, cfg *TelemetryConfig) (*sdkmetric.MeterProvider, error) {
	if !cfg.IsEnabled() {
		return nil, ErrDisabled
	}

	// Check if metrics are enabled (opt-in)
	if cfg.Metrics == nil || !cfg.Metrics.IsEnabled() {
		return nil, ErrMetricsDisabled
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

// buildResource creates a common resource for all providers.
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
		ms := int64(value / time.Nanosecond)
		if ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}

		return defaultValue
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
