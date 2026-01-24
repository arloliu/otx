package nats

import (
	"context"
	"errors"
	"testing"
	"time"

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

// mockMsg implements jetstream.Msg for testing.
type mockMsg struct {
	subject  string
	data     []byte
	headers  nats.Header
	metadata *jetstream.MsgMetadata
}

func (m *mockMsg) Subject() string                           { return m.subject }
func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Headers() nats.Header                      { return m.headers }
func (*mockMsg) Reply() string                               { return "" }
func (*mockMsg) Ack() error                                  { return nil }
func (*mockMsg) DoubleAck(_ context.Context) error           { return nil }
func (*mockMsg) Nak() error                                  { return nil }
func (*mockMsg) NakWithDelay(_ time.Duration) error          { return nil }
func (*mockMsg) Term() error                                 { return nil }
func (*mockMsg) TermWithReason(_ string) error               { return nil }
func (*mockMsg) InProgress() error                           { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return m.metadata, nil }

// testConsumer is a test-specific implementation mirroring TracedConsumer behavior.
type testConsumer struct {
	consumerInfo *jetstream.ConsumerInfo
	stream       string
	tracer       oteltrace.Tracer
	prop         propagation.TextMapPropagator
	opts         options

	// Mock functions
	fetchFunc func(batch int) ([]jetstream.Msg, error)
	nextFunc  func() (jetstream.Msg, error)
}

func (tc *testConsumer) extractContext(ctx context.Context, msg jetstream.Msg) context.Context {
	if msg == nil {
		return ctx
	}

	headers := msg.Headers()
	if headers == nil {
		return ctx
	}

	return tc.prop.Extract(ctx, headerCarrier(headers))
}

func (tc *testConsumer) fetch(_ int) (*testMessageBatch, error) {
	consumerName := ""
	if tc.consumerInfo != nil {
		consumerName = tc.consumerInfo.Name
	}

	spanName := opTypeReceive + " " + tc.stream

	ctx, span := tc.tracer.Start(context.Background(), spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(receiveAttributes(tc.stream, consumerName, 0)...),
	)

	msgs, err := tc.fetchFunc(0) // ignoring batch in tests
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return &testMessageBatch{
		msgs:       msgs,
		ctx:        ctx,
		extractCtx: tc.extractContext,
	}, nil
}

func (tc *testConsumer) next() (*TracedMsg, error) {
	consumerName := ""
	if tc.consumerInfo != nil {
		consumerName = tc.consumerInfo.Name
	}

	spanName := opTypeReceive + " " + tc.stream

	_, span := tc.tracer.Start(context.Background(), spanName,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(receiveAttributes(tc.stream, consumerName, 0)...),
	)

	msg, err := tc.nextFunc()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return &TracedMsg{
		Msg: msg,
		ctx: tc.extractContext(context.Background(), msg),
	}, nil
}

// testMessageBatch is a test helper for message batch iteration.
type testMessageBatch struct {
	msgs       []jetstream.Msg
	ctx        context.Context
	extractCtx func(context.Context, jetstream.Msg) context.Context
}

func (b *testMessageBatch) Messages() <-chan *TracedMsg {
	ch := make(chan *TracedMsg)
	go func() {
		defer close(ch)
		for _, msg := range b.msgs {
			ch <- &TracedMsg{
				Msg: msg,
				ctx: b.extractCtx(b.ctx, msg),
			}
		}
	}()

	return ch
}

// setupTestConsumer creates a testConsumer for testing.
//
//nolint:unparam // stream is part of test interface
func setupTestConsumer(
	t *testing.T,
	stream string,
	opts ...Option,
) (*testConsumer, *tracetest.InMemoryExporter, *trace.TracerProvider) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	o := applyOptions(opts)

	tc := &testConsumer{
		stream: stream,
		tracer: getTracer(tp, o),
		prop:   getPropagator(o),
		opts:   o,
	}

	return tc, exporter, tp
}

func TestTracedConsumer_Fetch_CreatesSpan(t *testing.T) {
	tc, exporter, _ := setupTestConsumer(t, "ORDERS")

	tc.fetchFunc = func(_ int) ([]jetstream.Msg, error) {
		return []jetstream.Msg{
			&mockMsg{subject: "orders.created", data: []byte("order1")},
			&mockMsg{subject: "orders.created", data: []byte("order2")},
		}, nil
	}

	batch, err := tc.fetch(10)
	require.NoError(t, err)
	require.NotNil(t, batch)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "receive ORDERS", span.Name)
	assert.Equal(t, oteltrace.SpanKindClient, span.SpanKind)

	attrMap := spanAttrMap(span)
	assert.Equal(t, "nats", attrMap["messaging.system"])
	assert.Equal(t, "receive", attrMap["messaging.operation.name"])
	assert.Equal(t, "ORDERS", attrMap["nats.stream"])
}

func TestTracedConsumer_Fetch_RecordsError(t *testing.T) {
	tc, exporter, _ := setupTestConsumer(t, "ORDERS")

	expectedErr := errors.New("fetch failed")
	tc.fetchFunc = func(_ int) ([]jetstream.Msg, error) {
		return nil, expectedErr
	}

	batch, err := tc.fetch(10)
	require.Error(t, err)
	require.Nil(t, batch)
	assert.Equal(t, expectedErr, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Contains(t, span.Status.Description, "fetch failed")
}

func TestTracedConsumer_Fetch_ExtractsTraceContext(t *testing.T) {
	tc, _, tp := setupTestConsumer(t, "ORDERS")

	// Create a parent span to inject trace context
	_, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	parentSpanCtx := parentSpan.SpanContext()

	// Create headers with trace context
	headers := make(nats.Header)
	prop := propagation.TraceContext{}
	carrier := headerCarrier(headers)
	prop.Inject(oteltrace.ContextWithSpan(context.Background(), parentSpan), carrier)
	parentSpan.End()

	tc.fetchFunc = func(_ int) ([]jetstream.Msg, error) {
		return []jetstream.Msg{
			&mockMsg{
				subject: "orders.created",
				data:    []byte("order1"),
				headers: headers,
			},
		}, nil
	}

	batch, err := tc.fetch(10)
	require.NoError(t, err)

	// Get the message and verify trace context was extracted
	for msg := range batch.Messages() {
		spanCtx := oteltrace.SpanContextFromContext(msg.Context())
		assert.Equal(t, parentSpanCtx.TraceID(), spanCtx.TraceID())
	}
}

func TestTracedConsumer_Next_CreatesSpan(t *testing.T) {
	tc, exporter, _ := setupTestConsumer(t, "ORDERS")

	tc.nextFunc = func() (jetstream.Msg, error) {
		return &mockMsg{subject: "orders.created", data: []byte("order")}, nil
	}

	msg, err := tc.next()
	require.NoError(t, err)
	require.NotNil(t, msg)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "receive ORDERS", span.Name)
	assert.Equal(t, oteltrace.SpanKindClient, span.SpanKind)
}

func TestTracedConsumer_Next_RecordsError(t *testing.T) {
	tc, exporter, _ := setupTestConsumer(t, "ORDERS")

	expectedErr := errors.New("no messages")
	tc.nextFunc = func() (jetstream.Msg, error) {
		return nil, expectedErr
	}

	msg, err := tc.next()
	require.Error(t, err)
	require.Nil(t, msg)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status.Code)
}

func TestTracedConsumer_Next_ExtractsTraceContext(t *testing.T) {
	tc, _, tp := setupTestConsumer(t, "ORDERS")

	// Create trace context
	_, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	parentSpanCtx := parentSpan.SpanContext()

	headers := make(nats.Header)
	prop := propagation.TraceContext{}
	prop.Inject(oteltrace.ContextWithSpan(context.Background(), parentSpan), headerCarrier(headers))
	parentSpan.End()

	tc.nextFunc = func() (jetstream.Msg, error) {
		return &mockMsg{
			subject: "orders.created",
			data:    []byte("order"),
			headers: headers,
		}, nil
	}

	msg, err := tc.next()
	require.NoError(t, err)

	spanCtx := oteltrace.SpanContextFromContext(msg.Context())
	assert.Equal(t, parentSpanCtx.TraceID(), spanCtx.TraceID())
}

func TestTracedConsumer_BatchMessages_IteratesAllMessages(t *testing.T) {
	tc, _, _ := setupTestConsumer(t, "ORDERS")

	tc.fetchFunc = func(_ int) ([]jetstream.Msg, error) {
		return []jetstream.Msg{
			&mockMsg{subject: "orders.created", data: []byte("order1")},
			&mockMsg{subject: "orders.created", data: []byte("order2")},
			&mockMsg{subject: "orders.created", data: []byte("order3")},
		}, nil
	}

	batch, err := tc.fetch(10)
	require.NoError(t, err)

	messages := make([]*TracedMsg, 0, 3)
	for msg := range batch.Messages() {
		messages = append(messages, msg)
	}

	assert.Len(t, messages, 3)
}

func TestTracedConsumer_ExtractContext_NilMsg(t *testing.T) {
	tc, _, _ := setupTestConsumer(t, "ORDERS")

	ctx := context.Background()
	result := tc.extractContext(ctx, nil)

	// Should return original context
	assert.Equal(t, ctx, result)
}

func TestTracedConsumer_ExtractContext_NilHeaders(t *testing.T) {
	tc, _, _ := setupTestConsumer(t, "ORDERS")

	ctx := context.Background()
	msg := &mockMsg{headers: nil}
	result := tc.extractContext(ctx, msg)

	// Should return original context
	assert.Equal(t, ctx, result)
}
