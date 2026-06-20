package integration

import (
	"context"
	"testing"
	"time"

	otxnats "github.com/arloliu/otx/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// newPubConsumer wires a real publisher + traced consumer over a fresh stream.
func newPubConsumer(
	t *testing.T, stream, subject string, opts ...otxnats.Option,
) (*otxnats.Publisher, *otxnats.TracedConsumer) {
	t.Helper()
	js := connectJetStream(t)
	cons := newStreamConsumer(t, js, stream, subject, 30*time.Second)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter()))
	prop := propagation.TraceContext{}

	pub := otxnats.NewPublisherWithProviders(js, tp, prop, opts...)
	tc := otxnats.WrapConsumerWithProviders(cons, stream, tp, prop, opts...)

	return pub, tc
}

// drainBatch collects every message a batch delivers, acking each, and returns
// the count plus the batch's terminal error.
func drainBatch(t *testing.T, batch *otxnats.TracedMessageBatch) int {
	t.Helper()
	n := 0
	for msg := range batch.Messages() {
		require.NoError(t, msg.Ack())
		n++
	}
	require.NoError(t, batch.Error())

	return n
}

// M8 — PublishAsync (with async spans) injects trace context and delivers.
func TestNATS_PublishAsync_DeliversWithTrace(t *testing.T) {
	pub, tc := newPubConsumer(t, "ASYNC", "async.>", otxnats.WithAsyncSpans(true))

	future, err := pub.PublishAsync("async.evt", []byte("payload"))
	require.NoError(t, err)
	select {
	case <-future.Ok():
	case aerr := <-future.Err():
		t.Fatalf("async publish failed: %v", aerr)
	case <-time.After(5 * time.Second):
		t.Fatal("async publish ack timeout")
	}

	msg, err := tc.NextContext(context.Background(), jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	require.NoError(t, msg.Ack())
	assert.True(t, oteltrace.SpanContextFromContext(msg.Context()).IsRemote(),
		"async-published message must carry wire trace context")
}

// M8 — PublishAsyncMsg (caller-owned *nats.Msg, async spans) injects trace
// context and delivers through the real broker.
func TestNATS_PublishAsyncMsg_DeliversWithTrace(t *testing.T) {
	pub, tc := newPubConsumer(t, "ASYNCMSG", "asyncmsg.>", otxnats.WithAsyncSpans(true))

	future, err := pub.PublishAsyncMsg(&nats.Msg{Subject: "asyncmsg.evt", Data: []byte("payload")})
	require.NoError(t, err)
	select {
	case <-future.Ok():
	case aerr := <-future.Err():
		t.Fatalf("async publish failed: %v", aerr)
	case <-time.After(5 * time.Second):
		t.Fatal("async publish ack timeout")
	}

	msg, err := tc.NextContext(context.Background(), jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	require.NoError(t, msg.Ack())
	assert.True(t, oteltrace.SpanContextFromContext(msg.Context()).IsRemote(),
		"async-published message must carry wire trace context")
}

// M9 — the handler-callback Consume path delivers a message to the handler.
func TestNATS_Consume_HandlerReceivesMessage(t *testing.T) {
	pub, tc := newPubConsumer(t, "CONSUME", "consume.>")
	_, err := pub.Publish(context.Background(), "consume.item", []byte("x"))
	require.NoError(t, err)

	received := make(chan struct{}, 1)
	cc, err := tc.Consume(func(msg jetstream.Msg) {
		_ = msg.Ack()
		select {
		case received <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(cc.Stop)

	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("Consume handler was not invoked")
	}
}

// M8 — FetchContext returns a batch the caller iterates and acks.
func TestNATS_FetchContext_BatchDeliversAll(t *testing.T) {
	pub, tc := newPubConsumer(t, "BATCH", "batch.>")
	for range 3 {
		_, err := pub.Publish(context.Background(), "batch.item", []byte("x"))
		require.NoError(t, err)
	}

	batch, err := tc.FetchContext(context.Background(), 3, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	assert.Equal(t, 3, drainBatch(t, batch))
}

// M8 — FetchBytesContext returns available messages bounded by bytes.
func TestNATS_FetchBytesContext_Delivers(t *testing.T) {
	pub, tc := newPubConsumer(t, "BYTES", "bytes.>")
	_, err := pub.Publish(context.Background(), "bytes.item", []byte("hello"))
	require.NoError(t, err)

	// A byte-bounded fetch fills until maxBytes or MaxWait; keep the wait short.
	batch, err := tc.FetchBytesContext(context.Background(), 1<<20, jetstream.FetchMaxWait(time.Second))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, drainBatch(t, batch), 1)
}

// M8 — FetchNoWaitContext returns immediately with whatever is available.
func TestNATS_FetchNoWaitContext_ReturnsAvailable(t *testing.T) {
	pub, tc := newPubConsumer(t, "NOWAIT", "nowait.>")
	for range 2 {
		_, err := pub.Publish(context.Background(), "nowait.item", []byte("x"))
		require.NoError(t, err)
	}

	// A no-wait fetch returns immediately with whatever is currently available;
	// poll it (no sleep) until both persisted messages have been collected.
	got := 0
	require.Eventually(t, func() bool {
		batch, err := tc.FetchNoWaitContext(context.Background(), 10)
		if err != nil {
			return false
		}
		for msg := range batch.Messages() {
			if msg.Ack() == nil {
				got++
			}
		}

		return got >= 2
	}, 5*time.Second, 20*time.Millisecond, "no-wait fetch must return the persisted messages")
}

// M9 — the continuous-consumption iterator delivers via Next and shuts down
// cleanly via Stop/Drain.
func TestNATS_MessagesIterator_NextThenDrain(t *testing.T) {
	pub, tc := newPubConsumer(t, "ITER", "iter.>")
	for range 2 {
		_, err := pub.Publish(context.Background(), "iter.item", []byte("x"))
		require.NoError(t, err)
	}

	it, err := tc.MessagesWithContext(context.Background())
	require.NoError(t, err)

	msg, err := it.Next()
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.NoError(t, msg.Ack())

	// Drain in-flight then stop; must not hang or panic.
	it.Drain()
	it.Stop()
}
