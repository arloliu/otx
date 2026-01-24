package otx

import (
	"context"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// exporterParams holds common parameters for building exporters.
type exporterParams struct {
	Type        string            // "otlp", "console", "none"
	Protocol    string            // "grpc", "http/protobuf"
	Endpoint    string            // host:port or URL
	Headers     map[string]string // custom headers
	Timeout     time.Duration     // request timeout
	Compression string            // "gzip", "none"
	Insecure    bool              // disable TLS
}

func baseExporterParams(cfg *TelemetryConfig) exporterParams {
	params := exporterParams{
		Type:     "otlp",
		Protocol: "grpc",
		Endpoint: "localhost:4317",
		Timeout:  10 * time.Second,
		Insecure: true,
	}

	if cfg == nil {
		return params
	}

	otlp := cfg.GetOTLPConfig()
	if otlp.Endpoint != "" {
		params.Endpoint = otlp.Endpoint
	}
	if otlp.Protocol != "" {
		params.Protocol = otlp.Protocol
	}
	if otlp.Timeout > 0 {
		params.Timeout = normalizeDuration(otlp.Timeout)
	}
	if otlp.Headers != nil {
		params.Headers = otlp.Headers
	}
	params.Compression = otlp.Compression
	params.Insecure = otlp.IsInsecure()

	return params
}

// nopSpanExporter is a no-op span exporter.
type nopSpanExporter struct{}

func (nopSpanExporter) ExportSpans(_ context.Context, _ []sdktrace.ReadOnlySpan) error { return nil }
func (nopSpanExporter) Shutdown(_ context.Context) error                               { return nil }

// buildTraceExporter creates a trace exporter based on configuration.
func buildTraceExporter(ctx context.Context, cfg *TelemetryConfig) (sdktrace.SpanExporter, error) {
	params := resolveTraceExporterParams(cfg)
	params.Type = normalizeExporterType(params.Type)

	switch params.Type {
	case "console":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "none", "nop":
		return nopSpanExporter{}, nil
	case "otlp":
		return buildOTLPTraceExporter(ctx, params)
	default:
		return buildOTLPTraceExporter(ctx, params)
	}
}

// resolveTraceExporterParams resolves effective trace exporter parameters.
func resolveTraceExporterParams(cfg *TelemetryConfig) exporterParams {
	params := baseExporterParams(cfg)

	// Apply traces-specific overrides
	params.Type = cfg.GetTracesExporter()
	if cfg.Traces != nil && cfg.Traces.Endpoint != "" {
		params.Endpoint = cfg.Traces.Endpoint
	}

	return params
}

func buildOTLPTraceExporter(ctx context.Context, params exporterParams) (sdktrace.SpanExporter, error) {
	if params.Protocol == "http/protobuf" || params.Protocol == "http" {
		opts := []otlptracehttp.Option{}
		if endpoint, path := splitEndpointURL(params.Endpoint); endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
			if path != "" {
				opts = append(opts, otlptracehttp.WithURLPath(path))
			}
		} else {
			opts = append(opts, otlptracehttp.WithEndpoint(params.Endpoint))
		}

		if len(params.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(params.Headers))
		}
		if params.Timeout > 0 {
			opts = append(opts, otlptracehttp.WithTimeout(params.Timeout))
		}
		if params.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if params.Compression == "gzip" {
			opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
		}

		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	}

	// Default to gRPC
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(params.Endpoint),
	}
	if len(params.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(params.Headers))
	}
	if params.Timeout > 0 {
		opts = append(opts, otlptracegrpc.WithTimeout(params.Timeout))
	}
	if params.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if params.Compression == "gzip" {
		opts = append(opts, otlptracegrpc.WithCompressor("gzip"))
	}

	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

// nopLogExporter is a no-op log exporter.
type nopLogExporter struct{}

func (nopLogExporter) Export(_ context.Context, _ []sdklog.Record) error { return nil }
func (nopLogExporter) Shutdown(_ context.Context) error                  { return nil }
func (nopLogExporter) ForceFlush(_ context.Context) error                { return nil }

// buildLogExporter creates a log exporter based on configuration.
func buildLogExporter(ctx context.Context, cfg *TelemetryConfig) (sdklog.Exporter, error) {
	params := resolveLogExporterParams(cfg)
	params.Type = normalizeExporterType(params.Type)

	switch params.Type {
	case "console":
		return stdoutlog.New(stdoutlog.WithPrettyPrint())
	case "none", "nop":
		return nopLogExporter{}, nil
	case "otlp":
		return buildOTLPLogExporter(ctx, params)
	default:
		return buildOTLPLogExporter(ctx, params)
	}
}

// resolveLogExporterParams resolves effective log exporter parameters.
func resolveLogExporterParams(cfg *TelemetryConfig) exporterParams {
	params := baseExporterParams(cfg)

	// Apply logs-specific overrides
	if cfg.Logs != nil {
		if cfg.Logs.Exporter != "" {
			params.Type = cfg.Logs.Exporter
		}
		if cfg.Logs.Endpoint != "" {
			params.Endpoint = cfg.Logs.Endpoint
		}
	}

	return params
}

func buildOTLPLogExporter(ctx context.Context, params exporterParams) (sdklog.Exporter, error) {
	if params.Protocol == "http/protobuf" || params.Protocol == "http" {
		opts := buildHTTPOptions(
			params,
			otlploghttp.WithEndpoint,
			otlploghttp.WithEndpointURL,
			otlploghttp.WithHeaders,
			otlploghttp.WithTimeout,
			otlploghttp.WithInsecure,
			func() otlploghttp.Option { return otlploghttp.WithCompression(otlploghttp.GzipCompression) },
		)

		return otlploghttp.New(ctx, opts...)
	}

	// Default to gRPC
	opts := buildGRPCOptions(
		params,
		otlploggrpc.WithEndpoint,
		otlploggrpc.WithHeaders,
		otlploggrpc.WithTimeout,
		otlploggrpc.WithInsecure,
		func() otlploggrpc.Option { return otlploggrpc.WithCompressor("gzip") },
	)

	return otlploggrpc.New(ctx, opts...)
}

// buildMetricExporter creates a metric exporter based on configuration.
func buildMetricExporter(ctx context.Context, cfg *TelemetryConfig) (sdkmetric.Exporter, error) {
	params := resolveMetricExporterParams(cfg)
	params.Type = normalizeExporterType(params.Type)

	switch params.Type {
	case "console":
		return stdoutmetric.New(stdoutmetric.WithPrettyPrint())
	case "none", "nop":
		return newNopMetricExporter(), nil
	case "otlp":
		return buildOTLPMetricExporter(ctx, params)
	default:
		return buildOTLPMetricExporter(ctx, params)
	}
}

// resolveMetricExporterParams resolves effective metric exporter parameters.
func resolveMetricExporterParams(cfg *TelemetryConfig) exporterParams {
	params := baseExporterParams(cfg)

	// Apply metrics-specific overrides
	if cfg.Metrics != nil {
		if cfg.Metrics.Exporter != "" {
			params.Type = cfg.Metrics.Exporter
		}
		if cfg.Metrics.Endpoint != "" {
			params.Endpoint = cfg.Metrics.Endpoint
		}
	}

	return params
}

func buildOTLPMetricExporter(ctx context.Context, params exporterParams) (sdkmetric.Exporter, error) {
	if params.Protocol == "http/protobuf" || params.Protocol == "http" {
		opts := buildHTTPOptions(
			params,
			otlpmetrichttp.WithEndpoint,
			otlpmetrichttp.WithEndpointURL,
			otlpmetrichttp.WithHeaders,
			otlpmetrichttp.WithTimeout,
			otlpmetrichttp.WithInsecure,
			func() otlpmetrichttp.Option { return otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression) },
		)

		return otlpmetrichttp.New(ctx, opts...)
	}

	// Default to gRPC
	opts := buildGRPCOptions(
		params,
		otlpmetricgrpc.WithEndpoint,
		otlpmetricgrpc.WithHeaders,
		otlpmetricgrpc.WithTimeout,
		otlpmetricgrpc.WithInsecure,
		func() otlpmetricgrpc.Option { return otlpmetricgrpc.WithCompressor("gzip") },
	)

	return otlpmetricgrpc.New(ctx, opts...)
}

// nopMetricExporter is a no-op metric exporter.
type nopMetricExporter struct{}

func newNopMetricExporter() sdkmetric.Exporter { return nopMetricExporter{} }

func (nopMetricExporter) Export(_ context.Context, _ *metricdata.ResourceMetrics) error { return nil }
func (nopMetricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}

func (nopMetricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}
func (nopMetricExporter) ForceFlush(_ context.Context) error { return nil }
func (nopMetricExporter) Shutdown(_ context.Context) error   { return nil }

func normalizeExporterType(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return "otlp"
	}
	switch v {
	case "stdout":
		return "console"
	case "noop":
		return "nop"
	default:
		return v
	}
}

// normalizeDuration treats sub-millisecond values as milliseconds per OTel spec for numeric env vars.
func normalizeDuration(value time.Duration) time.Duration {
	if value > 0 && value < time.Millisecond {
		//nolint:durationcheck // required to interpret numeric env values as milliseconds
		return value * time.Millisecond
	}

	return value
}

func splitEndpointURL(raw string) (host string, path string) {
	if raw == "" {
		return "", ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || !isHTTPSScheme(parsed.Scheme) {
		return "", ""
	}

	return parsed.Host, parsed.Path
}

func buildHTTPOptions[T any](
	params exporterParams,
	withEndpoint func(string) T,
	withEndpointURL func(string) T,
	withHeaders func(map[string]string) T,
	withTimeout func(time.Duration) T,
	withInsecure func() T,
	withCompression func() T,
) []T {
	var opts []T
	if parsed, err := url.Parse(params.Endpoint); err == nil && isHTTPSScheme(parsed.Scheme) {
		opts = append(opts, withEndpointURL(params.Endpoint))
	} else {
		opts = append(opts, withEndpoint(params.Endpoint))
	}
	if len(params.Headers) > 0 {
		opts = append(opts, withHeaders(params.Headers))
	}
	if params.Timeout > 0 {
		opts = append(opts, withTimeout(params.Timeout))
	}
	if params.Insecure {
		opts = append(opts, withInsecure())
	}
	if params.Compression == "gzip" {
		opts = append(opts, withCompression())
	}

	return opts
}

func isHTTPSScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

func buildGRPCOptions[T any](
	params exporterParams,
	withEndpoint func(string) T,
	withHeaders func(map[string]string) T,
	withTimeout func(time.Duration) T,
	withInsecure func() T,
	withCompression func() T,
) []T {
	opts := []T{withEndpoint(params.Endpoint)}
	if len(params.Headers) > 0 {
		opts = append(opts, withHeaders(params.Headers))
	}
	if params.Timeout > 0 {
		opts = append(opts, withTimeout(params.Timeout))
	}
	if params.Insecure {
		opts = append(opts, withInsecure())
	}
	if params.Compression == "gzip" {
		opts = append(opts, withCompression())
	}

	return opts
}
