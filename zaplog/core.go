// Package zaplog adapts otx telemetry configuration to zapwire's OTLP log
// data plane (HTTP or gRPC). It builds a zapwire OTLP core and writer from a
// single otx.TelemetryConfig, deriving the resource identity (service.name +
// resource attributes) from the same otx.BuildResource the trace/metric
// providers use, so logs and traces join on identical resource attributes in
// the backend.
//
// It also provides the InfoCtx-style, context-aware logging surface zapwire
// deliberately leaves to the application layer (see Logger and Attach).
//
// # Quick start
//
//	core, w, err := zaplog.NewCore(ctx, cfg, zapcore.InfoLevel)
//	if err != nil { ... }
//	defer func() { _ = w.Sync(); _ = w.Close() }()
//	log := zaplog.Logger{Logger: zap.New(core)}
//	log.InfoCtx(ctx, "request handled")
//
// # Boundary
//
// otx owns the control plane (config, resource, providers, span context);
// zapwire/otlp owns the data plane (encoder, core, async exporter). zaplog is
// the single seam that derives the latter from the former. The dependency
// direction is permanent: otx may depend on zapwire, never the reverse.
package zaplog

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/arloliu/otx"
	"github.com/arloliu/otx/internal/endpoint"
	"github.com/arloliu/zapwire/otlp"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// defaultEndpointHTTP is the default OTLP endpoint for HTTP/protobuf, per the
	// OTel spec recommendation for OTLP/HTTP.
	defaultEndpointHTTP = "localhost:4318"
	// defaultEndpointGRPC is the default OTLP endpoint for gRPC, per the OTel
	// spec recommendation for OTLP/gRPC.
	defaultEndpointGRPC = "localhost:4317"
	defaultProtocol     = "http/protobuf"

	protocolHTTPProtobuf = "http/protobuf"
	protocolHTTP         = "http"
	protocolGRPC         = "grpc"

	serviceNameKey = "service.name"
)

// NewCore builds a zapwire OTLP core and its writer from otx telemetry config.
//
// The effective endpoint, protocol, TLS mode, headers, timeout, and compression
// are resolved through the same wholesale + per-signal-overlay chain otx's SDK
// log exporter uses: the base is cfg.GetOTLPConfig() (returned as-is when an
// OTLP block is set, else converted from the deprecated Exporter block), and
// Logs.Endpoint / Logs.Protocol overlay it per field. The resolved transport
// settings become config-derived otlp options applied BEFORE the caller's opts,
// so an explicit caller option always wins.
//
// Protocol routing: when the effective protocol is "grpc", NewCore calls
// otlp.NewGRPCCore — zapwire's hand-rolled OTLP/gRPC client (zero grpc-go in
// the data plane). Any other protocol (http/protobuf, http) calls otlp.NewHTTPCore.
// Default endpoints follow the effective protocol: grpc → localhost:4317,
// http/protobuf → localhost:4318. The existing buildEndpoint output
// (http:// or https:// URL from Classify + IsInsecure) is reused for both
// protocols; scheme precedence is unchanged. Note that gRPC additionally rejects
// URL paths at construction with an actionable zapwire error (e.g.
// "http://host:4317/path" is rejected; use bare "host:4317" or a scheme URL
// with no path).
//
// Resource identity is translated from otx.BuildResource(ctx, cfg):
// service.name feeds otlp.WithServiceName and is excluded from the remaining
// resource attributes (which feed otlp.WithResource) so exactly one
// service.name is emitted.
//
// When level is nil, NewCore resolves it to cfg.Logs.MinLevel (a zap level
// string) and then to zapcore.InfoLevel — it never passes nil to the otlp core,
// whose Check would panic. Pass a *zap.AtomicLevel for a runtime cost dial.
//
// The caller owns the returned *otlp.Writer and must Close it; the recommended
// shutdown is w.Sync() then w.Close().
func NewCore(
	ctx context.Context,
	cfg *otx.TelemetryConfig,
	level zapcore.LevelEnabler,
	opts ...otlp.Option,
) (zapcore.Core, *otlp.Writer, error) {
	base := cfg.GetOTLPConfig()

	// Resolve protocol BEFORE endpoint — the default endpoint depends on protocol.
	protocol := effectiveProtocol(cfg, base)

	defaultPort := defaultPortFor(protocol)
	resolvedEndpoint, err := buildEndpoint(effectiveEndpoint(cfg, base, protocol), base.IsInsecure(), defaultPort)
	if err != nil {
		return nil, nil, err
	}

	resOpts, err := resourceOptions(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	enabler, err := resolveLevel(cfg, level)
	if err != nil {
		return nil, nil, err
	}

	// Config-derived options FIRST, caller opts AFTER (later wins; design §3.2).
	merged := make([]otlp.Option, 0, len(resOpts)+4+len(opts))
	merged = append(merged, resOpts...)
	if len(base.Headers) > 0 {
		merged = append(merged, otlp.WithHeaders(base.Headers))
	}
	if base.Timeout > 0 {
		merged = append(merged, otlp.WithTimeout(normalizeDuration(base.Timeout)))
	}
	if base.Compression == "gzip" {
		merged = append(merged, otlp.WithCompression(otlp.Gzip))
	}
	merged = append(merged, opts...)

	// Route to the appropriate zapwire transport based on effective protocol
	// (design §3.2): grpc → zapwire's hand-rolled OTLP/gRPC client; everything
	// else (http/protobuf, http) → OTLP/HTTP.
	if protocol == protocolGRPC {
		return otlp.NewGRPCCore(resolvedEndpoint, enabler, merged...)
	}

	return otlp.NewHTTPCore(resolvedEndpoint, enabler, merged...)
}

// effectiveEndpoint applies the Logs.Endpoint overlay over the wholesale base.
// When neither is set, the default follows the effective protocol: grpc →
// localhost:4317, everything else → localhost:4318 (programmatic configs carry
// no struct-tag defaults, so the default must be applied here).
func effectiveEndpoint(cfg *otx.TelemetryConfig, base *otx.OTLPConfig, protocol string) string {
	if cfg.Logs != nil && cfg.Logs.Endpoint != "" {
		return cfg.Logs.Endpoint
	}
	if base.Endpoint != "" {
		return base.Endpoint
	}

	if protocol == protocolGRPC {
		return defaultEndpointGRPC
	}

	return defaultEndpointHTTP
}

// effectiveProtocol applies the Logs.Protocol overlay over the wholesale base,
// defaulting to http/protobuf (matching otx's baseExporterParams and the OTel
// spec recommendation) — zaplog accepts this default without a protocol override.
func effectiveProtocol(cfg *otx.TelemetryConfig, base *otx.OTLPConfig) string {
	if cfg.Logs != nil && cfg.Logs.Protocol != "" {
		return normalizeProtocol(cfg.Logs.Protocol)
	}
	if base.Protocol != "" {
		return normalizeProtocol(base.Protocol)
	}

	return defaultProtocol
}

// normalizeProtocol folds the "http" alias into "http/protobuf"; grpc and any
// unrecognized value pass through (validation rejects unrecognized at load).
func normalizeProtocol(p string) string {
	if p == protocolHTTP {
		return protocolHTTPProtobuf
	}

	return p
}

// buildEndpoint maps an otx host:port (or full URL) to the http(s):// form
// zapwire requires. It classifies via the shared endpoint.Classify so a
// programmatic config (which bypasses LoadConfig's ValidateEndpoints) receives
// the same actionable validation as a loaded config:
//
//   - invalid scheme (grpc://, tcp://, …) → the classifier's actionable error,
//     instead of silently prefixing it into garbage like "https://grpc://h"
//     and deferring to a dial failure at export time;
//   - bare host:port → prefixed http:// when insecure, else https://;
//   - bare host without port → defaultPort is appended before the scheme is
//     prefixed (e.g. "collector" + defaultPort "4317" → "http://collector:4317"
//     for gRPC insecure). This avoids the silent misconfig where a scheme URL
//     without a port delegates to the URL-default port (80/443) instead of the
//     protocol-correct OTLP port. HTTP/protobuf paths see the same fix
//     ("collector" without a port would have become http://collector → port 80).
//   - http/https URL → passed through unchanged (zapwire validates it and
//     appends /v1/logs to an empty path for HTTP; gRPC uses a fixed method path
//     and rejects user-supplied paths/query/fragments at construction).
//
// Classify avoids url.Parse's colon trap (url.Parse("localhost:4317") yields
// Scheme="localhost") and is case-insensitive on the scheme.
func buildEndpoint(ep string, insecure bool, defaultPort string) (string, error) {
	kind, err := endpoint.Classify(ep)
	if err != nil {
		return "", err
	}
	if kind == endpoint.KindHTTP || kind == endpoint.KindHTTPS {
		return ep, nil
	}
	// For bare endpoints, ensure a port is present so the caller does not land on
	// the URL-default port (80 or 443) when the scheme is prefixed.
	if _, _, err := net.SplitHostPort(ep); err != nil && ep != "" {
		// SplitHostPort failed → no port in the bare endpoint; append the default.
		ep = net.JoinHostPort(ep, defaultPort)
	}
	if insecure {
		return "http://" + ep, nil
	}

	return "https://" + ep, nil
}

// defaultPortFor returns the OTel-spec default OTLP port for the given protocol:
// gRPC uses 4317, everything else (http/protobuf, http) uses 4318.
func defaultPortFor(protocol string) string {
	if protocol == protocolGRPC {
		return "4317"
	}

	return "4318"
}

// normalizeDuration mirrors otx's exporter.go: a sub-millisecond value comes
// from the numeric OTEL_EXPORTER_OTLP_TIMEOUT env form (interpreted as
// milliseconds per the OTel spec), so it is scaled up to match the SDK path.
func normalizeDuration(value time.Duration) time.Duration {
	if value > 0 && value < time.Millisecond {
		return value * time.Millisecond //nolint:durationcheck // numeric env interpreted as ms
	}

	return value
}

// resolveLevel resolves the effective LevelEnabler: an explicit non-nil level
// wins; else cfg.Logs.MinLevel (a zap level string); else zapcore.InfoLevel.
// Never returns nil — the otlp core stores the enabler directly and a nil would
// panic in Check.
func resolveLevel(cfg *otx.TelemetryConfig, level zapcore.LevelEnabler) (zapcore.LevelEnabler, error) {
	if level != nil {
		return level, nil
	}
	if cfg.Logs != nil && cfg.Logs.MinLevel != "" {
		lvl, err := zapcore.ParseLevel(cfg.Logs.MinLevel)
		if err != nil {
			return nil, fmt.Errorf("zaplog: invalid logs minLevel %q: %w", cfg.Logs.MinLevel, err)
		}

		return lvl, nil
	}

	return zapcore.InfoLevel, nil
}

// resourceOptions translates otx.BuildResource into zapwire options:
// service.name -> WithServiceName, every other attribute -> WithResource as a
// typed zap.Field. service.name is excluded from WithResource so exactly one
// service.name is emitted (design §3.3).
func resourceOptions(ctx context.Context, cfg *otx.TelemetryConfig) ([]otlp.Option, error) {
	res, err := otx.BuildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var serviceName string
	var fields []zap.Field
	for _, kv := range res.Attributes() {
		if string(kv.Key) == serviceNameKey {
			serviceName = kv.Value.AsString()

			continue
		}
		if f, ok := attrToField(kv); ok {
			fields = append(fields, f)
		}
	}

	opts := make([]otlp.Option, 0, 2)
	if serviceName != "" {
		opts = append(opts, otlp.WithServiceName(serviceName))
	}
	if len(fields) > 0 {
		opts = append(opts, otlp.WithResource(fields...))
	}

	return opts, nil
}

// attrToField converts an OTel attribute.KeyValue to a typed zap.Field across
// the scalar and slice forms otx may produce (today only STRING, but the rest
// are handled for forward safety). Unknown/empty types are skipped.
func attrToField(kv attribute.KeyValue) (zap.Field, bool) {
	key := string(kv.Key)
	switch kv.Value.Type() {
	case attribute.STRING:
		return zap.String(key, kv.Value.AsString()), true
	case attribute.BOOL:
		return zap.Bool(key, kv.Value.AsBool()), true
	case attribute.INT64:
		return zap.Int64(key, kv.Value.AsInt64()), true
	case attribute.FLOAT64:
		return zap.Float64(key, kv.Value.AsFloat64()), true
	case attribute.STRINGSLICE:
		return zap.Strings(key, kv.Value.AsStringSlice()), true
	case attribute.BOOLSLICE:
		return zap.Bools(key, kv.Value.AsBoolSlice()), true
	case attribute.INT64SLICE:
		return zap.Int64s(key, kv.Value.AsInt64Slice()), true
	case attribute.FLOAT64SLICE:
		return zap.Float64s(key, kv.Value.AsFloat64Slice()), true
	default:
		return zap.Field{}, false
	}
}
