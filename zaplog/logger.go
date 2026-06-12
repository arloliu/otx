package zaplog

import (
	"context"

	"github.com/arloliu/otx"
	"github.com/arloliu/zapwire/otlp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Attach tees an OTLP core onto an existing zap logger using zap.WrapCore +
// zapcore.NewTee, so the returned logger writes to both the original sink(s)
// and the OTLP core. Level gating on the OTLP side is whatever LevelEnabler
// NewCore received (pass a *zap.AtomicLevel for a runtime cost dial).
//
// The original logger is not mutated; zap.WrapCore returns a clone.
func Attach(logger *zap.Logger, core zapcore.Core) *zap.Logger {
	return logger.WithOptions(zap.WrapCore(func(existing zapcore.Core) zapcore.Core {
		return zapcore.NewTee(existing, core)
	}))
}

// Logger wraps *zap.Logger with context-aware logging methods built on
// zapwire's trace-field injectors — the app-layer surface zapwire deliberately
// omits. Because it embeds *zap.Logger, every standard zap method
// (Sugar, WithOptions, DPanic/Panic/Fatal, Sync, ...) remains reachable; only
// the ctx-aware methods and the wrapper-preserving With/Named are added here.
//
// For a context-aware Panic/Fatal, call the embedded logger directly with the
// injector, e.g. l.Logger.Fatal(msg, otlp.InjectTraceFields(ctx, fields...)...).
type Logger struct {
	*zap.Logger
}

// DebugCtx logs at debug level, injecting the active span context from ctx so
// the OTLP core populates the LogRecord trace fields (and tee'd non-OTLP cores
// render span_context legibly).
func (l Logger) DebugCtx(ctx context.Context, msg string, fields ...zap.Field) {
	l.Logger.Debug(msg, otlp.InjectTraceFields(ctx, fields...)...)
}

// InfoCtx logs at info level with the active span context injected.
func (l Logger) InfoCtx(ctx context.Context, msg string, fields ...zap.Field) {
	l.Logger.Info(msg, otlp.InjectTraceFields(ctx, fields...)...)
}

// WarnCtx logs at warn level with the active span context injected.
func (l Logger) WarnCtx(ctx context.Context, msg string, fields ...zap.Field) {
	l.Logger.Warn(msg, otlp.InjectTraceFields(ctx, fields...)...)
}

// ErrorCtx logs at error level with the active span context injected.
func (l Logger) ErrorCtx(ctx context.Context, msg string, fields ...zap.Field) {
	l.Logger.Error(msg, otlp.InjectTraceFields(ctx, fields...)...)
}

// With returns a child Logger with the given fields added, preserving the
// Logger wrapper (unlike the embedded *zap.Logger.With, which returns
// *zap.Logger and would drop the ctx-aware methods).
func (l Logger) With(fields ...zap.Field) Logger {
	return Logger{Logger: l.Logger.With(fields...)}
}

// Named returns a child Logger with the given name segment added, preserving
// the Logger wrapper.
func (l Logger) Named(name string) Logger {
	return Logger{Logger: l.Logger.Named(name)}
}

// Wrap returns a ctx-aware Logger over an existing *zap.Logger. Use it when
// you composed the core yourself (NewCore + Attach).
func Wrap(l *zap.Logger) Logger { return Logger{Logger: l} }

// New is the one-call setup: it builds the zapwire OTLP core from cfg
// (NewCore semantics: nil level resolves Logs.MinLevel then info), tees it
// onto base (Attach), and returns the ctx-aware Logger plus the Writer the
// caller must Close at shutdown (w.Sync() then w.Close() for the full retry
// budget). A nil base returns a Logger writing to the OTLP core only.
//
// See NewCore for endpoint/protocol/level resolution, and Attach for tee
// semantics.
func New(
	ctx context.Context,
	cfg *otx.TelemetryConfig,
	base *zap.Logger,
	level zapcore.LevelEnabler,
	opts ...otlp.Option,
) (Logger, *otlp.Writer, error) {
	core, w, err := NewCore(ctx, cfg, level, opts...)
	if err != nil {
		return Logger{}, nil, err
	}
	if base == nil {
		return Wrap(zap.New(core)), w, nil
	}

	return Wrap(Attach(base, core)), w, nil
}
