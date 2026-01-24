package tracker

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"
)

// Namer determines how span names are formatted.
type Namer interface {
	Name(string) string
}

type defaultNamer struct{}

func (defaultNamer) Name(s string) string { return s }

type state struct {
	tracer trace.Tracer
	namer  Namer
}

var global atomic.Pointer[state]

func init() {
	global.Store(&state{namer: defaultNamer{}})
}

// Set updates the global tracing state.
// If n is nil, defaultNamer is used.
func Set(t trace.Tracer, n Namer) {
	if n == nil {
		n = defaultNamer{}
	}
	global.Store(&state{tracer: t, namer: n})
}

// Start begins a new span using the global tracer and namer.
// If no tracer is configured, it returns the current span from context (no-op).
func Start(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	s := global.Load()
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}

	return s.tracer.Start(ctx, s.namer.Name(operation), opts...)
}

// Tracer returns the configured global tracer, or nil if not set.
func Tracer() trace.Tracer {
	return global.Load().tracer
}
