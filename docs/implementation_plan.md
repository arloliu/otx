# NATS JetStream OpenTelemetry Support

Provide seamless OpenTelemetry instrumentation for NATS JetStream publish and consume operations via wrapper types.

## User Review Required

> [!IMPORTANT]
> **Interface Design**: We define **focused interfaces** (`Publisher`, `TracedConsumer`) rather than claiming to implement the full `jetstream.JetStream`/`jetstream.Consumer` interfaces. Callers access the underlying JetStream/Consumer via getter methods for non-traced operations.

---

## Proposed Changes

### New Package: `otx/nats`

---

#### [NEW] [carrier.go](file:///home/arlo/projects/lib-go/otx/nats/carrier.go)

NATS header carrier for context propagation.

```go
// headerCarrier adapts nats.Header to propagation.TextMapCarrier
type headerCarrier nats.Header

func (c headerCarrier) Get(key string) string
func (c headerCarrier) Set(key, value string)
func (c headerCarrier) Keys() []string

// InjectNATS injects trace context into NATS headers.
// Initializes headers if nil to prevent panics.
func InjectNATS(ctx context.Context, msg *nats.Msg) {
    if msg.Header == nil {
        msg.Header = make(nats.Header)
    }
    otel.GetTextMapPropagator().Inject(ctx, headerCarrier(msg.Header))
}

// ExtractNATS extracts trace context from NATS headers.
func ExtractNATS(ctx context.Context, header nats.Header) context.Context
```

---

#### [NEW] [attributes.go](file:///home/arlo/projects/lib-go/otx/nats/attributes.go)

Messaging semantic convention attributes:

| Attribute | Usage |
|-----------|-------|
| `messaging.system` = `nats` | Required |
| `messaging.operation.name` | `publish`/`receive`/`process` |
| `messaging.operation.type` | `send`/`receive`/`process` |
| `messaging.destination.name` | Subject name |
| `messaging.consumer.group.name` | Durable consumer name (if applicable) |
| `messaging.message.id` | Message ID from PubAck or metadata |
| `messaging.message.body.size` | Payload length |
| `nats.stream` | Stream name (custom attribute) |

---

#### [NEW] [publisher.go](file:///home/arlo/projects/lib-go/otx/nats/publisher.go)

Publisher wrapper for traced publish operations.

```go
// Publisher provides traced JetStream publish operations.
type Publisher struct {
    js     jetstream.JetStream
    tracer trace.Tracer
    prop   propagation.TextMapPropagator
    opts   options
}

func NewPublisher(js jetstream.JetStream, opts ...Option) *Publisher
func NewPublisherWithProviders(js jetstream.JetStream, tp trace.TracerProvider,
    prop propagation.TextMapPropagator, opts ...Option) *Publisher

// Traced publish methods
func (p *Publisher) Publish(ctx context.Context, subject string, data []byte,
    opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
func (p *Publisher) PublishMsg(ctx context.Context, msg *nats.Msg,
    opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)

// Async publish - uses context.Background() for spans.
// When WithAsyncSpans(false), no span is created and no headers are injected.
// When WithAsyncSpans(true) (default), msg.Header is created if nil before injection.
func (p *Publisher) PublishAsync(subject string, data []byte,
    opts ...jetstream.PublishOpt) (jetstream.PubAckFuture, error)
func (p *Publisher) PublishAsyncMsg(msg *nats.Msg,
    opts ...jetstream.PublishOpt) (jetstream.PubAckFuture, error)

// Access underlying JetStream for non-traced operations
func (p *Publisher) JetStream() jetstream.JetStream
```

**Publish span**: Kind=`PRODUCER`, Name=`publish {subject}`

---

#### [NEW] [consumer.go](file:///home/arlo/projects/lib-go/otx/nats/consumer.go)

Consumer wrapper with traced consume operations.

```go
// TracedConsumer wraps a jetstream.Consumer with tracing.
type TracedConsumer struct {
    consumer jetstream.Consumer
    stream   string
    tracer   trace.Tracer
    prop     propagation.TextMapPropagator
    opts     options
}

func WrapConsumer(c jetstream.Consumer, stream string, opts ...Option) *TracedConsumer
func WrapConsumerWithProviders(c jetstream.Consumer, stream string,
    tp trace.TracerProvider, prop propagation.TextMapPropagator,
    opts ...Option) *TracedConsumer

// Returns wrapped MessageBatch with traced messages
func (tc *TracedConsumer) Fetch(batch int, opts ...jetstream.FetchOpt) (*TracedMessageBatch, error)
func (tc *TracedConsumer) FetchBytes(maxBytes int, opts ...jetstream.FetchOpt) (*TracedMessageBatch, error)
func (tc *TracedConsumer) FetchNoWait(batch int) (*TracedMessageBatch, error)

// Returns wrapped context for iterator pattern
func (tc *TracedConsumer) Messages(opts ...jetstream.PullMessagesOpt) (*TracedMessagesContext, error)

// Next returns a single traced message
func (tc *TracedConsumer) Next(opts ...jetstream.FetchOpt) (*TracedMsg, error)

// For callback pattern - use MessageHandlerWithTracing instead
func (tc *TracedConsumer) Consume(handler jetstream.MessageHandler,
    opts ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error)

// Access underlying consumer
func (tc *TracedConsumer) Consumer() jetstream.Consumer
```

**Span Kinds** (per [semconv](https://opentelemetry.io/docs/specs/semconv/messaging/messaging-spans/#span-kind)):
- `Fetch`/`Next`/`Messages`: Kind=`CLIENT` (receive operation - pulling from server)
- `process` spans on individual messages: Kind=`CONSUMER`

---

#### [NEW] [message.go](file:///home/arlo/projects/lib-go/otx/nats/message.go)

Wrapped message types exposing extracted trace context.

```go
// TracedMsg embeds jetstream.Msg and provides trace context.
// Existing handlers work unchanged; use Context() for traced workflows.
type TracedMsg struct {
    jetstream.Msg
    ctx context.Context
}

// Context returns context with extracted trace (derived from message headers).
func (m *TracedMsg) Context() context.Context

// TracedMessageBatch wraps jetstream.MessageBatch.
type TracedMessageBatch struct {
    batch  jetstream.MessageBatch
    tracer trace.Tracer
    prop   propagation.TextMapPropagator
    stream string
}

// Messages returns a channel of traced messages. The channel blocks until
// messages arrive or the batch completes. Always check Error() after the
// channel closes to detect fetch failures.
func (b *TracedMessageBatch) Messages() <-chan *TracedMsg
func (b *TracedMessageBatch) Error() error

// TracedMessagesContext wraps jetstream.MessagesContext.
type TracedMessagesContext struct {
    ctx    jetstream.MessagesContext
    tracer trace.Tracer
    prop   propagation.TextMapPropagator
    stream string
}

func (c *TracedMessagesContext) Next() (*TracedMsg, error)
func (c *TracedMessagesContext) Stop()
func (c *TracedMessagesContext) Drain()
```

---

#### [NEW] [handler.go](file:///home/arlo/projects/lib-go/otx/nats/handler.go)

Message handler wrapper for callback-style consumption.

```go
// MessageHandlerWithTracing wraps a handler to add process spans.
// The returned jetstream.MessageHandler receives the original jetstream.Msg,
// extracts trace context from headers, wraps it as *TracedMsg (with Context()
// derived from extracted trace), starts a process span, then calls your handler.
func MessageHandlerWithTracing(handler func(*TracedMsg), stream string,
    opts ...Option) jetstream.MessageHandler

// MessageHandlerWithTracingProviders with explicit providers.
func MessageHandlerWithTracingProviders(handler func(*TracedMsg), stream string,
    tp trace.TracerProvider, prop propagation.TextMapPropagator,
    opts ...Option) jetstream.MessageHandler
```

**Process span**: Kind=`CONSUMER`, Name=`process {stream}`

---

#### [NEW] [options.go](file:///home/arlo/projects/lib-go/otx/nats/options.go)

```go
type Option func(*options)

func WithTracerName(name string) Option
func WithPropagator(prop propagation.TextMapPropagator) Option  // Override propagator
func WithProcessSpans(enabled bool) Option      // Enable per-message process spans (default: true)
func WithAsyncSpans(enabled bool) Option        // Enable spans+header injection for PublishAsync (default: true)
```

---

## Usage Examples

### Publisher

```go
js, _ := jetstream.New(nc)
publisher := natsotx.NewPublisher(js)

// Traced publish - context propagated via headers
publisher.Publish(ctx, "orders.created", data)

// For non-traced operations
publisher.JetStream().CreateStream(ctx, cfg)
```

### Consumer with Fetch

```go
consumer, _ := stream.CreateConsumer(ctx, cfg)
traced := natsotx.WrapConsumer(consumer, "ORDERS")

msgs, _ := traced.Fetch(10)
for msg := range msgs.Messages() {
    processOrder(msg.Context(), msg.Data())
    msg.Ack()
}
if err := msgs.Error(); err != nil {
    log.Error("fetch error", err)
}
```

### Consumer with Consume callback

```go
consumer.Consume(natsotx.MessageHandlerWithTracing(func(msg *natsotx.TracedMsg) {
    processOrder(msg.Context(), msg.Data())
    msg.Ack()
}, "ORDERS"))
```

---

## Verification Plan

### Automated Tests

```bash
cd /home/arlo/projects/lib-go/otx && go test -v ./nats/...
cd /home/arlo/projects/lib-go/otx && make lint
```

Key test cases:
1. **TestHeaderCarrier** - nil header handling, Get/Set/Keys
2. **TestPublishSpan** - PRODUCER span, attributes, context injection
3. **TestPublishAsync** - uses background context, span created
4. **TestFetchSpan** - CLIENT span for receive
5. **TestProcessSpan** - CONSUMER span for message handler
6. **TestTracePropagation** - publish→consume trace correlation
