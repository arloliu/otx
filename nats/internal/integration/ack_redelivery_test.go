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
)

// H3 — Nak without Ack must trigger a real redelivery (second delivery, a second
// process span), and Ack must advance the consumer so the message is not
// redelivered. Mocks return nil for Ack/Nak and never exercise these semantics.
func TestNATS_NakRedeliversThenAckAdvances(t *testing.T) {
	js := connectJetStream(t)
	// Short AckWait so a missing Ack also redelivers, and Nak redelivers promptly.
	cons := newStreamConsumer(t, js, "JOBS", "jobs.>", time.Second)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prop := propagation.TraceContext{}

	pub := otxnats.NewPublisherWithProviders(js, tp, prop)
	tc := otxnats.WrapConsumerWithProviders(cons, "JOBS", tp, prop)

	_, err := pub.Publish(context.Background(), "jobs.do", []byte("payload"))
	require.NoError(t, err)

	// First delivery: process then Nak (no Ack) -> must be redelivered.
	m1, err := tc.NextContext(context.Background(), jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	meta1, err := m1.Metadata()
	require.NoError(t, err)
	assert.Equal(t, uint64(1), meta1.NumDelivered, "first delivery")
	_, end := m1.StartProcessSpanWithTracer(tp)
	end(nil)
	require.NoError(t, m1.Nak())

	// Redelivery: NumDelivered increments, a second process span is produced.
	m2, err := tc.NextContext(context.Background(), jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	meta2, err := m2.Metadata()
	require.NoError(t, err)
	assert.Equal(t, uint64(2), meta2.NumDelivered, "Nak must redeliver")
	_, end2 := m2.StartProcessSpanWithTracer(tp)
	end2(nil)
	require.NoError(t, m2.Ack())

	// Two process spans, one per delivery.
	process := 0
	for _, s := range exporter.GetSpans() {
		if s.Name == "process JOBS" {
			process++
		}
	}
	assert.Equal(t, 2, process, "one process span per delivery")

	// After Ack the message is gone: a bounded fetch yields no message.
	_, err = tc.NextContext(context.Background(), jetstream.FetchMaxWait(time.Second))
	assert.Error(t, err, "Ack must advance the consumer (no redelivery)")
}
