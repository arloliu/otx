package nats

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// setupRecorder installs an in-memory tracer provider and W3C propagator for a
// test and returns the exporter and provider for assertions.
func setupRecorder(t *testing.T) (*tracetest.InMemoryExporter, *trace.TracerProvider) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	return exporter, tp
}

// ============================================================================
// M5 — WithProcessSpans must be honored
// ============================================================================

func TestMessageHandlerWithTracing_ProcessSpansDisabled_SuppressesSpan(t *testing.T) {
	exporter, _ := setupRecorder(t)

	called := false
	handler := MessageHandlerWithTracing(func(msg *TracedMsg) {
		called = true
		// The context handed to the handler must not carry a process span.
		assert.False(t, oteltrace.SpanContextFromContext(msg.Context()).IsValid())
	}, WithProcessSpans(false))

	handler(&mockMsg{subject: "orders.created", data: []byte("data")})

	require.True(t, called)
	assert.Empty(t, exporter.GetSpans(), "no process span should be created when disabled")
}

func TestMessageHandlerWithTracing_ProcessSpansDefault_CreatesSpan(t *testing.T) {
	exporter, _ := setupRecorder(t)

	handler := MessageHandlerWithTracing(func(_ *TracedMsg) {})
	handler(&mockMsg{subject: "orders.created", data: []byte("data")})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "process ", spans[0].Name)
}

func TestStartProcessSpan_ProcessSpansDisabled_SuppressesSpan(t *testing.T) {
	exporter, _ := setupRecorder(t)

	msg := &mockMsg{subject: "orders.created", data: []byte("data")}
	tracedMsg := NewTracedMsg(msg)

	ctx, endSpan := tracedMsg.StartProcessSpan(WithStream("ORDERS"), WithProcessSpans(false))
	endSpan(assert.AnError) // even with an error, no span exists to record on

	assert.Equal(t, tracedMsg.Context(), ctx, "ctx should be unchanged when span suppressed")
	assert.Empty(t, exporter.GetSpans(), "no process span should be created when disabled")
}

func TestStartProcessSpan_ProcessSpansDefault_CreatesSpan(t *testing.T) {
	exporter, _ := setupRecorder(t)

	msg := &mockMsg{subject: "orders.created", data: []byte("data")}
	tracedMsg := NewTracedMsg(msg)

	_, endSpan := tracedMsg.StartProcessSpan(WithStream("ORDERS"))
	endSpan(nil)

	require.Len(t, exporter.GetSpans(), 1)
}

// ============================================================================
// mock consumer / batch for exercising the real TracedConsumer code paths
// ============================================================================

// mockConsumer implements jetstream.Consumer for tests.
type mockConsumer struct {
	info       *jetstream.ConsumerInfo
	fetchBatch jetstream.MessageBatch
	fetchErr   error
	nextMsg    jetstream.Msg
	nextErr    error
}

func (m *mockConsumer) Fetch(_ int, _ ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
	return m.fetchBatch, m.fetchErr
}

func (m *mockConsumer) FetchBytes(_ int, _ ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
	return m.fetchBatch, m.fetchErr
}

func (m *mockConsumer) FetchNoWait(_ int) (jetstream.MessageBatch, error) {
	return m.fetchBatch, m.fetchErr
}

func (*mockConsumer) Consume(
	_ jetstream.MessageHandler,
	_ ...jetstream.PullConsumeOpt,
) (jetstream.ConsumeContext, error) {
	return nil, nil //nolint:nilnil // unused in tests
}

func (*mockConsumer) Messages(_ ...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error) {
	return nil, nil //nolint:nilnil // unused in tests
}

func (m *mockConsumer) Next(_ ...jetstream.FetchOpt) (jetstream.Msg, error) {
	return m.nextMsg, m.nextErr
}

func (m *mockConsumer) Info(_ context.Context) (*jetstream.ConsumerInfo, error) {
	return m.info, nil
}

func (m *mockConsumer) CachedInfo() *jetstream.ConsumerInfo { return m.info }

// mockBatch implements jetstream.MessageBatch backed by a caller-controlled channel.
type mockBatch struct {
	ch  chan jetstream.Msg
	err error
}

func (b *mockBatch) Messages() <-chan jetstream.Msg { return b.ch }
func (b *mockBatch) Error() error                   { return b.err }

// ============================================================================
// M4 — goroutine leak on early consumer exit, fixed via Stop()
// ============================================================================

func TestTracedMessageBatch_Stop_TerminatesForwarder(t *testing.T) {
	_, _ = setupRecorder(t)

	upstream := make(chan jetstream.Msg) // unbuffered, never closed by us
	batch := &mockBatch{ch: upstream}

	tc := WrapConsumer(&mockConsumer{fetchBatch: batch}, "ORDERS")

	tb, err := tc.Fetch(10)
	require.NoError(t, err)

	msgs := tb.Messages()

	// Deliver one message and consume it so the forwarder loops back.
	upstream <- &mockMsg{subject: "orders.created", data: []byte("x")}
	<-msgs

	// Deliver a second message SYNCHRONOUSLY. This send returns only once the
	// forwarder has received it, which proves the forwarder has left the outer
	// select and is committed to the downstream b.msgChan send. We deliberately
	// never read msgs again, so the forwarder is now parked on that unread send
	// — the exact leak scenario M4 fixes.
	upstream <- &mockMsg{subject: "orders.created", data: []byte("y")}
	parked := runtime.NumGoroutine()

	// The caller abandons the channel (no further reads) and calls Stop(). Only
	// the send-side cancel arm in forward() can release the parked send and let
	// the goroutine exit. A regression that drops that arm (relying on the outer
	// select, which is never re-entered once parked on the send) leaks the
	// forwarder forever — caught here as a goroutine that never exits.
	//
	// We assert on goroutine termination rather than draining the channel,
	// because any read would unblock the send and let even the buggy version
	// recover via the outer select, masking the leak.
	tb.Stop()

	// Poll in THIS goroutine (testify's Eventually would add its own goroutine,
	// masking the forwarder's exit in the count). The forwarder's exit drops the
	// count below the parked baseline; a leak keeps it at or above it.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() >= parked {
		if time.Now().After(deadline) {
			t.Fatal("forwarder goroutine did not exit after Stop(); the send-side cancel arm is missing")
		}
		runtime.Gosched()
	}
}

func TestTracedMessageBatch_StopBeforeMessages(t *testing.T) {
	_, _ = setupRecorder(t)

	upstream := make(chan jetstream.Msg) // never delivers, never closed
	batch := &mockBatch{ch: upstream}

	tc := WrapConsumer(&mockConsumer{fetchBatch: batch}, "ORDERS")
	tb, err := tc.Fetch(10)
	require.NoError(t, err)

	// Stop() is documented safe even when Messages() was never called.
	tb.Stop()

	// A subsequent Messages() must hand back a channel that closes promptly,
	// because the forwarder sees the already-cancelled context immediately.
	select {
	case _, ok := <-tb.Messages():
		assert.False(t, ok, "channel should be closed after Stop() before Messages()")
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after Stop() called before Messages()")
	}
}

func TestTracedMessageBatch_StopIdempotent(t *testing.T) {
	_, _ = setupRecorder(t)

	upstream := make(chan jetstream.Msg)
	close(upstream)
	batch := &mockBatch{ch: upstream}

	tc := WrapConsumer(&mockConsumer{fetchBatch: batch}, "ORDERS")
	tb, err := tc.Fetch(10)
	require.NoError(t, err)

	// Stop is documented safe to call multiple times.
	assert.NotPanics(t, func() {
		tb.Stop()
		tb.Stop()
	})
}

// ============================================================================
// L-N1 — ctx-accepting fetch variants nest under the passed context
// ============================================================================

func TestFetchContext_NestsUnderPassedContext(t *testing.T) {
	exporter, tp := setupRecorder(t)

	upstream := make(chan jetstream.Msg)
	close(upstream) // empty, immediately closed batch
	batch := &mockBatch{ch: upstream}

	tc := WrapConsumer(&mockConsumer{fetchBatch: batch, info: &jetstream.ConsumerInfo{Name: "c"}}, "ORDERS")

	parentCtx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	parentSC := parentSpan.SpanContext()

	tb, err := tc.FetchContext(parentCtx, 10)
	require.NoError(t, err)
	require.NotNil(t, tb)
	parentSpan.End()

	// Drain to let the forwarder finish.
	for range tb.Messages() { //nolint:revive // intentional drain
	}

	receive := findSpan(t, exporter, "receive ORDERS")
	assert.Equal(t, parentSC.TraceID(), receive.SpanContext.TraceID())
	assert.Equal(t, parentSC.SpanID(), receive.Parent.SpanID())
}

func TestNextContext_NestsUnderPassedContext(t *testing.T) {
	exporter, tp := setupRecorder(t)

	tc := WrapConsumer(&mockConsumer{nextMsg: &mockMsg{subject: "orders.created", data: []byte("x")}}, "ORDERS")

	parentCtx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	parentSC := parentSpan.SpanContext()

	_, err := tc.NextContext(parentCtx)
	require.NoError(t, err)
	parentSpan.End()

	receive := findSpan(t, exporter, "receive ORDERS")
	assert.Equal(t, parentSC.TraceID(), receive.SpanContext.TraceID())
	assert.Equal(t, parentSC.SpanID(), receive.Parent.SpanID())
}

// ============================================================================
// L-N2 — header-less Next() message descends from the receive span
// ============================================================================

func TestNext_HeaderlessMessage_DescendsFromReceiveSpan(t *testing.T) {
	exporter, _ := setupRecorder(t)

	tc := WrapConsumer(&mockConsumer{nextMsg: &mockMsg{subject: "orders.created", data: []byte("x")}}, "ORDERS")

	tracedMsg, err := tc.Next()
	require.NoError(t, err)

	receive := findSpan(t, exporter, "receive ORDERS")

	msgSC := oteltrace.SpanContextFromContext(tracedMsg.Context())
	require.True(t, msgSC.IsValid(), "header-less message ctx should carry the receive span")
	assert.Equal(t, receive.SpanContext.TraceID(), msgSC.TraceID())
	assert.Equal(t, receive.SpanContext.SpanID(), msgSC.SpanID())
}

// ============================================================================
// L-N3 — Messages() lazy init must be race-free
// ============================================================================

func TestTracedMessageBatch_Messages_ConcurrentFirstCall_NoRace(t *testing.T) {
	_, _ = setupRecorder(t)

	upstream := make(chan jetstream.Msg)
	close(upstream)
	batch := &mockBatch{ch: upstream}

	tc := WrapConsumer(&mockConsumer{fetchBatch: batch}, "ORDERS")
	tb, err := tc.Fetch(10)
	require.NoError(t, err)

	const n = 16
	var wg sync.WaitGroup
	chans := make([]<-chan *TracedMsg, n)
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			chans[i] = tb.Messages()
		}(i)
	}
	wg.Wait()

	// All callers must observe the same channel (single forwarder).
	for i := 1; i < n; i++ {
		assert.Equal(t, chans[0], chans[i])
	}
}

// ============================================================================
// L-N4 — PubAck sets only message.id, full attribute set unchanged
// ============================================================================

// mockJetStream embeds jetstream.JetStream and overrides only PublishMsg.
type mockJetStream struct {
	jetstream.JetStream
	publishMsg func(ctx context.Context, msg *nats.Msg) (*jetstream.PubAck, error)
}

func (m *mockJetStream) PublishMsg(
	ctx context.Context,
	msg *nats.Msg,
	_ ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	return m.publishMsg(ctx, msg)
}

func TestPublish_RealPublisher_SetsMessageID(t *testing.T) {
	exporter, _ := setupRecorder(t)

	js := &mockJetStream{
		publishMsg: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return &jetstream.PubAck{Sequence: 42}, nil
		},
	}

	p := NewPublisher(js)
	_, err := p.Publish(context.Background(), "orders.created", []byte("data"))
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	attrs := spanAttrMap(spans[0])
	assert.Equal(t, "42", attrs["messaging.message.id"])
	// The base attributes must remain present and not be duplicated/dropped.
	assert.Equal(t, "nats", attrs["messaging.system"])
	assert.Equal(t, "publish", attrs["messaging.operation.name"])
	assert.Equal(t, "send", attrs["messaging.operation.type"])
	assert.Equal(t, "orders.created", attrs["messaging.destination.name"])
}

func TestPublishMsg_RealPublisher_SetsMessageID(t *testing.T) {
	exporter, _ := setupRecorder(t)

	js := &mockJetStream{
		publishMsg: func(_ context.Context, _ *nats.Msg) (*jetstream.PubAck, error) {
			return &jetstream.PubAck{Sequence: 7}, nil
		},
	}

	p := NewPublisher(js)
	_, err := p.PublishMsg(context.Background(), &nats.Msg{Subject: "orders.created", Data: []byte("data")})
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	attrs := spanAttrMap(spans[0])
	assert.Equal(t, "7", attrs["messaging.message.id"])
	assert.Equal(t, "nats", attrs["messaging.system"])
	assert.Equal(t, "orders.created", attrs["messaging.destination.name"])
}

// findSpan returns the recorded span with the given name, failing the test if
// it is not present.
func findSpan(t *testing.T, exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	t.Helper()

	for _, s := range exporter.GetSpans() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("span %q not recorded", name)

	return tracetest.SpanStub{}
}
