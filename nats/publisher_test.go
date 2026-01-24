package nats

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// testPublisher mirrors Publisher but uses a smaller interface for testing.
// This avoids implementing the full JetStream interface in mocks.
type testPublisher struct {
	mock   *mockPublishMethods
	tracer oteltrace.Tracer
	prop   propagation.TextMapPropagator
	opts   options
}

// mockPublishMethods contains mock functions for Publisher testing.
type mockPublishMethods struct {
	publishMsgFunc      func(ctx context.Context, msg *nats.Msg) (*jetstream.PubAck, error)
	publishMsgAsyncFunc func(msg *nats.Msg) (jetstream.PubAckFuture, error)
	publishAsyncFunc    func(subject string, data []byte) (jetstream.PubAckFuture, error)
}

func (p *testPublisher) publish(
	ctx context.Context,
	subject string,
	data []byte,
) (*jetstream.PubAck, error) {
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer),
		oteltrace.WithAttributes(publishAttributes(subject, "", len(data))...),
	)
	defer span.End()

	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}
	p.prop.Inject(ctx, headerCarrier(msg.Header))

	ack, err := p.mock.publishMsgFunc(ctx, msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	if ack != nil {
		span.SetAttributes(attribute.String("messaging.message.id", string(rune(ack.Sequence)+'0')))
	}

	return ack, nil
}

func (p *testPublisher) publishMsg(ctx context.Context, msg *nats.Msg) (*jetstream.PubAck, error) {
	subject := msg.Subject
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer),
		oteltrace.WithAttributes(publishAttributes(subject, "", len(msg.Data))...),
	)
	defer span.End()

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	p.prop.Inject(ctx, headerCarrier(msg.Header))

	ack, err := p.mock.publishMsgFunc(ctx, msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	return ack, nil
}

//nolint:unparam // return value is part of interface signature
func (p *testPublisher) publishAsync(subject string, data []byte) (jetstream.PubAckFuture, error) {
	if !p.opts.asyncSpans {
		return p.mock.publishAsyncFunc(subject, data)
	}

	ctx := context.Background()
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer),
		oteltrace.WithAttributes(publishAttributes(subject, "", len(data))...),
	)
	defer span.End()

	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}
	p.prop.Inject(ctx, headerCarrier(msg.Header))

	future, err := p.mock.publishMsgAsyncFunc(msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	return future, nil
}

//nolint:unparam // return value is part of interface signature
func (p *testPublisher) publishAsyncMsg(msg *nats.Msg) (jetstream.PubAckFuture, error) {
	if !p.opts.asyncSpans {
		return p.mock.publishMsgAsyncFunc(msg)
	}

	ctx := context.Background()
	subject := msg.Subject
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer),
		oteltrace.WithAttributes(publishAttributes(subject, "", len(msg.Data))...),
	)
	defer span.End()

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	p.prop.Inject(ctx, headerCarrier(msg.Header))

	future, err := p.mock.publishMsgAsyncFunc(msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	return future, nil
}

// setupTestPublisher creates a testPublisher with an in-memory exporter for testing.
func setupTestPublisher(
	t *testing.T,
	mock *mockPublishMethods,
	opts ...Option,
) (*testPublisher, *tracetest.InMemoryExporter, *trace.TracerProvider) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	o := applyOptions(opts)

	pub := &testPublisher{
		mock:   mock,
		tracer: getTracer(tp, o),
		prop:   getPropagator(o),
		opts:   o,
	}

	return pub, exporter, tp
}

func TestPublisher_Publish_CreatesSpan(t *testing.T) {
	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return &jetstream.PubAck{Sequence: 42}, nil
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock)

	ack, err := pub.publish(context.Background(), "orders.created", []byte("test-payload"))
	require.NoError(t, err)
	require.NotNil(t, ack)
	assert.Equal(t, uint64(42), ack.Sequence)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "publish orders.created", span.Name)
	assert.Equal(t, oteltrace.SpanKindProducer, span.SpanKind)

	// Verify attributes
	attrMap := spanAttrMap(span)
	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "publish", attrMap["messaging.operation.name"])
	assert.Equal(t, "send", attrMap["messaging.operation.type"])
	assert.Equal(t, "orders.created", attrMap["messaging.destination.name"])
}

func TestPublisher_Publish_InjectsTraceContext(t *testing.T) {
	var capturedMsg *nats.Msg

	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, msg *nats.Msg) (*jetstream.PubAck, error) {
			capturedMsg = msg

			return &jetstream.PubAck{Sequence: 1}, nil
		},
	}

	pub, _, _ := setupTestPublisher(t, mock)

	_, err := pub.publish(context.Background(), "test.subject", []byte("data"))
	require.NoError(t, err)

	// Trace context should be injected into headers
	require.NotNil(t, capturedMsg)
	require.NotNil(t, capturedMsg.Header)
	assert.NotEmpty(t, capturedMsg.Header.Get("traceparent"))
}

func TestPublisher_Publish_RecordsError(t *testing.T) {
	expectedErr := errors.New("publish failed")

	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return nil, expectedErr
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock)

	_, err := pub.publish(context.Background(), "test.subject", []byte("data"))
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Contains(t, span.Status.Description, "publish failed")

	// Should have recorded the error event
	require.NotEmpty(t, span.Events)
}

func TestPublisher_PublishMsg_InitializesNilHeader(t *testing.T) {
	var capturedMsg *nats.Msg

	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, msg *nats.Msg) (*jetstream.PubAck, error) {
			capturedMsg = msg

			return &jetstream.PubAck{Sequence: 1}, nil
		},
	}

	pub, _, _ := setupTestPublisher(t, mock)

	// Create message with nil header
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("data"),
		Header:  nil,
	}

	_, err := pub.publishMsg(context.Background(), msg)
	require.NoError(t, err)

	// Header should be initialized and have trace context
	require.NotNil(t, capturedMsg.Header)
	assert.NotEmpty(t, capturedMsg.Header.Get("traceparent"))
}

func TestPublisher_PublishAsync_WithAsyncSpansEnabled(t *testing.T) {
	var capturedMsg *nats.Msg

	mock := &mockPublishMethods{
		publishMsgAsyncFunc: func(msg *nats.Msg) (jetstream.PubAckFuture, error) {
			capturedMsg = msg

			return nil, nil //nolint:nilnil // mock returning nil future is intentional for test
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock, WithAsyncSpans(true))

	_, err := pub.publishAsync("async.subject", []byte("async-data"))
	require.NoError(t, err)

	// Should create a span
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "publish async.subject", spans[0].Name)

	// Should inject trace context
	require.NotNil(t, capturedMsg)
	require.NotNil(t, capturedMsg.Header)
	assert.NotEmpty(t, capturedMsg.Header.Get("traceparent"))
}

func TestPublisher_PublishAsync_WithAsyncSpansDisabled(t *testing.T) {
	publishAsyncCalled := false

	mock := &mockPublishMethods{
		publishAsyncFunc: func(_ string, _ []byte) (jetstream.PubAckFuture, error) {
			publishAsyncCalled = true

			return nil, nil //nolint:nilnil // mock returning nil future is intentional for test
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock, WithAsyncSpans(false))

	_, err := pub.publishAsync("async.subject", []byte("data"))
	require.NoError(t, err)

	// Should not create a span
	spans := exporter.GetSpans()
	assert.Len(t, spans, 0)

	// Should call the original PublishAsync
	assert.True(t, publishAsyncCalled)
}

func TestPublisher_PublishAsyncMsg_InitializesNilHeader(t *testing.T) {
	var capturedMsg *nats.Msg

	mock := &mockPublishMethods{
		publishMsgAsyncFunc: func(msg *nats.Msg) (jetstream.PubAckFuture, error) {
			capturedMsg = msg

			return nil, nil //nolint:nilnil // mock returning nil future is intentional for test
		},
	}

	pub, _, _ := setupTestPublisher(t, mock, WithAsyncSpans(true))

	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("data"),
		Header:  nil,
	}

	_, err := pub.publishAsyncMsg(msg)
	require.NoError(t, err)

	// Header should be initialized
	require.NotNil(t, capturedMsg.Header)
	assert.NotEmpty(t, capturedMsg.Header.Get("traceparent"))
}

func TestPublisher_WithCustomTracerName(t *testing.T) {
	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return &jetstream.PubAck{Sequence: 1}, nil
		},
	}
	pub, exporter, _ := setupTestPublisher(t, mock, WithTracerName("custom.tracer.name"))

	_, err := pub.publish(context.Background(), "test.subject", []byte("data"))
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	// The tracer name is in the InstrumentationScope
	assert.Equal(t, "custom.tracer.name", spans[0].InstrumentationScope.Name)
}

// spanAttrMap converts span attributes to a map for easier assertion.
func spanAttrMap(span tracetest.SpanStub) map[string]any {
	result := make(map[string]any)
	for _, attr := range span.Attributes {
		result[string(attr.Key)] = attr.Value.AsInterface()
	}

	return result
}

func TestPublisher_Publish_PropagatesParentContext(t *testing.T) {
	var capturedMsg *nats.Msg

	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, msg *nats.Msg) (*jetstream.PubAck, error) {
			capturedMsg = msg

			return &jetstream.PubAck{Sequence: 1}, nil
		},
	}

	pub, exporter, tp := setupTestPublisher(t, mock)

	// Start a parent span
	ctx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent-operation")
	parentSpanCtx := parentSpan.SpanContext()

	_, err := pub.publish(ctx, "test.subject", []byte("data"))
	require.NoError(t, err)
	parentSpan.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)

	// Find the publish span
	var publishSpan tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "publish test.subject" {
			publishSpan = s

			break
		}
	}

	// Publish span should be child of parent span
	assert.Equal(t, parentSpanCtx.TraceID(), publishSpan.SpanContext.TraceID())
	assert.Equal(t, parentSpanCtx.SpanID(), publishSpan.Parent.SpanID())

	// Verify injected header contains parent trace context
	require.NotNil(t, capturedMsg)

	traceparent := capturedMsg.Header.Get("traceparent")
	assert.Contains(t, traceparent, parentSpanCtx.TraceID().String())
}

func TestPublisher_Publish_SetsMessageBodySize(t *testing.T) {
	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return &jetstream.PubAck{Sequence: 1}, nil
		},
	}
	pub, exporter, _ := setupTestPublisher(t, mock)

	payload := []byte("this is a test payload with specific size")
	_, err := pub.publish(context.Background(), "test.subject", payload)
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	attrMap := spanAttrMap(spans[0])
	assert.Equal(t, int64(len(payload)), attrMap["messaging.message.body.size"])
}

func TestPublisher_PublishAsyncMsg_WithAsyncSpansDisabled(t *testing.T) {
	publishAsyncCalled := false

	mock := &mockPublishMethods{
		publishMsgAsyncFunc: func(_ *nats.Msg) (jetstream.PubAckFuture, error) {
			publishAsyncCalled = true

			return nil, nil //nolint:nilnil // mock returning nil future is intentional for test
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock, WithAsyncSpans(false))

	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("data"),
	}

	_, err := pub.publishAsyncMsg(msg)
	require.NoError(t, err)

	// Should not create a span
	spans := exporter.GetSpans()
	assert.Len(t, spans, 0)

	// Should call the underlying method
	assert.True(t, publishAsyncCalled)
}

func TestPublisher_PublishAsync_RecordsError(t *testing.T) {
	expectedErr := errors.New("async publish failed")

	mock := &mockPublishMethods{
		publishMsgAsyncFunc: func(_ *nats.Msg) (jetstream.PubAckFuture, error) {
			return nil, expectedErr
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock, WithAsyncSpans(true))

	_, err := pub.publishAsync("test.subject", []byte("data"))
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Contains(t, span.Status.Description, "async publish failed")
}

func TestPublisher_PublishAsyncMsg_RecordsError(t *testing.T) {
	expectedErr := errors.New("async publish msg failed")

	mock := &mockPublishMethods{
		publishMsgAsyncFunc: func(_ *nats.Msg) (jetstream.PubAckFuture, error) {
			return nil, expectedErr
		},
	}

	pub, exporter, _ := setupTestPublisher(t, mock, WithAsyncSpans(true))

	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("data"),
	}

	_, err := pub.publishAsyncMsg(msg)
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
}

// trackingPropagator is a propagator that tracks if it was used.
type trackingPropagator struct {
	injected  bool
	extracted bool
}

func (p *trackingPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	p.injected = true
	propagation.TraceContext{}.Inject(ctx, carrier)
}

func (p *trackingPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	p.extracted = true

	return propagation.TraceContext{}.Extract(ctx, carrier)
}

func (*trackingPropagator) Fields() []string {
	return propagation.TraceContext{}.Fields()
}

func TestPublisher_WithCustomPropagator(t *testing.T) {
	customProp := &trackingPropagator{}

	mock := &mockPublishMethods{
		publishMsgFunc: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return &jetstream.PubAck{Sequence: 1}, nil
		},
	}

	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	o := applyOptions([]Option{WithPropagator(customProp)})

	pub := &testPublisher{
		mock:   mock,
		tracer: getTracer(tp, o),
		prop:   getPropagator(o),
		opts:   o,
	}

	_, err := pub.publish(context.Background(), "test.subject", []byte("data"))
	require.NoError(t, err)

	// Custom propagator should have been used
	assert.True(t, customProp.injected)
}
