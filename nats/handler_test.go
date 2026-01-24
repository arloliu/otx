package nats

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func setupHandlerTest(t *testing.T) (*tracetest.InMemoryExporter, *trace.TracerProvider) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return exporter, tp
}

func TestMessageHandlerWithTracing_CreatesProcessSpan(t *testing.T) {
	exporter, _ := setupHandlerTest(t)

	var receivedMsg *TracedMsg

	// Stream auto-detected from metadata
	handler := MessageHandlerWithTracing(func(msg *TracedMsg) {
		receivedMsg = msg
	})

	// Create a mock message with metadata
	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
		metadata: &jetstream.MsgMetadata{
			Consumer: "test-consumer",
			Stream:   "ORDERS",
		},
	}

	handler(msg)

	require.NotNil(t, receivedMsg)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "process ORDERS", span.Name)
	assert.Equal(t, oteltrace.SpanKindConsumer, span.SpanKind)

	attrMap := spanAttrMap(span)
	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "process", attrMap["messaging.operation.name"])
	assert.Equal(t, "process", attrMap["messaging.operation.type"])
	assert.Equal(t, "ORDERS", attrMap["nats.stream"])
}

func TestMessageHandlerWithTracing_WithStreamOption(t *testing.T) {
	exporter, _ := setupHandlerTest(t)

	// Use WithStream to override/provide stream name
	handler := MessageHandlerWithTracing(func(_ *TracedMsg) {
	}, WithStream("CUSTOM-STREAM"))

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
	}

	handler(msg)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "process CUSTOM-STREAM", spans[0].Name)
}

func TestMessageHandlerWithTracing_ExtractsTraceContext(t *testing.T) {
	exporter, tp := setupHandlerTest(t)

	// Create a parent span
	ctx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent-operation")
	parentSpanCtx := parentSpan.SpanContext()

	// Create headers with trace context
	headers := make(nats.Header)
	propagation.TraceContext{}.Inject(ctx, headerCarrier(headers))
	parentSpan.End()

	var receivedCtx context.Context

	handler := MessageHandlerWithTracing(func(msg *TracedMsg) {
		receivedCtx = msg.Context() //nolint:fatcontext // intentionally capturing context for test verification
	}, WithStream("ORDERS"))

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
		headers: headers,
		metadata: &jetstream.MsgMetadata{
			Stream: "ORDERS",
		},
	}

	handler(msg)

	// The received context should have trace info from parent
	spanCtx := oteltrace.SpanContextFromContext(receivedCtx)
	assert.Equal(t, parentSpanCtx.TraceID(), spanCtx.TraceID())

	// Verify spans
	spans := exporter.GetSpans()
	require.Len(t, spans, 2) // parent + process

	// Find the process span
	var processSpan tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "process ORDERS" {
			processSpan = s

			break
		}
	}

	// Process span should be a child of parent span
	assert.Equal(t, parentSpanCtx.TraceID(), processSpan.SpanContext.TraceID())
	assert.Equal(t, parentSpanCtx.SpanID(), processSpan.Parent.SpanID())
}

func TestMessageHandlerWithTracing_HandlesPanic(t *testing.T) {
	exporter, _ := setupHandlerTest(t)

	handler := MessageHandlerWithTracing(func(_ *TracedMsg) {
		panic("handler panic")
	}, WithStream("ORDERS"))

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
	}

	// Should re-panic after recording error
	assert.Panics(t, func() {
		handler(msg)
	})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Contains(t, span.Status.Description, "panic")

	// Should have recorded error event
	require.NotEmpty(t, span.Events)
}

func TestMessageHandlerWithTracing_NilHeaders(t *testing.T) {
	exporter, _ := setupHandlerTest(t)

	var receivedMsg *TracedMsg

	handler := MessageHandlerWithTracing(func(msg *TracedMsg) {
		receivedMsg = msg
	}, WithStream("ORDERS"))

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
		headers: nil, // No headers
	}

	handler(msg)

	require.NotNil(t, receivedMsg)

	// Should still have a valid context (just without extracted trace)
	assert.NotNil(t, receivedMsg.Context())

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
}

func TestMessageHandlerWithTracing_CustomTracerName(t *testing.T) {
	exporter, _ := setupHandlerTest(t)

	handler := MessageHandlerWithTracing(func(_ *TracedMsg) {
	}, WithStream("ORDERS"), WithTracerName("custom.handler.tracer"))

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
	}

	handler(msg)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "custom.handler.tracer", spans[0].InstrumentationScope.Name)
}

func TestMessageHandlerWithTracingProviders_ExplicitProviders(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	customProp := &trackingPropagator{}

	handler := MessageHandlerWithTracingProviders(func(_ *TracedMsg) {
	}, tp, customProp, WithStream("ORDERS"))

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
		headers: make(nats.Header),
	}

	handler(msg)

	// Should have used the custom propagator
	assert.True(t, customProp.extracted)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
}
