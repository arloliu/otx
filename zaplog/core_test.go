package zaplog

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arloliu/otx"
	"github.com/arloliu/zapwire/otlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
)

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

// captureLogsService is a minimal gRPC LogsService capture server for testing.
type captureLogsService struct {
	collogspb.UnimplementedLogsServiceServer
	mu       sync.Mutex
	requests []*collogspb.ExportLogsServiceRequest
}

func (s *captureLogsService) Export(
	_ context.Context,
	req *collogspb.ExportLogsServiceRequest,
) (*collogspb.ExportLogsServiceResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	return &collogspb.ExportLogsServiceResponse{}, nil
}

func (s *captureLogsService) allRequests() []*collogspb.ExportLogsServiceRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*collogspb.ExportLogsServiceRequest, len(s.requests))
	copy(out, s.requests)

	return out
}

// newGRPCCaptureServer starts a plaintext gRPC server on a random port and
// returns the listener address and the capture service. The server is stopped
// via t.Cleanup.
func newGRPCCaptureServer(t *testing.T) (addr string, svc *captureLogsService) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	svc = &captureLogsService{}
	srv := grpc.NewServer()
	collogspb.RegisterLogsServiceServer(srv, svc)
	t.Cleanup(srv.Stop)

	go func() { _ = srv.Serve(lis) }()

	return lis.Addr().String(), svc
}

// TestNewCore_GRPCRoutes verifies that a config with protocol grpc routes to
// zapwire's OTLP/gRPC transport and returns a non-nil core and writer without
// error.
func TestNewCore_GRPCRoutes(t *testing.T) {
	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP: &otx.OTLPConfig{
			Endpoint: "localhost:4317",
			Protocol: "grpc",
			Insecure: boolPtr(true), // default-insecure → http:// → h2c
		},
		Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err, "grpc protocol must route to NewGRPCCore without error")
	require.NotNil(t, core)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })
}

// TestNewCore_GRPCEndToEnd verifies that logs written through a grpc-protocol
// core actually arrive at a real gRPC LogsService server, carrying the service
// name and the logged message body.
func TestNewCore_GRPCEndToEnd(t *testing.T) {
	addr, svc := newGRPCCaptureServer(t)

	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "grpc-e2e-svc",
		OTLP: &otx.OTLPConfig{
			// Bare host:port + Insecure → http:// → h2c (no TLS)
			Endpoint: addr,
			Protocol: "grpc",
			Insecure: boolPtr(true),
		},
		Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	require.NotNil(t, core)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })

	logger := zap.New(core)
	logger.Info("grpc-e2e-message", zap.String("key", "val"))
	require.NoError(t, w.Sync())

	reqs := svc.allRequests()
	require.Len(t, reqs, 1, "server must receive exactly one ExportLogsServiceRequest")

	// Verify the log body.
	var body string
	for _, rl := range reqs[0].GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, rec := range sl.GetLogRecords() {
				body = rec.GetBody().GetStringValue()
			}
		}
	}
	assert.Equal(t, "grpc-e2e-message", body)

	// Verify service.name appears in resource attributes.
	var foundSvc bool
	for _, rl := range reqs[0].GetResourceLogs() {
		for _, attr := range rl.GetResource().GetAttributes() {
			if attr.GetKey() == serviceNameKey && attr.GetValue().GetStringValue() == "grpc-e2e-svc" {
				foundSvc = true
			}
		}
	}
	assert.True(t, foundSvc, "service.name must appear in resource attributes")
}

// TestNewCore_GRPCDefaultEndpoint verifies that when no endpoint is configured
// and protocol is grpc, effectiveEndpoint returns localhost:4317; for
// http/protobuf it returns localhost:4318.
func TestNewCore_GRPCDefaultEndpoint(t *testing.T) {
	// Test effectiveEndpoint directly — it is in the same package and provides
	// the cleanest unit seam for default-endpoint validation without a dial.
	t.Run("grpc default is 4317", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{}
		base := cfg.GetOTLPConfig()
		assert.Equal(t, defaultEndpointGRPC, effectiveEndpoint(cfg, base, protocolGRPC))
	})

	t.Run("http/protobuf default is 4318", func(t *testing.T) {
		cfg := &otx.TelemetryConfig{}
		base := cfg.GetOTLPConfig()
		assert.Equal(t, defaultEndpointHTTP, effectiveEndpoint(cfg, base, protocolHTTPProtobuf))
	})

	t.Run("NewCore grpc with no endpoint succeeds construction", func(t *testing.T) {
		// NewCore must not dial at construction — grpc with no endpoint should
		// succeed (default endpoint 4317 is set; dial happens at first export).
		cfg := &otx.TelemetryConfig{
			Enabled:     boolPtr(true),
			ServiceName: "svc",
			OTLP: &otx.OTLPConfig{
				Protocol: "grpc",
				Insecure: boolPtr(true),
			},
			Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
		}
		core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
		require.NoError(t, err, "construction must succeed even when no endpoint is configured")
		require.NotNil(t, core)
		require.NotNil(t, w)
		t.Cleanup(func() { _ = w.Close() })
	})
}

// TestNewCore_LoadedConfigGRPCDefault verifies that a config loaded via
// otx.ParseConfig (which runs fuda defaults) with otlp.protocol: grpc and
// no endpoint results in effectiveEndpoint returning localhost:4317. This
// pins the P1-1 fix: removing OTLPConfig.Endpoint's struct-tag default
// prevents fuda from materialising "localhost:4318" before the resolver runs.
func TestNewCore_LoadedConfigGRPCDefault(t *testing.T) {
	cfg, err := otx.ParseConfig([]byte(`
enabled: true
serviceName: "svc"
otlp:
  protocol: grpc
logs:
  enabled: true
`))
	require.NoError(t, err)

	// Confirm fuda did NOT inject the old 4318 default.
	require.Equal(t, "", cfg.OTLP.Endpoint, "loaded grpc config must have empty endpoint")

	base := cfg.GetOTLPConfig()
	protocol := effectiveProtocol(cfg, base)
	ep := effectiveEndpoint(cfg, base, protocol)
	assert.Equal(t, defaultEndpointGRPC, ep,
		"zaplog effectiveEndpoint must return 4317 for a loaded grpc config with no endpoint")

	// Construction must also succeed (zapwire does not dial at build time).
	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err, "NewCore must succeed for loaded grpc config with no endpoint")
	require.NotNil(t, core)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })
}

func TestNewCore_RejectsInvalidEndpointScheme(t *testing.T) {
	// A hand-built config bypasses LoadConfig's ValidateEndpoints, so NewCore is
	// the only validation seam. An invalid endpoint scheme (grpc://) must surface
	// the classifier's actionable error rather than being prefixed into garbage
	// ("https://grpc://collector:4317") and deferred to a dial failure. Protocol
	// is http/protobuf so NewCore routes to otlp.NewCore, not otlp.NewGRPCCore —
	// the endpoint check must be what rejects this.
	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP: &otx.OTLPConfig{
			Endpoint: "grpc://collector:4317",
			Protocol: "http/protobuf",
			Insecure: boolPtr(true),
		},
		Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid endpoint scheme "grpc"`)
	assert.Contains(t, err.Error(), "transport is selected by protocol")
	assert.Nil(t, core)
	assert.Nil(t, w)
}

func TestNewCore_DefaultProtocolWorks(t *testing.T) {
	// Empty effective protocol now defaults to http/protobuf (shared default
	// flipped to http/protobuf post-review; spec-aligned). NewCore must succeed
	// and produce a working writer that ships a record to a real HTTP server.
	cs := newCaptureServer(t)

	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		// Protocol is intentionally omitted — resolves to defaultProtocol = "http/protobuf".
		OTLP: &otx.OTLPConfig{Endpoint: cs.Server.URL, Insecure: boolPtr(true)},
		Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err, "default http/protobuf protocol must not be rejected")
	require.NotNil(t, core)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })

	zap.New(core).Info("default-proto-test")
	require.NoError(t, w.Sync())
	assert.Len(t, cs.allRecords(), 1)
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
