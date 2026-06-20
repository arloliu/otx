// Package otx (OpenTelemetry eXtensions) provides a config-driven OpenTelemetry
// tracing layer for Go services.
//
// # Overview
//
// The otx package wraps official OTel APIs, providing:
//   - Config-driven sampling (OTel standard: always_on, always_off, traceidratio, parentbased_*)
//   - Pluggable span naming via [SpanNamer] interface
//   - W3C TraceContext and Baggage propagation (OTEL_PROPAGATORS)
//   - Context-aware span helpers for tracing operations
//
// # Quick Start
//
// Initialize the tracer provider and global tracer. Use [BoolPtr] to set the
// *bool Enabled fields when building a config programmatically:
//
//	cfg := &otx.TelemetryConfig{
//	    Enabled:     otx.BoolPtr(true),
//	    ServiceName: "my-service",
//	}
//	tp, err := otx.NewTracerProvider(ctx, cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer tp.Shutdown(ctx)
//	otx.InitTracing(tp.Tracer("my-service"), otx.DefaultNamer{})
//
// Use span helpers in your code:
//
//	func ProcessBatch(ctx context.Context, batch []Item) error {
//	    ctx, span := otx.Start(ctx, "ProcessBatch")
//	    defer span.End()
//
//	    otx.SetAttributes(ctx, attribute.Int("batch.size", len(batch)))
//
//	    if err := process(ctx, batch); err != nil {
//	        otx.RecordError(ctx, err)
//	        return err
//	    }
//
//	    otx.SetSuccess(ctx)
//	    return nil
//	}
//
// # Configuration
//
// Configure via YAML or environment variables (OTel standard):
//
//	otx:
//	  enabled: true
//	  serviceName: "my-service"  # OTEL_SERVICE_NAME
//	  otlp:
//	    endpoint: "otel-collector:4318"  # OTEL_EXPORTER_OTLP_ENDPOINT
//	  traces:
//	    sampling:
//	      sampler: "parentbased_traceidratio"  # OTEL_TRACES_SAMPLER
//	      samplerArg: 0.1                       # OTEL_TRACES_SAMPLER_ARG
//	  propagation:
//	    propagators: "tracecontext,baggage"  # OTEL_PROPAGATORS
//
// # Span Naming
//
// The [SpanNamer] interface controls how operation names become span names.
// [DefaultNamer] passes the operation name through unchanged.
// For structured, signal-specific names use helpers such as [NameHTTP], [NameRPC],
// [NameMessaging], and [NameDB].
//
// # Baggage
//
// Use baggage helpers to propagate metadata across services:
//
//	ctx = otx.MustSetBaggage(ctx, "tenant.id", tenantID)
//	// ... later in downstream service
//	tenantID := otx.GetBaggage(ctx, "tenant.id")
//
// # Middleware
//
// The otx/middleware sub-package provides gRPC and HTTP instrumentation.
// See that package for details.
package otx
