package tracker

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// restoreGlobal snapshots and restores the package global so cases don't leak.
func restoreGlobal(t *testing.T) {
	t.Helper()
	prev := global.Load()
	t.Cleanup(func() { global.Store(prev) })
}

type prefixNamer struct{ prefix string }

func (p prefixNamer) Name(s string) string { return p.prefix + s }

func recorderTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	return rec, tp
}

// L1 — a custom Namer must reach the recorded span name.
func TestStart_UsesNamerForSpanName(t *testing.T) {
	restoreGlobal(t)
	rec, tp := recorderTracer(t)
	Set(tp.Tracer("test"), prefixNamer{prefix: "svc:"})

	_, span := Start(context.Background(), "operation")
	span.End()

	ended := rec.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "svc:operation", ended[0].Name(), "custom namer must rename the span")
}

func TestSet_NilNamer_UsesDefault(t *testing.T) {
	restoreGlobal(t)
	rec, tp := recorderTracer(t)
	Set(tp.Tracer("test"), nil) // nil namer -> default (identity)

	_, span := Start(context.Background(), "operation")
	span.End()

	ended := rec.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "operation", ended[0].Name())
}

func TestStart_NoTracer_DoesNotRecord(t *testing.T) {
	restoreGlobal(t)
	Set(nil, nil) // no tracer configured

	rec, _ := recorderTracer(t) // unrelated recorder; must stay empty
	_, span := Start(context.Background(), "operation")
	span.End()

	assert.False(t, span.SpanContext().IsValid(), "no tracer -> no recording span")
	assert.Empty(t, rec.Ended())
}

func TestTracer_ReturnsConfigured(t *testing.T) {
	restoreGlobal(t)
	assert.Nil(t, Tracer(), "default state has no tracer")

	_, tp := recorderTracer(t)
	tr := tp.Tracer("test")
	Set(tr, nil)
	assert.Equal(t, tr, Tracer())
}

// L9 — concurrent Set/Start/Tracer must be race-free (run under -race).
func TestConcurrent_SetStartTracer(t *testing.T) {
	restoreGlobal(t)
	_, tp := recorderTracer(t)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(3)
		go func() { defer wg.Done(); Set(tp.Tracer("a"), prefixNamer{prefix: "p:"}) }()
		go func() { defer wg.Done(); _, s := Start(context.Background(), "op"); s.End() }()
		go func() { defer wg.Done(); _ = Tracer() }()
	}
	wg.Wait()
}
