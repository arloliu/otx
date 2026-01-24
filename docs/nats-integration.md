# NATS Integration

OTX provides comprehensive tracing for NATS JetStream operations, following [OpenTelemetry Messaging Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/messaging/).

## Overview

The `otx/nats` package provides:
- **Publisher**: Traced JetStream publishing with context injection
- **TracedConsumer**: Traced message fetching with context extraction
- **MessageHandler**: Traced message processing with automatic spans
- **TracedMsg**: Message wrapper with trace context

## Publisher

### Basic Usage

```go
import (
    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"
    otxnats "github.com/arloliu/otx/nats"
)

func main() {
    nc, _ := nats.Connect(nats.DefaultURL)
    js, _ := jetstream.New(nc)

    // Create traced publisher
    publisher := otxnats.NewPublisher(js)

    // Publish with tracing
    ctx := context.Background()
    ack, err := publisher.Publish(ctx, "orders.created", orderData)
}
```

### Publish Methods

```go
// Simple publish
ack, err := publisher.Publish(ctx, "orders.created", data)

// Publish with message struct
msg := &nats.Msg{
    Subject: "orders.created",
    Data:    data,
    Header:  nats.Header{"X-Custom": []string{"value"}},
}
ack, err := publisher.PublishMsg(ctx, msg)

// Async publish (span optional via WithAsyncSpans)
future, err := publisher.PublishAsync("orders.created", data)
```

### Publisher Options

```go
publisher := otxnats.NewPublisher(js,
    otxnats.WithTracerName("order-service"),
    otxnats.WithPropagator(customPropagator),
    otxnats.WithAsyncSpans(false),  // Disable async publish spans
)
```

### With Explicit Providers

```go
publisher := otxnats.NewPublisherWithProviders(
    js,
    tracerProvider,
    propagator,
    otxnats.WithTracerName("order-service"),
)
```

## Consumer

### Basic Usage

```go
// Get JetStream consumer
consumer, _ := js.Consumer(ctx, "ORDERS", "order-processor")

// Wrap with tracing
tracedConsumer := otxnats.WrapConsumer(consumer, "ORDERS")

// Fetch messages
batch, err := tracedConsumer.Fetch(10)
for msg := range batch.Messages() {
    ctx := msg.Context()  // Contains extracted trace context
    processOrder(ctx, msg.Data())
    msg.Ack()
}
```

### Fetch Methods

```go
// Fetch batch
batch, err := tracedConsumer.Fetch(10)

// Fetch by bytes
batch, err := tracedConsumer.FetchBytes(1024 * 1024)  // 1MB

// Fetch without waiting
batch, err := tracedConsumer.FetchNoWait(10)

// Fetch single message
msg, err := tracedConsumer.Next()

// Continuous consumption
msgs, err := tracedConsumer.Messages()
for {
    msg, err := msgs.Next()
    if err != nil {
        break
    }
    processOrder(msg.Context(), msg.Data())
    msg.Ack()
}
```

### Consumer Options

```go
tracedConsumer := otxnats.WrapConsumer(consumer, "ORDERS",
    otxnats.WithTracerName("order-processor"),
    otxnats.WithProcessSpans(true),  // Enable per-message process spans
    otxnats.WithStream("ORDERS"),    // Override stream name
)
```

## Message Handler

For `Consumer.Consume()` callback pattern:

```go
// Define handler with tracing
handler := otxnats.MessageHandlerWithTracing(func(msg *otxnats.TracedMsg) {
    ctx := msg.Context()  // Contains span context

    if err := processOrder(ctx, msg.Data()); err != nil {
        otx.RecordError(ctx, err)
        msg.Nak()
        return
    }

    otx.SetSuccess(ctx)
    msg.Ack()
})

// Use with JetStream consumer
consumer.Consume(handler)
```

### With Explicit Providers

```go
handler := otxnats.MessageHandlerWithTracingProviders(
    func(msg *otxnats.TracedMsg) {
        // Process message
    },
    tracerProvider,
    propagator,
    otxnats.WithStream("ORDERS"),
)
```

## TracedMsg

For manual tracing control:

```go
// From Consumer.Consume callback
consumer.Consume(func(msg jetstream.Msg) {
    // Wrap message to extract trace context
    tracedMsg := otxnats.NewTracedMsg(msg)
    ctx := tracedMsg.Context()

    // Create process span manually
    ctx, endSpan := tracedMsg.StartProcessSpan()
    defer endSpan(nil)

    if err := processOrder(ctx, msg.Data()); err != nil {
        endSpan(err)  // Records error
        msg.Nak()
        return
    }

    msg.Ack()
})
```

## Context Injection/Extraction

### Manual Injection

```go
// Inject trace context into NATS message
msg := &nats.Msg{
    Subject: "orders.created",
    Data:    data,
}
otxnats.InjectNATS(ctx, msg)

// With custom propagator
otxnats.InjectNATSWithPropagator(ctx, msg, customPropagator)
```

### Manual Extraction

```go
// Extract trace context from NATS message
ctx := otxnats.ExtractNATS(context.Background(), msg.Header)
```

## Span Naming

Following [Messaging Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/messaging/):

| Operation | Span Name | Span Kind |
|-----------|-----------|-----------|
| Publish | `"publish {subject}"` | Producer |
| Receive/Fetch | `"receive {stream}"` | Client |
| Process | `"process {stream}"` | Consumer |

## Attributes

OTX automatically sets messaging semantic convention attributes:

| Attribute | Description | Example |
|-----------|-------------|---------|
| `messaging.system` | Messaging system | `"nats"` |
| `messaging.operation.name` | Operation name | `"publish"`, `"receive"`, `"process"` |
| `messaging.operation.type` | Operation type | `"send"`, `"receive"`, `"process"` |
| `messaging.destination.name` | Subject/stream | `"orders.created"` |
| `messaging.message.id` | Message ID | `"msg-123"` |
| `messaging.message.body.size` | Payload size | `1024` |
| `messaging.consumer.group.name` | Consumer name | `"order-processor"` |

## Best Practices

### 1. Always Use Context

```go
handler := otxnats.MessageHandlerWithTracing(func(msg *otxnats.TracedMsg) {
    ctx := msg.Context()  // ✅ Always get context from message

    // Pass context to downstream calls
    s.repo.SaveOrder(ctx, order)
    s.notify.Send(ctx, notification)
})
```

### 2. Handle Errors Properly

```go
handler := otxnats.MessageHandlerWithTracing(func(msg *otxnats.TracedMsg) {
    ctx := msg.Context()

    order, err := parseOrder(msg.Data())
    if err != nil {
        otx.RecordError(ctx, err)
        // Don't ack - message will be redelivered
        return
    }

    if err := s.processOrder(ctx, order); err != nil {
        otx.RecordError(ctx, err)
        msg.Nak()  // Explicit nak
        return
    }

    otx.SetSuccess(ctx)
    msg.Ack()
})
```

### 3. Add Business Attributes

```go
handler := otxnats.MessageHandlerWithTracing(func(msg *otxnats.TracedMsg) {
    ctx := msg.Context()

    order, _ := parseOrder(msg.Data())

    // Add business context
    otx.SetAttributes(ctx,
        attribute.String("order.id", order.ID),
        attribute.String("order.type", order.Type),
        attribute.Float64("order.total", order.Total),
    )

    // Process...
})
```

### 4. Use Stream Override for Multiple Streams

```go
// When consumer handles multiple streams
handler := otxnats.MessageHandlerWithTracing(
    processMessage,
    otxnats.WithStream("ORDERS"),  // Explicit stream name
)
```

### 5. Disable Async Spans When Not Needed

```go
// For high-throughput async publishing, disable spans
publisher := otxnats.NewPublisher(js,
    otxnats.WithAsyncSpans(false),
)
```

## Testing

```go
func TestMessageHandler(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

    handler := otxnats.MessageHandlerWithTracingProviders(
        func(msg *otxnats.TracedMsg) {
            msg.Ack()
        },
        tp,
        nil,
    )

    // Create mock message and call handler
    msg := &mockMsg{data: []byte("test")}
    handler(msg)

    // Assert spans
    spans := exporter.GetSpans()
    require.Len(t, spans, 1)
    assert.Equal(t, "process ", spans[0].Name)
}
```

## References

- [Messaging Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/messaging/)
- [NATS JetStream Documentation](https://docs.nats.io/nats-concepts/jetstream)
