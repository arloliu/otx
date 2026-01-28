// Package engine provides the trace/log generation engine for the OTLP simulator.
package engine

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/arloliu/otx"
	"github.com/arloliu/otx/cmd/otlp-sim/scenario"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Engine generates traces and logs from scenarios.
type Engine struct {
	tracerProvider *sdktrace.TracerProvider
	loggerProvider *sdklog.LoggerProvider
	enableLogs     bool
	jitterPct      int
	serviceName    string
}

// Config holds engine configuration.
type Config struct {
	Endpoint    string
	UseHTTP     bool
	Insecure    bool
	ServiceName string
	EnableLogs  bool
	JitterPct   int
}

// New creates a new Engine with the given configuration.
func New(ctx context.Context, cfg Config) (*Engine, error) {
	// Build otx telemetry config
	protocol := "grpc"
	if cfg.UseHTTP {
		protocol = "http"
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "otlp-sim"
	}

	telCfg := &otx.TelemetryConfig{
		ServiceName: serviceName,
		OTLP: &otx.OTLPConfig{
			Endpoint: cfg.Endpoint,
			Protocol: protocol,
			Insecure: &cfg.Insecure,
		},
		Traces: &otx.TracesConfig{},
	}

	// Initialize tracer provider
	tp, err := otx.NewTracerProvider(ctx, telCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer provider: %w", err)
	}

	e := &Engine{
		tracerProvider: tp,
		enableLogs:     cfg.EnableLogs,
		jitterPct:      cfg.JitterPct,
		serviceName:    serviceName,
	}

	// Initialize logger provider if logs enabled
	if cfg.EnableLogs {
		enabled := true
		telCfg.Logs = &otx.LogsConfig{Enabled: &enabled}
		lp, err := otx.NewLoggerProvider(ctx, telCfg)
		if err != nil {
			// Log provider is optional, continue without it
			fmt.Printf("Warning: failed to create logger provider: %v\n", err)
		} else {
			e.loggerProvider = lp
		}
	}

	return e, nil
}

// Shutdown flushes and closes providers.
func (e *Engine) Shutdown(ctx context.Context) error {
	var errs []error
	if e.tracerProvider != nil {
		if err := e.tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if e.loggerProvider != nil {
		if err := e.loggerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// GenerateTrace generates a complete trace from a scenario.
func (e *Engine) GenerateTrace(ctx context.Context, s *scenario.Scenario) error {
	return e.generateSpan(ctx, s.RootSpan, nil)
}

// generateSpan recursively generates a span and its children.
func (e *Engine) generateSpan(
	ctx context.Context,
	tmpl scenario.SpanTemplate,
	parentSpan trace.Span,
) error {
	// Determine service name for this span
	serviceName := tmpl.Service
	if e.serviceName != "" && parentSpan == nil {
		// Only override root span service name
		serviceName = e.serviceName
	}

	// Create tracer for this service
	tracer := otel.Tracer(serviceName)

	// Convert span kind
	kind := toTraceSpanKind(tmpl.Kind)

	// Build attributes
	attrs := parseAttributes(tmpl.Attributes)

	// Start span
	spanCtx := ctx
	if parentSpan != nil {
		spanCtx = trace.ContextWithSpan(ctx, parentSpan)
	}

	_, span := tracer.Start(spanCtx, tmpl.Name,
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrs...),
	)

	// Calculate duration with jitter
	duration := e.applyJitter(tmpl.Duration.AsDuration())

	// Generate logs if enabled and provider available
	if e.enableLogs && e.loggerProvider != nil {
		e.generateLogs(spanCtx, tmpl.Logs, span)
	}

	// Check for error simulation
	if tmpl.ErrorRate > 0 && rand.Float64() < tmpl.ErrorRate { //nolint:gosec // weak rand is fine for simulation
		span.SetStatus(codes.Error, tmpl.ErrorStatus)
		span.RecordError(fmt.Errorf("%s", tmpl.ErrorStatus))
	}

	// Generate child spans
	for _, child := range tmpl.Children {
		if err := e.generateSpan(spanCtx, child, span); err != nil {
			span.End()
			return err
		}
	}

	// Simulate duration by sleeping
	time.Sleep(duration)

	span.End()

	return nil
}

// generateLogs generates log entries for a span.
func (*Engine) generateLogs(ctx context.Context, logs []scenario.LogTemplate, _ trace.Span) {
	logger := global.GetLoggerProvider().Logger("otlp-sim")

	for _, l := range logs {
		// Build log record
		var rec otellog.Record
		rec.SetBody(otellog.StringValue(l.Message))
		rec.SetSeverity(toLogSeverity(l.Level))

		attrs := make([]otellog.KeyValue, 0, len(l.Attributes))
		for k, v := range l.Attributes {
			attrs = append(attrs, otellog.String(k, v))
		}
		rec.AddAttributes(attrs...)

		// Emit using the logger with span context
		logger.Emit(ctx, rec)
	}
}

// applyJitter adds random timing variation to a duration.
func (e *Engine) applyJitter(d time.Duration) time.Duration {
	if e.jitterPct <= 0 {
		return d
	}
	jitter := float64(d) * float64(e.jitterPct) / 100.0
	offset := (rand.Float64() * 2 * jitter) - jitter //nolint:gosec // weak rand is fine for jitter

	return d + time.Duration(offset)
}

func toTraceSpanKind(k scenario.SpanKind) trace.SpanKind {
	switch k {
	case scenario.SpanKindServer:
		return trace.SpanKindServer
	case scenario.SpanKindClient:
		return trace.SpanKindClient
	case scenario.SpanKindProducer:
		return trace.SpanKindProducer
	case scenario.SpanKindConsumer:
		return trace.SpanKindConsumer
	default:
		return trace.SpanKindInternal
	}
}

func toLogSeverity(level string) otellog.Severity {
	switch level {
	case "DEBUG":
		return otellog.SeverityDebug
	case "INFO":
		return otellog.SeverityInfo
	case "WARN":
		return otellog.SeverityWarn
	case "ERROR":
		return otellog.SeverityError
	default:
		return otellog.SeverityInfo
	}
}

// parseAttributes converts string map to OTel attributes with type inference.
func parseAttributes(attrs map[string]string) []attribute.KeyValue {
	result := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		// Try to parse as int
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			result = append(result, attribute.Int64(k, i))
			continue
		}
		// Try to parse as float
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			result = append(result, attribute.Float64(k, f))
			continue
		}
		// Try to parse as bool
		if b, err := strconv.ParseBool(v); err == nil {
			result = append(result, attribute.Bool(k, b))
			continue
		}
		// Default to string
		result = append(result, attribute.String(k, v))
	}

	return result
}
