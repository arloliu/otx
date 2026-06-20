package zaplog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/arloliu/otx"
)

// grpcRecords flattens every LogRecord across captured gRPC export requests.
func grpcRecords(reqs []*collogspb.ExportLogsServiceRequest) []*logspb.LogRecord {
	var recs []*logspb.LogRecord
	for _, req := range reqs {
		for _, rl := range req.GetResourceLogs() {
			for _, sl := range rl.GetScopeLogs() {
				recs = append(recs, sl.GetLogRecords()...)
			}
		}
	}

	return recs
}

// H8 — the gRPC transport must carry trace_id/span_id on the LogRecord and the
// resource identity, not just the body. The existing gRPC e2e test logged
// without a span context, so trace correlation was only ever proven on the HTTP
// transport (a separate hand-rolled OTLP/gRPC client).
func TestNewCore_GRPCEndToEnd_TraceIDAndResource(t *testing.T) {
	addr, svc := newGRPCCaptureServer(t)

	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "grpc-trace-svc",
		OTLP: &otx.OTLPConfig{
			Endpoint: addr,
			Protocol: "grpc",
			Insecure: boolPtr(true),
		},
		Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	log := Logger{Logger: zap.New(core)}
	ctx, traceID, spanID := ctxWithTrace(t, "0123456789abcdef0123456789abcdef", "0123456789abcdef")
	log.InfoCtx(ctx, "grpc-trace-message")
	require.NoError(t, w.Sync())

	reqs := svc.allRequests()
	recs := grpcRecords(reqs)
	require.Len(t, recs, 1)

	// trace correlation must survive the gRPC transport.
	assert.Equal(t, traceID[:], recs[0].GetTraceId(), "trace_id must reach the LogRecord over gRPC")
	assert.Equal(t, spanID[:], recs[0].GetSpanId(), "span_id must reach the LogRecord over gRPC")
	assert.Equal(t, "grpc-trace-message", recs[0].GetBody().GetStringValue())

	// resource parity on the gRPC path.
	var foundSvc bool
	for _, rl := range reqs[0].GetResourceLogs() {
		for _, attr := range rl.GetResource().GetAttributes() {
			if attr.GetKey() == serviceNameKey && attr.GetValue().GetStringValue() == "grpc-trace-svc" {
				foundSvc = true
			}
		}
	}
	assert.True(t, foundSvc, "service.name must appear on the gRPC ResourceLogs")
}
