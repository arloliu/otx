package nats

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracedMsg wraps a jetstream.Msg with trace context.
// Use Context() to access the extracted trace context for downstream propagation.
type TracedMsg struct {
	jetstream.Msg
	ctx context.Context
}

// Context returns the context containing the extracted trace.
// Use this to propagate trace context to downstream operations.
func (m *TracedMsg) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}

	return m.ctx
}

// StartProcessSpan creates a process span for this message using the global TracerProvider.
// It returns a new context containing the span and an end function that should be called
// when processing is complete.
//
// The span is created with proper OTel messaging semantic convention attributes
// derived from the message metadata (stream, consumer, subject, size).
// The stream name is extracted from the message metadata automatically.
//
// Example:
//
//	consumer.Consume(func(msg jetstream.Msg) {
//	    tracedMsg := otelnats.NewTracedMsg(msg)
//	    ctx, endSpan := tracedMsg.StartProcessSpan()
//	    defer endSpan(nil)
//
//	    if err := processOrder(ctx, msg.Data()); err != nil {
//	        endSpan(err) // Records error and sets error status
//	        msg.Nak()
//	        return
//	    }
//	    msg.Ack()
//	})
func (m *TracedMsg) StartProcessSpan(opts ...Option) (context.Context, func(error)) {
	return m.StartProcessSpanWithTracer(nil, opts...)
}

// StartProcessSpanWithTracer creates a process span using the provided TracerProvider.
// If tp is nil, the global TracerProvider is used.
func (m *TracedMsg) StartProcessSpanWithTracer(
	tp trace.TracerProvider,
	opts ...Option,
) (context.Context, func(error)) {
	o := applyOptions(opts)
	tracer := getTracer(tp, o)

	// Extract message metadata for attributes
	stream := ""
	consumerName := ""
	subject := ""
	messageID := ""
	bodySize := 0

	if m.Msg != nil {
		if metadata, err := m.Msg.Metadata(); err == nil && metadata != nil {
			stream = metadata.Stream
			consumerName = metadata.Consumer
		}

		if m.Msg.Subject() != "" {
			subject = m.Msg.Subject()
		}

		bodySize = len(m.Msg.Data())
	}

	// Allow stream override via option
	if o.stream != "" {
		stream = o.stream
	}

	// Create span name following semconv
	spanName := opTypeProcess + " " + stream

	// Start span with proper kind and attributes
	ctx, span := tracer.Start(m.Context(), spanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(processAttributes(stream, consumerName, subject, messageID, bodySize)...),
	)

	// Return context and end function
	endFunc := func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}

		span.End()
	}

	return ctx, endFunc
}

// NewTracedMsg creates a TracedMsg from a jetstream.Msg by extracting
// trace context from the message headers using the global propagator.
//
// Use this function when you have a jetstream.Msg from your own consumption
// mechanism (e.g., from Consumer.Consume callback) and want to extract the
// propagated trace context without fully adopting TracedConsumer.
//
// Example:
//
//	consumer.Consume(func(msg jetstream.Msg) {
//	    tracedMsg := otelnats.NewTracedMsg(msg)
//	    ctx := tracedMsg.Context() // Contains extracted trace context
//	    processOrder(ctx, msg)
//	    msg.Ack()
//	})
func NewTracedMsg(msg jetstream.Msg) *TracedMsg {
	return NewTracedMsgWithPropagator(msg, nil)
}

// NewTracedMsgWithPropagator creates a TracedMsg from a jetstream.Msg using
// the provided propagator. If prop is nil, the global propagator is used.
func NewTracedMsgWithPropagator(msg jetstream.Msg, prop propagation.TextMapPropagator) *TracedMsg {
	ctx := context.Background()

	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}

	if msg != nil {
		headers := msg.Headers()
		if headers != nil {
			ctx = prop.Extract(ctx, headerCarrier(headers))
		}
	}

	return &TracedMsg{
		Msg: msg,
		ctx: ctx,
	}
}

// TracedMessageBatch wraps a jetstream.MessageBatch with tracing support.
type TracedMessageBatch struct {
	batch      jetstream.MessageBatch
	msgChan    chan *TracedMsg
	ctx        context.Context
	opts       options
	stream     string
	extractCtx func(context.Context, jetstream.Msg) context.Context
}

// Messages returns a channel of traced messages.
// The channel blocks until messages arrive or the batch completes.
// Always check Error() after the channel closes to detect fetch failures.
func (b *TracedMessageBatch) Messages() <-chan *TracedMsg {
	if b.msgChan != nil {
		return b.msgChan
	}

	b.msgChan = make(chan *TracedMsg)

	go func() {
		defer close(b.msgChan)

		for msg := range b.batch.Messages() {
			tracedMsg := &TracedMsg{
				Msg: msg,
				ctx: b.extractCtx(b.ctx, msg),
			}
			b.msgChan <- tracedMsg
		}
	}()

	return b.msgChan
}

// Error returns any error that occurred during the fetch operation.
// Should be called after Messages() channel is closed.
func (b *TracedMessageBatch) Error() error {
	return b.batch.Error()
}

// TracedMessagesContext wraps a jetstream.MessagesContext with tracing support.
type TracedMessagesContext struct {
	messagesCtx jetstream.MessagesContext
	ctx         context.Context
	opts        options
	stream      string
	extractCtx  func(context.Context, jetstream.Msg) context.Context
}

// Next retrieves the next message with trace context extracted.
func (c *TracedMessagesContext) Next() (*TracedMsg, error) {
	msg, err := c.messagesCtx.Next()
	if err != nil {
		return nil, err
	}

	return &TracedMsg{
		Msg: msg,
		ctx: c.extractCtx(c.ctx, msg),
	}, nil
}

// Stop signals the iterator to stop.
func (c *TracedMessagesContext) Stop() {
	c.messagesCtx.Stop()
}

// Drain allows in-flight messages to be processed before stopping.
func (c *TracedMessagesContext) Drain() {
	c.messagesCtx.Drain()
}
