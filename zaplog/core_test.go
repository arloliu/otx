package zaplog

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arloliu/otx"
	"github.com/arloliu/zapwire/otlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// grpcRejectionMsg is the spec-pinned error string (design §3.2 rule 2).
// Tests use the literal, not the sentinel, so they catch any future drift.
const grpcRejectionMsg = "zaplog requires logs protocol http/protobuf; " +
	"set telemetry.logs.protocol, or use otx.NewLoggerProvider for gRPC"

// baseCfg is a minimal enabled config pointing logs at the given http endpoint.
func httpCfg(endpoint string) *otx.TelemetryConfig {
	return &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "checkout",
		Version:     "1.2.3",
		Environment: "production",
		OTLP:        &otx.OTLPConfig{Endpoint: endpoint, Protocol: "http/protobuf", Insecure: boolPtr(true)},
		Logs:        &otx.LogsConfig{Enabled: boolPtr(true)},
	}
}

func TestNewCore_RejectsGRPC(t *testing.T) {
	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP:        &otx.OTLPConfig{Endpoint: "collector:4317", Protocol: "grpc"},
		Logs:        &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.EqualError(t, err, grpcRejectionMsg)
	assert.Nil(t, core)
	assert.Nil(t, w)
}

func TestNewCore_RejectsGRPC_DefaultProtocol(t *testing.T) {
	// Empty effective protocol defaults to grpc and is rejected with the exact
	// spec-pinned message (design §3.2 rule 2).
	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP:        &otx.OTLPConfig{Endpoint: "collector:4317"},
		Logs:        &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	_, _, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.EqualError(t, err, grpcRejectionMsg)
}

func TestNewCore_InvalidMinLevel(t *testing.T) {
	cfg := httpCfg("collector:4318")
	cfg.Logs.MinLevel = "bogus"

	_, _, err := NewCore(context.Background(), cfg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minLevel")
}

func TestNewCore_MissingServiceName(t *testing.T) {
	cfg := httpCfg("collector:4318")
	cfg.ServiceName = ""

	_, _, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.ErrorIs(t, err, otx.ErrServiceNameRequired)
}

// emitAndFlush logs one info record through a fresh core and flushes the writer.
func emitAndFlush(t *testing.T, cfg *otx.TelemetryConfig, opts ...otlp.Option) *captureServer {
	t.Helper()
	cs := newCaptureServer(t)
	cfg.OTLP.Endpoint = cs.Server.URL // full http URL passes through

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	logger := zap.New(core)
	logger.Info("hello", zap.String("k", "v"))
	require.NoError(t, w.Sync())

	return cs
}

func TestNewCore_HeadersTimeoutGzipReachWire(t *testing.T) {
	cfg := httpCfg("")
	cfg.OTLP.Headers = map[string]string{"Authorization": "Bearer tok", "X-Tenant": "acme"}
	cfg.OTLP.Timeout = 7 * time.Second
	cfg.OTLP.Compression = "gzip"

	cs := emitAndFlush(t, cfg)

	snap := cs.snapshot()
	require.NotEmpty(t, snap)
	got := snap[0]

	assert.Equal(t, "/v1/logs", got.path)
	assert.Equal(t, "application/x-protobuf", got.contentType)
	assert.Equal(t, "gzip", got.contentEncoding, "gzip Content-Encoding must reach the wire")
	assert.Equal(t, "Bearer tok", got.headers.Get("Authorization"))
	assert.Equal(t, "acme", got.headers.Get("X-Tenant"))

	// Body decompressed and decoded successfully (helper already did the decode);
	// confirm the record landed.
	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "hello", recs[0].GetBody().GetStringValue())
}

func TestNewCore_HTTPAliasProtocol(t *testing.T) {
	cfg := httpCfg("")
	cfg.OTLP.Protocol = "http" // alias -> http/protobuf, must not be rejected

	cs := emitAndFlush(t, cfg)
	assert.Len(t, cs.allRecords(), 1)
}

func TestNewCore_InsecureSchemeSelection(t *testing.T) {
	// Insecure + bare host:port must yield http:// so the plaintext httptest
	// server accepts it.
	cs := newCaptureServer(t)
	host := strings.TrimPrefix(cs.Server.URL, "http://") // bare host:port

	cfg := httpCfg(host)
	cfg.OTLP.Insecure = boolPtr(true)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	zap.New(core).Info("scheme-test")
	require.NoError(t, w.Sync())
	assert.Len(t, cs.allRecords(), 1)
}

func TestNewCore_CallerOptionWinsOverConfig(t *testing.T) {
	// Config sets service name "checkout"; caller override must win because
	// config-derived options apply first.
	cfg := httpCfg("")

	cs := emitAndFlush(t, cfg, otlp.WithServiceName("override-svc"))

	attrs := cs.resourceAttrs(t)
	v, ok := attrValue(attrs, string(semconv.ServiceNameKey))
	require.True(t, ok)
	assert.Equal(t, "override-svc", v.GetStringValue())
}

func TestNewCore_ResourceParity(t *testing.T) {
	cfg := httpCfg("")
	cfg.ResourceAttributes = map[string]string{"team": "platform", "region": "us-east-1"}

	cs := emitAndFlush(t, cfg)

	attrs := cs.resourceAttrs(t)

	// Exactly one service.name (extracted for WithServiceName, excluded from
	// WithResource).
	assert.Equal(t, 1, countKey(attrs, string(semconv.ServiceNameKey)),
		"exactly one service.name in encoded request")

	sn, ok := attrValue(attrs, string(semconv.ServiceNameKey))
	require.True(t, ok)
	assert.Equal(t, "checkout", sn.GetStringValue())

	sv, ok := attrValue(attrs, string(semconv.ServiceVersionKey))
	require.True(t, ok)
	assert.Equal(t, "1.2.3", sv.GetStringValue())

	env, ok := attrValue(attrs, string(semconv.DeploymentEnvironmentKey))
	require.True(t, ok)
	assert.Equal(t, "production", env.GetStringValue())

	team, ok := attrValue(attrs, "team")
	require.True(t, ok)
	assert.Equal(t, "platform", team.GetStringValue())

	region, ok := attrValue(attrs, "region")
	require.True(t, ok)
	assert.Equal(t, "us-east-1", region.GetStringValue())

	// No synthesized service.instance.id / host attributes.
	_, hasInstance := attrValue(attrs, "service.instance.id")
	assert.False(t, hasInstance, "no synthesized service.instance.id")
	_, hasHost := attrValue(attrs, string(semconv.HostNameKey))
	assert.False(t, hasHost, "no synthesized host.name")

	// Schema URL is documented-absent on the log resource envelope.
	for _, u := range cs.schemaURLs() {
		assert.Empty(t, u, "zapwire envelope omits ResourceLogs.schema_url")
	}
}

func TestNewCore_NilLevelDefaultsToInfo(t *testing.T) {
	// nil level + no MinLevel -> info: a debug record must NOT be emitted, an
	// info record must.
	cfg := httpCfg("")
	cs := newCaptureServer(t)
	cfg.OTLP.Endpoint = cs.Server.URL

	core, w, err := NewCore(context.Background(), cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	logger := zap.New(core)
	logger.Debug("dropped")
	logger.Info("kept")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "kept", recs[0].GetBody().GetStringValue())
}

func TestNewCore_MinLevelGates(t *testing.T) {
	cfg := httpCfg("")
	cfg.Logs.MinLevel = "warn"
	cs := newCaptureServer(t)
	cfg.OTLP.Endpoint = cs.Server.URL

	core, w, err := NewCore(context.Background(), cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	logger := zap.New(core)
	logger.Info("dropped")
	logger.Warn("kept")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "kept", recs[0].GetBody().GetStringValue())
}
