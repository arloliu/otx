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

func TestHeaderCarrier_GetSetKeys(t *testing.T) {
	header := make(nats.Header)
	carrier := headerCarrier(header)

	// Test Set
	carrier.Set("traceparent", "00-abc-def-01")
	carrier.Set("tracestate", "key=value")

	// Test Get
	assert.Equal(t, "00-abc-def-01", carrier.Get("traceparent"))
	assert.Equal(t, "key=value", carrier.Get("tracestate"))
	assert.Equal(t, "", carrier.Get("nonexistent"))

	// Test Keys - NATS preserves keys lowercase via Set
	keys := carrier.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "traceparent")
	assert.Contains(t, keys, "tracestate")
}

func TestInjectNATS_NilHeader(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create a message with nil header
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("test"),
		Header:  nil, // nil header
	}

	// Create a trace context
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	// Should not panic
	InjectNATS(ctx, msg)

	// Header should now be initialized and populated
	require.NotNil(t, msg.Header)
	// Traceparent header should be set
	assert.NotEmpty(t, msg.Header.Get("traceparent"))
}

func TestExtractNATS_NilHeader(t *testing.T) {
	ctx := context.Background()

	// Should not panic with nil header
	result := ExtractNATS(ctx, nil)

	// Should return original context
	assert.Equal(t, ctx, result)
}

func TestExtractNATS_ValidHeader(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	prop := propagation.TraceContext{}
	otel.SetTextMapPropagator(prop)

	// Create a header with trace context
	header := make(nats.Header)
	header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	ctx := context.Background()
	result := ExtractNATS(ctx, header)

	// Context should have trace info - check via SpanContext
	spanCtx := oteltrace.SpanContextFromContext(result)
	assert.True(t, spanCtx.IsValid(), "SpanContext should be valid after extraction")
	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", spanCtx.TraceID().String())
}

func TestPublishAttributes(t *testing.T) {
	attrs := publishAttributes("orders.created", "msg-123", 1024)

	attrMap := make(map[string]any)
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "publish", attrMap["messaging.operation.name"])
	assert.Equal(t, "send", attrMap["messaging.operation.type"])
	assert.Equal(t, "orders.created", attrMap["messaging.destination.name"])
	assert.Equal(t, "msg-123", attrMap["messaging.message.id"])
	assert.Equal(t, int64(1024), attrMap["messaging.message.body.size"])
}

func TestReceiveAttributes(t *testing.T) {
	attrs := receiveAttributes("ORDERS", "my-consumer", 512)

	attrMap := make(map[string]any)
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "receive", attrMap["messaging.operation.name"])
	assert.Equal(t, "receive", attrMap["messaging.operation.type"])
	assert.Equal(t, "ORDERS", attrMap["nats.stream"])
	assert.Equal(t, "my-consumer", attrMap["messaging.consumer.group.name"])
}

func TestProcessAttributes(t *testing.T) {
	attrs := processAttributes("ORDERS", "my-consumer", "orders.created", "msg-456", 2048)

	attrMap := make(map[string]any)
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "process", attrMap["messaging.operation.name"])
	assert.Equal(t, "process", attrMap["messaging.operation.type"])
	assert.Equal(t, "ORDERS", attrMap["nats.stream"])
	assert.Equal(t, "orders.created", attrMap["messaging.destination.name"])
	assert.Equal(t, "my-consumer", attrMap["messaging.consumer.group.name"])
	assert.Equal(t, "msg-456", attrMap["messaging.message.id"])
	assert.Equal(t, int64(2048), attrMap["messaging.message.body.size"])
}

func TestOptions(t *testing.T) {
	// Test defaults
	opts := applyOptions(nil)
	assert.Equal(t, instrumentationName, opts.tracerName)
	assert.True(t, opts.processSpans)
	assert.True(t, opts.asyncSpans)
	assert.Nil(t, opts.prop)

	// Test custom options
	customProp := propagation.TraceContext{}
	opts = applyOptions([]Option{
		WithTracerName("custom.tracer"),
		WithProcessSpans(false),
		WithAsyncSpans(false),
		WithPropagator(customProp),
	})

	assert.Equal(t, "custom.tracer", opts.tracerName)
	assert.False(t, opts.processSpans)
	assert.False(t, opts.asyncSpans)
	assert.NotNil(t, opts.prop)
}

func TestTracedMsg_Context(t *testing.T) {
	// Test with nil context
	msg := &TracedMsg{
		Msg: nil,
		ctx: nil,
	}
	assert.NotNil(t, msg.Context())
	assert.Equal(t, context.Background(), msg.Context())

	// Test with actual context
	type ctxKey string
	ctx := context.WithValue(context.Background(), ctxKey("key"), "value")
	msg = &TracedMsg{
		Msg: nil,
		ctx: ctx,
	}
	assert.Equal(t, "value", msg.Context().Value(ctxKey("key")))
}

func TestNewTracedMsg(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create a parent span to inject trace context
	_, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	parentSpanCtx := parentSpan.SpanContext()

	// Create headers with trace context
	headers := make(nats.Header)
	propagation.TraceContext{}.Inject(
		oteltrace.ContextWithSpan(context.Background(), parentSpan),
		headerCarrier(headers),
	)
	parentSpan.End()

	// Create mock message with headers
	msg := &mockMsg{
		subject: "test.subject",
		data:    []byte("test-data"),
		headers: headers,
	}

	// Use NewTracedMsg to wrap it
	tracedMsg := NewTracedMsg(msg)

	// Verify trace context was extracted
	require.NotNil(t, tracedMsg)
	spanCtx := oteltrace.SpanContextFromContext(tracedMsg.Context())
	assert.Equal(t, parentSpanCtx.TraceID(), spanCtx.TraceID())
}

func TestNewTracedMsg_NilHeaders(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create mock message without headers
	msg := &mockMsg{
		subject: "test.subject",
		data:    []byte("test-data"),
		headers: nil,
	}

	// Use NewTracedMsg to wrap it
	tracedMsg := NewTracedMsg(msg)

	// Should still have a valid TracedMsg with background context
	require.NotNil(t, tracedMsg)
	assert.NotNil(t, tracedMsg.Context())
}

func TestNewTracedMsg_NilMsg(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Use NewTracedMsg with nil
	tracedMsg := NewTracedMsg(nil)

	// Should handle nil gracefully
	require.NotNil(t, tracedMsg)
	assert.Nil(t, tracedMsg.Msg)
	assert.NotNil(t, tracedMsg.Context())
}

func TestNewTracedMsgWithPropagator(t *testing.T) {
	customProp := &trackingPropagator{}

	// Create mock message with headers
	msg := &mockMsg{
		subject: "test.subject",
		data:    []byte("test-data"),
		headers: make(nats.Header),
	}

	tracedMsg := NewTracedMsgWithPropagator(msg, customProp)

	require.NotNil(t, tracedMsg)
	// Custom propagator should have been used
	assert.True(t, customProp.extracted)
}

func TestTracedMsg_StartProcessSpan_CreatesSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create a mock message
	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
		metadata: &jetstream.MsgMetadata{
			Consumer: "order-processor",
			Stream:   "ORDERS",
		},
	}

	tracedMsg := NewTracedMsg(msg)
	ctx, endSpan := tracedMsg.StartProcessSpan() // Stream extracted from metadata automatically
	defer endSpan(nil)

	// Context should contain the span
	assert.NotNil(t, ctx)
	spanCtx := oteltrace.SpanContextFromContext(ctx)
	assert.True(t, spanCtx.IsValid())

	// End the span so it's recorded
	endSpan(nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "process ORDERS", span.Name)
	assert.Equal(t, oteltrace.SpanKindConsumer, span.SpanKind)

	// Verify attributes
	attrMap := make(map[string]any)
	for _, attr := range span.Attributes {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "process", attrMap["messaging.operation.name"])
	assert.Equal(t, "ORDERS", attrMap["nats.stream"])
}

func TestTracedMsg_StartProcessSpan_RecordsError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
	}

	tracedMsg := NewTracedMsg(msg)
	_, endSpan := tracedMsg.StartProcessSpan(WithStream("ORDERS")) // Use WithStream when no metadata

	// End with error
	endSpan(assert.AnError)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
	require.NotEmpty(t, span.Events) // Error event recorded
}

func TestTracedMsg_StartProcessSpan_PropagatesParentTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	prop := propagation.TraceContext{}
	otel.SetTextMapPropagator(prop)

	// Create a parent span
	_, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	parentSpanCtx := parentSpan.SpanContext()

	// Inject parent trace into headers
	headers := make(nats.Header)
	prop.Inject(oteltrace.ContextWithSpan(context.Background(), parentSpan), headerCarrier(headers))
	parentSpan.End()

	// Create traced message with propagated context
	msg := &mockMsg{
		subject: "orders.created",
		data:    []byte("test-data"),
		headers: headers,
	}

	tracedMsg := NewTracedMsg(msg)
	_, endSpan := tracedMsg.StartProcessSpan(WithStream("ORDERS")) // Use WithStream when no metadata
	endSpan(nil)

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

	// Should be child of parent span
	assert.Equal(t, parentSpanCtx.TraceID(), processSpan.SpanContext.TraceID())
	assert.Equal(t, parentSpanCtx.SpanID(), processSpan.Parent.SpanID())
}

func TestTracedMsg_StartProcessSpan_CustomTracerName(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	msg := &mockMsg{
		subject: "test.subject",
		data:    []byte("data"),
	}

	tracedMsg := NewTracedMsg(msg)
	_, endSpan := tracedMsg.StartProcessSpan(WithStream("ORDERS"), WithTracerName("custom.tracer"))
	endSpan(nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "custom.tracer", spans[0].InstrumentationScope.Name)
}

// ============================================================================
// Nil-Safety Tests
// ============================================================================

func TestNewPublisher_NilJetStream_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "otx/nats: JetStream must not be nil", func() {
		NewPublisher(nil)
	})
}

func TestNewPublisherWithProviders_NilJetStream_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "otx/nats: JetStream must not be nil", func() {
		NewPublisherWithProviders(nil, nil, nil)
	})
}

func TestWrapConsumer_NilConsumer_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "otx/nats: Consumer must not be nil", func() {
		WrapConsumer(nil, "stream")
	})
}

func TestWrapConsumerWithProviders_NilConsumer_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "otx/nats: Consumer must not be nil", func() {
		WrapConsumerWithProviders(nil, "stream", nil, nil)
	})
}

func TestMessageHandlerWithTracing_NilHandler_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "otx/nats: handler must not be nil", func() {
		MessageHandlerWithTracing(nil)
	})
}

func TestMessageHandlerWithTracingProviders_NilHandler_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "otx/nats: handler must not be nil", func() {
		MessageHandlerWithTracingProviders(nil, nil, nil)
	})
}
