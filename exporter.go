package otx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"sync"

	"github.com/arloliu/otx/internal/endpoint"
	"github.com/arloliu/otx/internal/insecurewarn"
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

// ErrUnknownExporter is returned by the exporter builders when the configured
// exporter type is not one of the recognized values (otlp, console/stdout,
// none/nop). It is wrapped with the offending type for diagnostics; match it with
// errors.Is(err, ErrUnknownExporter).
var ErrUnknownExporter = errors.New("otx: unknown exporter type")

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
		Protocol: "http/protobuf",
		Endpoint: "", // protocol-aware default applied in each resolve*ExporterParams
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

// insecureWarned dedups the plaintext-to-remote diagnostic so each distinct
// remote endpoint is reported at most once per process (the same endpoint is
// resolved repeatedly across signals and provider re-creation).
var (
	insecureWarnMu sync.Mutex
	insecureWarned = map[string]struct{}{}
)

// warnInsecureRemote emits a one-time otel.Handle diagnostic when params would
// export plaintext (insecure, no https:// scheme) to a non-loopback host. An
// https:// scheme forces TLS regardless of the Insecure flag, so URL endpoints
// never warn. Loopback endpoints (localhost, 127.0.0.0/8, ::1) are the safe
// default and never warn. Each distinct endpoint warns at most once.
//
// Classification and the diagnostic message are shared with the zaplog OTLP
// core via internal/insecurewarn; this function keeps a local dedup set so the
// trace/metric/log call sites stay byte-identical and the existing exporter
// tests can reset it in isolation.
func warnInsecureRemote(params exporterParams) {
	if !insecurewarn.ShouldWarn(params.Endpoint, params.Insecure) {
		return
	}

	insecureWarnMu.Lock()
	_, seen := insecureWarned[params.Endpoint]
	if !seen {
		insecureWarned[params.Endpoint] = struct{}{}
	}
	insecureWarnMu.Unlock()
	if seen {
		return
	}

	insecurewarn.Emit(params.Endpoint)
}

// defaultEndpointFor returns the OTel-spec default OTLP endpoint for the given
// protocol: gRPC uses 4317, everything else (http/protobuf, http) uses 4318.
func defaultEndpointFor(protocol string) string {
	if protocol == "grpc" {
		return "localhost:4317"
	}

	return "localhost:4318"
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
		return nil, fmt.Errorf("%w: %q", ErrUnknownExporter, params.Type)
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

	// Apply protocol-aware default endpoint after all per-signal overrides.
	if params.Endpoint == "" {
		params.Endpoint = defaultEndpointFor(params.Protocol)
	}

	warnInsecureRemote(params)

	return params
}

func buildOTLPTraceExporter(ctx context.Context, params exporterParams) (sdktrace.SpanExporter, error) {
	if params.Protocol == "http/protobuf" || params.Protocol == "http" {
		opts := buildHTTPOptions(
			params,
			otlptracehttp.WithEndpoint,
			otlptracehttp.WithEndpointURL,
			otlptracehttp.WithHeaders,
			otlptracehttp.WithTimeout,
			otlptracehttp.WithInsecure,
			func() otlptracehttp.Option { return otlptracehttp.WithCompression(otlptracehttp.GzipCompression) },
		)

		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	}

	// Default to gRPC
	opts := buildGRPCOptions(
		params,
		otlptracegrpc.WithEndpoint,
		otlptracegrpc.WithEndpointURL,
		otlptracegrpc.WithHeaders,
		otlptracegrpc.WithTimeout,
		otlptracegrpc.WithInsecure,
		func() otlptracegrpc.Option { return otlptracegrpc.WithCompressor("gzip") },
	)

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
		return nil, fmt.Errorf("%w: %q", ErrUnknownExporter, params.Type)
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
		if cfg.Logs.Protocol != "" {
			params.Protocol = cfg.Logs.Protocol
		}
	}

	// Apply protocol-aware default endpoint after all per-signal overrides.
	if params.Endpoint == "" {
		params.Endpoint = defaultEndpointFor(params.Protocol)
	}

	warnInsecureRemote(params)

	return params
}

// otlpLogsPath is the OTel-standard URL path for the OTLP/HTTP logs signal.
const otlpLogsPath = "/v1/logs"

// withLogsPath ensures a URL-form OTLP endpoint targets the logs signal path.
//
// otlploghttp's WithEndpointURL (unlike the trace and metric exporters, which
// share otlpconfig) uses the URL path verbatim and does NOT default an empty
// path to /v1/logs. A bare "http://host:port" would therefore POST to "/" and
// 404 against a real collector. We append /v1/logs when the URL carries no
// explicit path. Bare host:port endpoints (which use WithEndpoint and default
// the path themselves) and URLs with an explicit path are returned unchanged,
// so this is forward-safe if the SDK later defaults the path itself.
func withLogsPath(rawEndpoint string) string {
	if !endpoint.IsHTTP(rawEndpoint) {
		return rawEndpoint
	}

	u, err := url.Parse(rawEndpoint)
	if err != nil {
		return rawEndpoint
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = otlpLogsPath

		return u.String()
	}

	return rawEndpoint
}

func buildOTLPLogExporter(ctx context.Context, params exporterParams) (sdklog.Exporter, error) {
	if params.Protocol == "http/protobuf" || params.Protocol == "http" {
		// Compensate for otlploghttp not defaulting the URL path to /v1/logs.
		params.Endpoint = withLogsPath(params.Endpoint)
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
		otlploggrpc.WithEndpointURL,
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
		return nil, fmt.Errorf("%w: %q", ErrUnknownExporter, params.Type)
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

	// Apply protocol-aware default endpoint after all per-signal overrides.
	if params.Endpoint == "" {
		params.Endpoint = defaultEndpointFor(params.Protocol)
	}

	warnInsecureRemote(params)

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
		otlpmetricgrpc.WithEndpointURL,
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

//nolint:dupl // intentionally mirrors buildGRPCOptions to keep option ordering byte-identical across transports
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
	// Scheme-overrides-Insecure precedence (OTel spec; mirrors zaplog/core.go):
	// a scheme-bearing URL determines TLS via WithEndpointURL — the SDK derives
	// insecurity from the scheme itself (http://→insecure, https://→secure), so
	// the conflicting WithInsecure MUST NOT be appended for URL endpoints or it
	// would override the scheme (options apply in order, last wins). For bare
	// host:port endpoints the scheme is absent, so Insecure decides plaintext.
	isURL := endpoint.IsHTTP(params.Endpoint)
	if isURL {
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
	if !isURL && params.Insecure {
		opts = append(opts, withInsecure())
	}
	if params.Compression == "gzip" {
		opts = append(opts, withCompression())
	}

	return opts
}

//nolint:dupl // intentionally mirrors buildHTTPOptions to keep option ordering byte-identical across transports
func buildGRPCOptions[T any](
	params exporterParams,
	withEndpoint func(string) T,
	withEndpointURL func(string) T,
	withHeaders func(map[string]string) T,
	withTimeout func(time.Duration) T,
	withInsecure func() T,
	withCompression func() T,
) []T {
	// Scheme-overrides-Insecure precedence (OTel spec; mirrors buildHTTPOptions):
	// a scheme-bearing URL determines TLS via WithEndpointURL — the SDK derives
	// insecurity from the scheme (http://→insecure, https://→secure; last option
	// wins). For bare host:port endpoints the scheme is absent, so Insecure decides
	// plaintext. WithInsecure MUST NOT be appended for URL endpoints or it would
	// override the scheme (trace/metric: Insecure = scheme != "https";
	// log: insecureFromScheme via setting[bool]).
	var opts []T
	isURL := endpoint.IsHTTP(params.Endpoint)
	if isURL {
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
	if !isURL && params.Insecure {
		opts = append(opts, withInsecure())
	}
	if params.Compression == "gzip" {
		opts = append(opts, withCompression())
	}

	return opts
}
