package integration

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// startEmbeddedServer boots an in-process NATS server with JetStream enabled on
// an ephemeral port and returns its client URL. The server and its temp store
// are torn down when the test ends.
func startEmbeddedServer(t *testing.T) string {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // ephemeral
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	}
	ns, err := natsserver.NewServer(opts)
	require.NoError(t, err)

	ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		ns.Shutdown()
		t.Fatal("embedded NATS server did not become ready")
	}
	t.Cleanup(ns.Shutdown)

	return ns.ClientURL()
}

// connectJetStream connects to the embedded server and returns a JetStream
// handle. The connection is closed when the test ends.
func connectJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	nc, err := nats.Connect(startEmbeddedServer(t))
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	return js
}

// newStreamConsumer creates a stream over subjects and an explicit-ack pull
// consumer with the given AckWait, returning the consumer.
func newStreamConsumer(
	t *testing.T, js jetstream.JetStream, stream, subject string, ackWait time.Duration,
) jetstream.Consumer {
	t.Helper()
	ctx := context.Background()
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     stream,
		Subjects: []string{subject},
	})
	require.NoError(t, err)

	cons, err := js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
		Durable:   "worker",
		AckPolicy: jetstream.AckExplicitPolicy,
		AckWait:   ackWait,
	})
	require.NoError(t, err)

	return cons
}
