package zaplog

import (
	"bytes"
	"context"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/arloliu/otx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestNew_BothSinks verifies that New with a base console/JSON-buffer logger
// routes a single InfoCtx call to both the console and the OTLP sinks, and
// that the OTLP LogRecord carries the injected trace_id.
func TestNew_BothSinks(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	var buf bytes.Buffer
	var mu sync.Mutex
	base := zap.New(newConsoleCore(&buf, &mu))

	log, w, err := New(context.Background(), cfg, base, zapcore.InfoLevel)
	require.NoError(t, err)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })

	ctx, traceID, _ := ctxWithTrace(t, "0102030405060708090a0b0c0d0e0f10", "0102030405060708")
	log.InfoCtx(ctx, "both-sinks-new")
	require.NoError(t, w.Sync())
	require.NoError(t, log.Sync())

	// Console sink received the message.
	mu.Lock()
	consoleOut := buf.String()
	mu.Unlock()
	assert.Contains(t, consoleOut, "both-sinks-new")

	// OTLP sink received it with the correct trace_id.
	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "both-sinks-new", recs[0].GetBody().GetStringValue())
	assert.Equal(t, traceID[:], recs[0].GetTraceId(), "InfoCtx must populate LogRecord.trace_id")
}

// TestNew_NilBase writes only to the OTLP core when base is nil.
func TestNew_NilBase(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	log, w, err := New(context.Background(), cfg, nil, zapcore.InfoLevel)
	require.NoError(t, err)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })

	// Must not panic.
	log.Info("otlp-only")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "otlp-only", recs[0].GetBody().GetStringValue())
}

// TestNew_PropagatesNewCoreError verifies that a NewCore error (e.g. invalid
// service name) propagates through New as a zero Logger and nil Writer.
func TestNew_PropagatesNewCoreError(t *testing.T) {
	cfg := &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "", // missing service name → ErrServiceNameRequired
		OTLP:        &otx.OTLPConfig{Endpoint: "collector:4318", Protocol: "http/protobuf", Insecure: boolPtr(true)},
		Logs:        &otx.LogsConfig{Enabled: boolPtr(true)},
	}

	got, w, err := New(context.Background(), cfg, nil, zapcore.InfoLevel)
	require.Error(t, err)
	require.Nil(t, got.Logger)
	require.Nil(t, w)
}

// TestNew_LevelPassthrough verifies that an explicit WarnLevel enabler blocks
// Info records and passes Warn records through to the OTLP sink.
func TestNew_LevelPassthrough(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	lvl := zap.NewAtomicLevelAt(zapcore.WarnLevel)
	log, w, err := New(context.Background(), cfg, nil, &lvl)
	require.NoError(t, err)
	require.NotNil(t, w)
	t.Cleanup(func() { _ = w.Close() })

	log.Info("should-be-dropped")
	log.Warn("should-be-exported")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "should-be-exported", recs[0].GetBody().GetStringValue())
}

// TestWrap_IdentityPreserved verifies that Wrap returns a Logger whose
// embedded *zap.Logger is exactly the pointer that was passed in.
func TestWrap_IdentityPreserved(t *testing.T) {
	l := zap.NewNop()
	require.Same(t, l, Wrap(l).Logger)
}

// TestAttach_NilLogger verifies that Attach with a nil *zap.Logger returns a
// usable logger over the OTLP core instead of panicking.
func TestAttach_NilLogger(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	var teed *zap.Logger
	require.NotPanics(t, func() { teed = Attach(nil, core) })
	require.NotNil(t, teed)

	teed.Info("nil-base")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "nil-base", recs[0].GetBody().GetStringValue())
}

// recordingCore counts Check calls and captures the fields handed to Write so a
// test can prove the ctx-aware methods only inject trace fields for entries that
// pass the level gate.
type recordingCore struct {
	zapcore.LevelEnabler
	mu      sync.Mutex
	written [][]zapcore.Field
}

func (c *recordingCore) With(_ []zapcore.Field) zapcore.Core { return c }

func (c *recordingCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}

	return ce
}

func (c *recordingCore) Write(_ zapcore.Entry, fields []zapcore.Field) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, fields)

	return nil
}

func (*recordingCore) Sync() error { return nil }

func (c *recordingCore) snapshot() [][]zapcore.Field {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]zapcore.Field, len(c.written))
	copy(out, c.written)

	return out
}

// TestLogger_CtxMethods_PreGateLevel verifies the ctx-aware methods do not write
// (and therefore do not inject trace fields) when the level is disabled, while
// an enabled-level call still injects the span_context field.
func TestLogger_CtxMethods_PreGateLevel(t *testing.T) {
	rc := &recordingCore{LevelEnabler: zapcore.WarnLevel}
	log := Logger{Logger: zap.New(rc)}
	ctx, _, _ := ctxWithTrace(t, "0123456789abcdef0123456789abcdef", "0123456789abcdef")

	// Disabled level: no Write, hence no trace-field injection reaches a core.
	log.DebugCtx(ctx, "dropped")
	log.InfoCtx(ctx, "dropped")
	written := rc.snapshot()
	require.Empty(t, written, "disabled-level ctx call must not write any entry")

	// Enabled level: the span_context field must be injected.
	log.WarnCtx(ctx, "kept", zap.String("user", "alice"))
	log.ErrorCtx(ctx, "kept")
	written = rc.snapshot()
	require.Len(t, written, 2)
	for _, fields := range written {
		assert.True(t, hasSpanContextField(fields),
			"enabled-level ctx call must inject the span_context field")
	}
	// The caller's own field is preserved alongside the injected one.
	assert.Equal(t, "user", written[0][0].Key)
}

// TestLogger_CtxMethods_DisabledLevel_NoAlloc pins the perf property behind the
// pre-gate: a disabled-level ctx call must not allocate. The pre-`Check` version
// called otlp.InjectTraceFields unconditionally, which allocates a field slice
// plus a boxed span context, so a regression to that shape trips this guard.
func TestLogger_CtxMethods_DisabledLevel_NoAlloc(t *testing.T) {
	rc := &recordingCore{LevelEnabler: zapcore.WarnLevel}
	log := Logger{Logger: zap.New(rc)}
	ctx, _, _ := ctxWithTrace(t, "0123456789abcdef0123456789abcdef", "0123456789abcdef")

	allocs := testing.AllocsPerRun(100, func() {
		log.DebugCtx(ctx, "dropped")
	})
	assert.Zero(t, allocs, "disabled-level ctx log must not allocate (pre-gate bypassed)")
}

func hasSpanContextField(fields []zapcore.Field) bool {
	for _, f := range fields {
		if f.Key == "span_context" {
			return true
		}
	}

	return false
}

// ctxWithTrace builds a context carrying a valid, sampled span context from the
// given hex IDs.
func ctxWithTrace(t *testing.T, traceHex, spanHex string) (context.Context, trace.TraceID, trace.SpanID) {
	t.Helper()
	tid, err := trace.TraceIDFromHex(traceHex)
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex(spanHex)
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	})

	return trace.ContextWithSpanContext(context.Background(), sc), tid, sid
}

// newConsoleCore returns a console core writing to buf (with a mutex-guarded
// writer so the test can read it safely after Sync).
func newConsoleCore(buf *bytes.Buffer, mu *sync.Mutex) zapcore.Core {
	enc := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	ws := zapcore.AddSync(&lockedWriter{buf: buf, mu: mu})

	return zapcore.NewCore(enc, ws, zapcore.DebugLevel)
}

type lockedWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

func TestAttach_TeePreservesConsole(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	var buf bytes.Buffer
	var mu sync.Mutex
	console := zap.New(newConsoleCore(&buf, &mu))

	teed := Attach(console, core)
	teed.Info("both-sinks", zap.String("k", "v"))
	require.NoError(t, w.Sync())
	require.NoError(t, teed.Sync())

	// Console sink received it.
	mu.Lock()
	consoleOut := buf.String()
	mu.Unlock()
	assert.Contains(t, consoleOut, "both-sinks")

	// OTLP sink received it.
	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "both-sinks", recs[0].GetBody().GetStringValue())
}

func TestLogger_InfoCtx_InjectsTraceID(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	log := Logger{Logger: zap.New(core)}

	// Synthesize a valid span context directly (no SDK needed for this unit).
	ctx, traceID, spanID := ctxWithTrace(t, "0123456789abcdef0123456789abcdef", "0123456789abcdef")

	log.InfoCtx(ctx, "with-trace")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, traceID[:], recs[0].GetTraceId(), "InfoCtx must populate LogRecord.trace_id")
	assert.Equal(t, spanID[:], recs[0].GetSpanId())
}

func TestLogger_StickyContextField(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	ctx, traceID, _ := ctxWithTrace(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb")

	// Sticky zap.Any("context", ctx) through the custom core (design §2.2).
	reqLog := Logger{Logger: zap.New(core)}.With(zap.Any("context", ctx))
	reqLog.Info("sticky")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, traceID[:], recs[0].GetTraceId(), "sticky context must reach LogRecord.trace_id")
}

func TestLogger_WithNamed_PreserveWrapper(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	log := Logger{Logger: zap.New(core)}
	child := log.With(zap.String("component", "api")).Named("sub")
	// Type identity: With/Named return Logger, so ctx methods stay reachable.
	child.InfoCtx(context.Background(), "child-msg")
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "child-msg", recs[0].GetBody().GetStringValue())
}

// TestAttach_TeeSpanContextInNonOTLP verifies the §4 matrix row: a logger whose
// OTLP core is tee'd behind a JSON core receives InfoCtx with a valid span
// context; the non-OTLP (JSON) output must contain a legible "span_context"
// field, and the OTLP body must carry the trace_id in the LogRecord fields.
func TestAttach_TeeSpanContextInNonOTLP(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	otlpCore, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	var buf bytes.Buffer
	var mu sync.Mutex
	jsonEnc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	jsonWS := zapcore.AddSync(&lockedWriter{buf: &buf, mu: &mu})
	jsonCore := zapcore.NewCore(jsonEnc, jsonWS, zapcore.DebugLevel)

	base := zap.New(jsonCore)
	teed := Attach(base, otlpCore)
	log := Logger{Logger: teed}

	ctx, traceID, _ := ctxWithTrace(t, "fedcba9876543210fedcba9876543210", "fedcba9876543210")

	log.InfoCtx(ctx, "tee-trace-test")
	require.NoError(t, w.Sync())
	require.NoError(t, teed.Sync())

	// Non-OTLP (JSON) core must contain a legible "span_context" field.
	mu.Lock()
	jsonOut := buf.String()
	mu.Unlock()
	assert.Contains(t, jsonOut, "span_context",
		"tee'd JSON core must render the span_context field from InjectTraceFields")

	// OTLP core must carry the trace_id in the LogRecord binary field.
	recs := cs.allRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, traceID[:], recs[0].GetTraceId(),
		"OTLP LogRecord.trace_id must match the injected span context")
}

func TestShutdown_SyncThenClose(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)

	zap.New(core).Info("flush-me")
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	assert.Len(t, cs.allRecords(), 1)
	// DroppedLogs readable after Close.
	assert.Equal(t, uint64(0), w.DroppedLogs())
}

func TestShutdown_CloseOnly(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)

	zap.New(core).Info("drain-me")
	// Close alone drains with single attempts.
	require.NoError(t, w.Close())

	// DroppedLogs remains readable after Close.
	_ = w.DroppedLogs()
}

func TestEndToEnd_OTXStartSpan_TraceIDInBody(t *testing.T) {
	cs := newCaptureServer(t)
	cfg := httpCfg(cs.Server.URL)

	// Real TracerProvider with AlwaysSample so otx.Start produces a recording,
	// valid span context.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otx.InitTracing(tp.Tracer("test"), otx.DefaultNamer{})

	core, w, err := NewCore(context.Background(), cfg, zapcore.InfoLevel)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	log := Logger{Logger: zap.New(core)}

	ctx, span := otx.Start(context.Background(), "e2e-op")
	wantTrace := span.SpanContext().TraceID()
	require.True(t, wantTrace.IsValid(), "otx.Start must yield a valid span context")
	log.InfoCtx(ctx, "operation done")
	span.End()
	require.NoError(t, w.Sync())

	recs := cs.allRecords()
	require.Len(t, recs, 1)
	gotTrace := hex.EncodeToString(recs[0].GetTraceId())
	assert.Equal(t, wantTrace.String(), gotTrace,
		"captured OTLP body trace_id must match the otx.Start span")
}
