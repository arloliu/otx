package otx

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// restoreOTelGlobals snapshots the OTel global providers/propagator and restores
// them after the test, since the provider constructors mutate them.
func restoreOTelGlobals(t *testing.T) {
	t.Helper()
	tp := otel.GetTracerProvider()
	mp := otel.GetMeterProvider()
	prop := otel.GetTextMapPropagator()
	lp := logglobal.GetLoggerProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		otel.SetTextMapPropagator(prop)
		logglobal.SetLoggerProvider(lp)
	})
}

// ---------------------------------------------------------------------------
// OTLP/HTTP capture server (traces + metrics + logs), gzip-aware.
// ---------------------------------------------------------------------------

type otlpHTTPCapture struct {
	*httptest.Server

	mu           sync.Mutex
	traces       []*coltracepb.ExportTraceServiceRequest
	metrics      []*colmetricspb.ExportMetricsServiceRequest
	logs         []*collogspb.ExportLogsServiceRequest
	lastHeader   http.Header
	lastEncoding string
}

func newOTLPHTTPCapture(t *testing.T) *otlpHTTPCapture {
	t.Helper()
	c := &otlpHTTPCapture{}
	c.Server = httptest.NewServer(http.HandlerFunc(c.handle))
	t.Cleanup(c.Close)

	return c
}

// newFailingHTTPServer returns a server whose handler always responds with the
// given status code (for error-propagation tests).
func newFailingHTTPServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", status)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func (c *otlpHTTPCapture) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	if r.Header.Get("Content-Encoding") == "gzip" {
		zr, gerr := gzip.NewReader(bytes.NewReader(body))
		if gerr != nil {
			http.Error(w, gerr.Error(), http.StatusBadRequest)

			return
		}
		if body, gerr = io.ReadAll(zr); gerr != nil {
			http.Error(w, gerr.Error(), http.StatusBadRequest)

			return
		}
	}

	c.mu.Lock()
	c.lastHeader = r.Header.Clone()
	c.lastEncoding = r.Header.Get("Content-Encoding")
	switch r.URL.Path {
	case "/v1/traces":
		var req coltracepb.ExportTraceServiceRequest
		if proto.Unmarshal(body, &req) == nil {
			c.traces = append(c.traces, &req)
		}
	case "/v1/metrics":
		var req colmetricspb.ExportMetricsServiceRequest
		if proto.Unmarshal(body, &req) == nil {
			c.metrics = append(c.metrics, &req)
		}
	case "/v1/logs":
		var req collogspb.ExportLogsServiceRequest
		if proto.Unmarshal(body, &req) == nil {
			c.logs = append(c.logs, &req)
		}
	}
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *otlpHTTPCapture) spanNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var names []string
	for _, req := range c.traces {
		for _, rs := range req.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				for _, s := range ss.GetSpans() {
					names = append(names, s.GetName())
				}
			}
		}
	}

	return names
}

func (c *otlpHTTPCapture) traceResourceAttrs() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, req := range c.traces {
		for _, rs := range req.GetResourceSpans() {
			return kvMap(rs.GetResource().GetAttributes())
		}
	}

	return nil
}

func (c *otlpHTTPCapture) logResourceAttrs() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, req := range c.logs {
		for _, rl := range req.GetResourceLogs() {
			return kvMap(rl.GetResource().GetAttributes())
		}
	}

	return nil
}

func (c *otlpHTTPCapture) metricNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var names []string
	for _, req := range c.metrics {
		for _, rm := range req.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					names = append(names, m.GetName())
				}
			}
		}
	}

	return names
}

func (c *otlpHTTPCapture) header() (http.Header, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastHeader, c.lastEncoding
}

func kvMap(attrs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.GetKey()] = a.GetValue().GetStringValue()
	}

	return m
}

// ---------------------------------------------------------------------------
// OTLP/gRPC trace capture server (real TCP listener so the SDK can dial it).
// ---------------------------------------------------------------------------

type otlpGRPCTraceCapture struct {
	coltracepb.UnimplementedTraceServiceServer
	addr string
	srv  *grpc.Server

	mu      sync.Mutex
	spanNum int
}

func newOTLPGRPCTraceCapture(t *testing.T) *otlpGRPCTraceCapture {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	c := &otlpGRPCTraceCapture{addr: lis.Addr().String(), srv: grpc.NewServer()}
	coltracepb.RegisterTraceServiceServer(c.srv, c)
	go func() { _ = c.srv.Serve(lis) }()
	t.Cleanup(c.srv.Stop)

	return c
}

func (c *otlpGRPCTraceCapture) Export(
	_ context.Context, req *coltracepb.ExportTraceServiceRequest,
) (*coltracepb.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			c.spanNum += len(ss.GetSpans())
		}
	}
	c.mu.Unlock()

	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func (c *otlpGRPCTraceCapture) spans() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.spanNum
}

// ---------------------------------------------------------------------------
// Config builder
// ---------------------------------------------------------------------------

func e2eConfig(endpoint, protocol string) *TelemetryConfig {
	return &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "e2e-svc",
		Version:     "1.2.3",
		Environment: "staging",
		OTLP:        &OTLPConfig{Endpoint: endpoint, Protocol: protocol, Insecure: BoolPtr(true)},
		Traces:      &TracesConfig{Enabled: BoolPtr(true), Exporter: "otlp"},
		Metrics:     &MetricsConfig{Enabled: BoolPtr(true), Exporter: "otlp", Interval: time.Minute},
	}
}

// ---------------------------------------------------------------------------
// H1 — trace & metric export reach a listener over the wire.
// ---------------------------------------------------------------------------

func TestExportE2E_HTTP_TraceReachesCollector(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t)

	tp, err := NewTracerProvider(context.Background(), e2eConfig(coll.URL, "http/protobuf"))
	require.NoError(t, err)

	_, span := tp.Tracer("e2e").Start(context.Background(), "exported-span")
	span.End()
	require.NoError(t, tp.ForceFlush(context.Background()))
	require.NoError(t, tp.Shutdown(context.Background()))

	assert.Contains(t, coll.spanNames(), "exported-span")
	assert.Equal(t, "e2e-svc", coll.traceResourceAttrs()["service.name"])
}

func TestExportE2E_HTTP_MetricReachesCollector(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t)

	mp, err := NewMeterProvider(context.Background(), e2eConfig(coll.URL, "http/protobuf"))
	require.NoError(t, err)

	ctr, err := mp.Meter("e2e").Int64Counter("requests.total")
	require.NoError(t, err)
	ctr.Add(context.Background(), 1)
	require.NoError(t, mp.ForceFlush(context.Background()))
	require.NoError(t, mp.Shutdown(context.Background()))

	assert.Contains(t, coll.metricNames(), "requests.total")
}

// ---------------------------------------------------------------------------
// M1 — custom headers + gzip compression are observable on the wire.
// ---------------------------------------------------------------------------

func TestExportE2E_HTTP_HeadersAndGzip(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t)

	cfg := e2eConfig(coll.URL, "http/protobuf")
	cfg.OTLP.Headers = map[string]string{"x-otx-test": "abc"}
	cfg.OTLP.Compression = "gzip"

	tp, err := NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	_, span := tp.Tracer("e2e").Start(context.Background(), "s")
	span.End()
	require.NoError(t, tp.ForceFlush(context.Background()))
	require.NoError(t, tp.Shutdown(context.Background()))

	require.NotEmpty(t, coll.spanNames(), "span must arrive (body gzip-decoded)")
	hdr, enc := coll.header()
	assert.Equal(t, "gzip", enc, "Content-Encoding must be gzip")
	require.NotNil(t, hdr)
	assert.Equal(t, "abc", hdr.Get("x-otx-test"), "custom header must reach the wire")
}

// ---------------------------------------------------------------------------
// M2 — flush on shutdown, double-shutdown idempotency, error propagation.
// ---------------------------------------------------------------------------

func TestExportE2E_HTTP_FlushOnShutdown(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t)

	tp, err := NewTracerProvider(context.Background(), e2eConfig(coll.URL, "http/protobuf"))
	require.NoError(t, err)

	const n = 5
	for i := 0; i < n; i++ {
		_, span := tp.Tracer("e2e").Start(context.Background(), "s")
		span.End()
	}
	// No explicit ForceFlush: Shutdown must drain the batch processor.
	require.NoError(t, tp.Shutdown(context.Background()))
	assert.Len(t, coll.spanNames(), n, "all spans must be flushed on shutdown")

	// Double shutdown must be a safe no-op.
	assert.NoError(t, tp.Shutdown(context.Background()))
}

func TestExportE2E_HTTP_ForceFlushSurfacesExporterError(t *testing.T) {
	restoreOTelGlobals(t)
	srv := newFailingHTTPServer(t, http.StatusBadRequest)

	cfg := e2eConfig(srv.URL, "http/protobuf")
	cfg.OTLP.Timeout = 2 * time.Second
	tp, err := NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("e2e").Start(context.Background(), "s")
	span.End()
	// A permanent 4xx is a non-retryable export failure; ForceFlush must surface it.
	assert.Error(t, tp.ForceFlush(context.Background()))
}

// ---------------------------------------------------------------------------
// M3 — gRPC transport selection works, and the URL scheme drives TLS.
// ---------------------------------------------------------------------------

func TestExportE2E_GRPC_TraceReachesCollector(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPGRPCTraceCapture(t)

	tp, err := NewTracerProvider(context.Background(), e2eConfig(coll.addr, "grpc"))
	require.NoError(t, err)

	_, span := tp.Tracer("e2e").Start(context.Background(), "grpc-span")
	span.End()
	require.NoError(t, tp.ForceFlush(context.Background()))
	require.NoError(t, tp.Shutdown(context.Background()))

	assert.Equal(t, 1, coll.spans(), "span must arrive over gRPC")
}

func TestExportE2E_HTTP_HTTPSSchemeForcesTLS(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t) // plaintext server

	// Pointing an https:// endpoint at a plaintext server must attempt a TLS
	// handshake (scheme overrides Insecure), which fails — proving TLS selection.
	httpsEndpoint := "https://" + coll.Listener.Addr().String()
	cfg := e2eConfig(httpsEndpoint, "http/protobuf")
	cfg.OTLP.Timeout = 2 * time.Second
	tp, err := NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("e2e").Start(context.Background(), "s")
	span.End()
	assert.Error(t, tp.ForceFlush(context.Background()), "TLS handshake to plaintext server must fail")
	assert.Empty(t, coll.spanNames(), "no span should be decoded from a TLS attempt")
}

// D2 — a URL-form OTLP endpoint must route logs to /v1/logs, not "/".
// otlploghttp's WithEndpointURL does not default the path; withLogsPath fixes it.
func TestExportE2E_HTTP_LogURLEndpointRoutesToV1Logs(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t)

	cfg := e2eConfig(coll.URL, "http/protobuf") // URL form (http://host:port)
	cfg.Logs = &LogsConfig{Enabled: BoolPtr(true), Exporter: "otlp"}

	lp, err := NewLoggerProvider(context.Background(), cfg)
	require.NoError(t, err)

	var rec otellog.Record
	rec.SetBody(otellog.StringValue("hello"))
	lp.Logger("e2e").Emit(context.Background(), rec)
	require.NoError(t, lp.ForceFlush(context.Background()))
	require.NoError(t, lp.Shutdown(context.Background()))

	// The capture server only records logs received at /v1/logs.
	assert.Equal(t, "e2e-svc", coll.logResourceAttrs()["service.name"],
		"logs must reach /v1/logs even with a URL-form endpoint")
}

// ---------------------------------------------------------------------------
// M12 — traces and logs reach the collector with identical resource identity.
// ---------------------------------------------------------------------------

func TestExportE2E_TraceLogResourceParity(t *testing.T) {
	restoreOTelGlobals(t)
	coll := newOTLPHTTPCapture(t)

	// Use the bare host:port form so both the trace (v1.x) and log (v0.20) HTTP
	// exporters route to their /v1/* paths consistently.
	cfg := e2eConfig(strings.TrimPrefix(coll.URL, "http://"), "http/protobuf")
	cfg.Logs = &LogsConfig{Enabled: BoolPtr(true), Exporter: "otlp"}

	tp, err := NewTracerProvider(context.Background(), cfg)
	require.NoError(t, err)
	lp, err := NewLoggerProvider(context.Background(), cfg)
	require.NoError(t, err)

	_, span := tp.Tracer("e2e").Start(context.Background(), "s")
	span.End()

	var rec otellog.Record
	rec.SetBody(otellog.StringValue("hello"))
	lp.Logger("e2e").Emit(context.Background(), rec)

	require.NoError(t, tp.ForceFlush(context.Background()))
	require.NoError(t, lp.ForceFlush(context.Background()))
	require.NoError(t, tp.Shutdown(context.Background()))
	require.NoError(t, lp.Shutdown(context.Background()))

	traceAttrs := coll.traceResourceAttrs()
	logAttrs := coll.logResourceAttrs()
	require.NotEmpty(t, traceAttrs)
	require.NotEmpty(t, logAttrs)
	assert.Equal(t, traceAttrs, logAttrs, "trace and log resource identity must match")
	assert.Equal(t, "e2e-svc", traceAttrs["service.name"])
}
