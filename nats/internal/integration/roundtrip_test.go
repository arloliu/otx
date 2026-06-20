package integration

import (
	"context"
	"testing"
	"time"

	otxnats "github.com/arloliu/otx/nats"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// H2 — a real publish -> JetStream -> consume round-trip must carry the trace
// context across the wire, so the consumed message shares the producer's trace
// and the process span parents under it. Mocks cannot reveal whether the broker
// preserves traceparent/tracestate headers; an embedded server can.
func TestNATS_PublishConsume_TraceContextCrossesWire(t *testing.T) {
	js := connectJetStream(t)
	cons := newStreamConsumer(t, js, "ORDERS", "orders.>", 30*time.Second)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prop := propagation.TraceContext{}

	pub := otxnats.NewPublisherWithProviders(js, tp, prop)
	tc := otxnats.WrapConsumerWithProviders(cons, "ORDERS", tp, prop)

	// Publish under a parent span.
	pctx, parent := tp.Tracer("test").Start(context.Background(), "produce")
	_, err := pub.Publish(pctx, "orders.created", []byte("hello"))
	require.NoError(t, err)
	parent.End()
	wantTrace := parent.SpanContext().TraceID()

	// Consume the delivered message.
	msg, err := tc.NextContext(context.Background(), jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	require.NotNil(t, msg)

	// The consumed message's extracted context shares the producer's trace and is
	// marked remote (it came over the wire), proving traceparent survived.
	consumed := oteltrace.SpanContextFromContext(msg.Context())
	assert.Equal(t, wantTrace, consumed.TraceID(), "trace id must cross the wire via headers")
	assert.True(t, consumed.IsRemote(), "consumed span context must be remote")

	// A process span parents under the propagated producer context.
	_, end := msg.StartProcessSpanWithTracer(tp)
	end(nil)
	require.NoError(t, msg.Ack())

	var processSpan tracetest.SpanStub
	for _, s := range exporter.GetSpans() {
		if s.SpanKind == oteltrace.SpanKindConsumer {
			processSpan = s
		}
	}
	require.True(t, processSpan.SpanContext.IsValid(), "a process (consumer) span must be recorded")
	assert.Equal(t, wantTrace, processSpan.SpanContext.TraceID(), "process span joins the producer trace")
	assert.True(t, processSpan.Parent.IsRemote(), "process span parents under the wire context")
}
