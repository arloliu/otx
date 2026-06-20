package nats

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracedConsumer wraps a jetstream.Consumer with OpenTelemetry tracing.
type TracedConsumer struct {
	consumer jetstream.Consumer
	stream   string
	tracer   trace.Tracer
	prop     propagation.TextMapPropagator
	opts     options
}

// WrapConsumer wraps a Consumer with tracing using the global providers.
func WrapConsumer(c jetstream.Consumer, stream string, opts ...Option) *TracedConsumer {
	return WrapConsumerWithProviders(c, stream, nil, nil, opts...)
}

// WrapConsumerWithProviders wraps a Consumer with explicit providers.
// If tp is nil, the global TracerProvider is used.
// If prop is nil, the global TextMapPropagator is used (or opts.prop if set).
//
// Tracer precedence: on the default tracer name, an otx-configured global
// tracer takes precedence over an explicit non-nil tp. Pass WithTracerName to
// force use of the supplied tp under your own instrumentation name.
//
// Panics if c is nil.
func WrapConsumerWithProviders(
	c jetstream.Consumer,
	stream string,
	tp trace.TracerProvider,
	prop propagation.TextMapPropagator,
	opts ...Option,
) *TracedConsumer {
	if c == nil {
		panic("otx/nats: Consumer must not be nil")
	}
	o := applyOptions(opts)

	if prop != nil {
		o.prop = prop
	}

	return &TracedConsumer{
		consumer: c,
		stream:   stream,
		tracer:   getTracer(tp, o),
		prop:     getPropagator(o),
		opts:     o,
	}
}

// Consumer returns the underlying jetstream.Consumer for non-traced operations.
func (tc *TracedConsumer) Consumer() jetstream.Consumer {
	return tc.consumer
}

// CachedInfo returns the cached consumer info.
func (tc *TracedConsumer) CachedInfo() *jetstream.ConsumerInfo {
	return tc.consumer.CachedInfo()
}

// Info fetches the latest consumer info.
func (tc *TracedConsumer) Info(ctx context.Context) (*jetstream.ConsumerInfo, error) {
	return tc.consumer.Info(ctx)
}

// startFetchSpanCtx starts a receive span as a child of the supplied context,
// allowing the span to nest under an ambient caller span.
func (tc *TracedConsumer) startFetchSpanCtx(ctx context.Context) (context.Context, trace.Span) {
	consumerName := ""
	if info := tc.consumer.CachedInfo(); info != nil {
		consumerName = info.Name
	}

	spanName := opTypeReceive + " " + tc.stream

	return tc.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(receiveAttributes(tc.stream, consumerName, 0)...),
	)
}

// wrapBatch wraps a MessageBatch with tracing support.
//
// ctx carries the receive span used to parent extracted message contexts; it is
// also used as the parent of the batch's internal cancellation context so that a
// cancelled caller context terminates the forwarder goroutine.
func (tc *TracedConsumer) wrapBatch(ctx context.Context, batch jetstream.MessageBatch) *TracedMessageBatch {
	cancelCtx, cancel := context.WithCancel(ctx)

	return &TracedMessageBatch{
		batch:      batch,
		ctx:        ctx,
		cancelCtx:  cancelCtx,
		cancel:     cancel,
		opts:       tc.opts,
		stream:     tc.stream,
		extractCtx: tc.extractContext,
	}
}

// Fetch retrieves a batch of messages with tracing.
// Returns a TracedMessageBatch where each message has trace context extracted.
//
// Deprecated: use FetchContext to allow the receive span to nest under an
// ambient context and to provide a cancelable context for the batch forwarder.
func (tc *TracedConsumer) Fetch(batch int, opts ...jetstream.FetchOpt) (*TracedMessageBatch, error) {
	return tc.FetchContext(context.Background(), batch, opts...)
}

// FetchContext retrieves a batch of messages with tracing, starting the receive
// span as a child of ctx. The returned batch's forwarder is cancelled if ctx is
// cancelled (or via TracedMessageBatch.Stop()).
func (tc *TracedConsumer) FetchContext(
	ctx context.Context,
	batch int,
	opts ...jetstream.FetchOpt,
) (*TracedMessageBatch, error) {
	spanCtx, span := tc.startFetchSpanCtx(ctx)

	msgBatch, err := tc.consumer.Fetch(batch, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return tc.wrapBatch(spanCtx, msgBatch), nil
}

// FetchBytes retrieves messages up to maxBytes with tracing.
//
// Deprecated: use FetchBytesContext to allow the receive span to nest under an
// ambient context and to provide a cancelable context for the batch forwarder.
func (tc *TracedConsumer) FetchBytes(maxBytes int, opts ...jetstream.FetchOpt) (*TracedMessageBatch, error) {
	return tc.FetchBytesContext(context.Background(), maxBytes, opts...)
}

// FetchBytesContext retrieves messages up to maxBytes with tracing, starting the
// receive span as a child of ctx.
func (tc *TracedConsumer) FetchBytesContext(
	ctx context.Context,
	maxBytes int,
	opts ...jetstream.FetchOpt,
) (*TracedMessageBatch, error) {
	spanCtx, span := tc.startFetchSpanCtx(ctx)

	msgBatch, err := tc.consumer.FetchBytes(maxBytes, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return tc.wrapBatch(spanCtx, msgBatch), nil
}

// FetchNoWait retrieves available messages without waiting.
//
// Deprecated: use FetchNoWaitContext to allow the receive span to nest under an
// ambient context and to provide a cancelable context for the batch forwarder.
func (tc *TracedConsumer) FetchNoWait(batch int) (*TracedMessageBatch, error) {
	return tc.FetchNoWaitContext(context.Background(), batch)
}

// FetchNoWaitContext retrieves available messages without waiting, starting the
// receive span as a child of ctx.
func (tc *TracedConsumer) FetchNoWaitContext(ctx context.Context, batch int) (*TracedMessageBatch, error) {
	spanCtx, span := tc.startFetchSpanCtx(ctx)

	msgBatch, err := tc.consumer.FetchNoWait(batch)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return tc.wrapBatch(spanCtx, msgBatch), nil
}

// Messages returns an iterator for continuous message consumption with tracing.
//
// Deprecated: use MessagesWithContext so extracted message contexts can nest
// under an ambient context.
func (tc *TracedConsumer) Messages(opts ...jetstream.PullMessagesOpt) (*TracedMessagesContext, error) {
	return tc.MessagesWithContext(context.Background(), opts...)
}

// MessagesWithContext returns an iterator for continuous message consumption
// with tracing. The supplied ctx parents the extracted message contexts.
func (tc *TracedConsumer) MessagesWithContext(
	ctx context.Context,
	opts ...jetstream.PullMessagesOpt,
) (*TracedMessagesContext, error) {
	messagesCtx, err := tc.consumer.Messages(opts...)
	if err != nil {
		return nil, err
	}

	return &TracedMessagesContext{
		messagesCtx: messagesCtx,
		ctx:         ctx,
		opts:        tc.opts,
		stream:      tc.stream,
		extractCtx:  tc.extractContext,
	}, nil
}

// Next retrieves a single message with tracing.
//
// Deprecated: use NextContext to allow the receive span to nest under an ambient
// context.
func (tc *TracedConsumer) Next(opts ...jetstream.FetchOpt) (*TracedMsg, error) {
	return tc.NextContext(context.Background(), opts...)
}

// NextContext retrieves a single message with tracing, starting the receive span
// as a child of ctx.
//
// The returned message's context descends from the receive span: when the
// message carries no propagated trace headers it is parented to the receive
// span (matching Fetch's behavior) rather than being a root.
func (tc *TracedConsumer) NextContext(ctx context.Context, opts ...jetstream.FetchOpt) (*TracedMsg, error) {
	spanCtx, span := tc.startFetchSpanCtx(ctx)

	msg, err := tc.consumer.Next(opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return &TracedMsg{
		Msg: msg,
		ctx: tc.extractContext(spanCtx, msg),
	}, nil
}

// Consume starts consuming messages with the provided handler.
// For traced handlers, use MessageHandlerWithTracing instead.
func (tc *TracedConsumer) Consume(
	handler jetstream.MessageHandler,
	opts ...jetstream.PullConsumeOpt,
) (jetstream.ConsumeContext, error) {
	return tc.consumer.Consume(handler, opts...)
}

// extractContext extracts trace context from a message's headers.
func (tc *TracedConsumer) extractContext(ctx context.Context, msg jetstream.Msg) context.Context {
	if msg == nil {
		return ctx
	}

	headers := msg.Headers()
	if headers == nil {
		return ctx
	}

	return tc.prop.Extract(ctx, headerCarrier(headers))
}
