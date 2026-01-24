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

func (tc *TracedConsumer) startFetchSpan() (context.Context, trace.Span) {
	consumerName := ""
	if info := tc.consumer.CachedInfo(); info != nil {
		consumerName = info.Name
	}

	spanName := opTypeReceive + " " + tc.stream

	return tc.tracer.Start(context.Background(), spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(receiveAttributes(tc.stream, consumerName, 0)...),
	)
}

// wrapBatch wraps a MessageBatch with tracing support.
func (tc *TracedConsumer) wrapBatch(ctx context.Context, batch jetstream.MessageBatch) *TracedMessageBatch {
	return &TracedMessageBatch{
		batch:      batch,
		ctx:        ctx,
		opts:       tc.opts,
		stream:     tc.stream,
		extractCtx: tc.extractContext,
	}
}

// Fetch retrieves a batch of messages with tracing.
// Returns a TracedMessageBatch where each message has trace context extracted.
func (tc *TracedConsumer) Fetch(batch int, opts ...jetstream.FetchOpt) (*TracedMessageBatch, error) {
	ctx, span := tc.startFetchSpan()

	msgBatch, err := tc.consumer.Fetch(batch, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return tc.wrapBatch(ctx, msgBatch), nil
}

// FetchBytes retrieves messages up to maxBytes with tracing.
func (tc *TracedConsumer) FetchBytes(maxBytes int, opts ...jetstream.FetchOpt) (*TracedMessageBatch, error) {
	ctx, span := tc.startFetchSpan()

	msgBatch, err := tc.consumer.FetchBytes(maxBytes, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return tc.wrapBatch(ctx, msgBatch), nil
}

// FetchNoWait retrieves available messages without waiting.
func (tc *TracedConsumer) FetchNoWait(batch int) (*TracedMessageBatch, error) {
	ctx, span := tc.startFetchSpan()

	msgBatch, err := tc.consumer.FetchNoWait(batch)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		return nil, err
	}

	span.End()

	return tc.wrapBatch(ctx, msgBatch), nil
}

// Messages returns an iterator for continuous message consumption with tracing.
func (tc *TracedConsumer) Messages(opts ...jetstream.PullMessagesOpt) (*TracedMessagesContext, error) {
	messagesCtx, err := tc.consumer.Messages(opts...)
	if err != nil {
		return nil, err
	}

	return &TracedMessagesContext{
		messagesCtx: messagesCtx,
		ctx:         context.Background(),
		opts:        tc.opts,
		stream:      tc.stream,
		extractCtx:  tc.extractContext,
	}, nil
}

// Next retrieves a single message with tracing.
func (tc *TracedConsumer) Next(opts ...jetstream.FetchOpt) (*TracedMsg, error) {
	_, span := tc.startFetchSpan()

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
		ctx: tc.extractContext(context.Background(), msg),
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
