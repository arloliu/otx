package nats

import (
	"context"
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Publisher wraps JetStream publish operations with OpenTelemetry tracing.
type Publisher struct {
	js     jetstream.JetStream
	tracer trace.Tracer
	prop   propagation.TextMapPropagator
	opts   options
}

// NewPublisher creates a Publisher with tracing using the global providers.
func NewPublisher(js jetstream.JetStream, opts ...Option) *Publisher {
	return NewPublisherWithProviders(js, nil, nil, opts...)
}

// NewPublisherWithProviders creates a Publisher with explicit providers.
// If tp is nil, the global TracerProvider is used.
// If prop is nil, the global TextMapPropagator is used (or opts.prop if set).
//
// Panics if js is nil.
func NewPublisherWithProviders(
	js jetstream.JetStream,
	tp trace.TracerProvider,
	prop propagation.TextMapPropagator,
	opts ...Option,
) *Publisher {
	if js == nil {
		panic("otx/nats: JetStream must not be nil")
	}
	o := applyOptions(opts)

	// Explicit prop parameter takes precedence over option
	if prop != nil {
		o.prop = prop
	}

	return &Publisher{
		js:     js,
		tracer: getTracer(tp, o),
		prop:   getPropagator(o),
		opts:   o,
	}
}

// JetStream returns the underlying JetStream client for non-traced operations.
func (p *Publisher) JetStream() jetstream.JetStream {
	return p.js
}

// Publish publishes a message with tracing.
// A producer span is created and trace context is injected into message headers.
func (p *Publisher) Publish(
	ctx context.Context,
	subject string,
	data []byte,
	opts ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(publishAttributes(subject, "", len(data))...),
	)
	defer span.End()

	// Create message with injected trace context
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}
	p.prop.Inject(ctx, headerCarrier(msg.Header))

	ack, err := p.js.PublishMsg(ctx, msg, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	// Add message ID from ack if available
	if ack != nil {
		span.SetAttributes(publishAttributes(subject, strconv.FormatUint(ack.Sequence, 10), 0)...)
	}

	return ack, nil
}

// PublishMsg publishes a message with tracing.
// If msg.Header is nil, it will be initialized before injecting trace context.
func (p *Publisher) PublishMsg(
	ctx context.Context,
	msg *nats.Msg,
	opts ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	subject := msg.Subject
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(publishAttributes(subject, "", len(msg.Data))...),
	)
	defer span.End()

	// Inject trace context
	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	p.prop.Inject(ctx, headerCarrier(msg.Header))

	ack, err := p.js.PublishMsg(ctx, msg, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	if ack != nil {
		span.SetAttributes(publishAttributes(subject, strconv.FormatUint(ack.Sequence, 10), 0)...)
	}

	return ack, nil
}

// PublishAsync publishes a message asynchronously with tracing.
// Uses context.Background() for span creation since async operations lack context.
// When WithAsyncSpans(false), no span is created and no headers are injected.
func (p *Publisher) PublishAsync(
	subject string,
	data []byte,
	opts ...jetstream.PublishOpt,
) (jetstream.PubAckFuture, error) {
	if !p.opts.asyncSpans {
		return p.js.PublishAsync(subject, data, opts...)
	}

	ctx := context.Background()
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(publishAttributes(subject, "", len(data))...),
	)
	// Note: span.End() is deferred here, not after future resolves
	// This captures the publish initiation, not the ack receipt
	defer span.End()

	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}
	p.prop.Inject(ctx, headerCarrier(msg.Header))

	future, err := p.js.PublishMsgAsync(msg, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	return future, nil
}

// PublishAsyncMsg publishes a message asynchronously with tracing.
// When WithAsyncSpans(false), no span is created and no headers are injected.
// If msg.Header is nil and async spans are enabled, it will be initialized.
func (p *Publisher) PublishAsyncMsg(
	msg *nats.Msg,
	opts ...jetstream.PublishOpt,
) (jetstream.PubAckFuture, error) {
	if !p.opts.asyncSpans {
		return p.js.PublishMsgAsync(msg, opts...)
	}

	ctx := context.Background()
	subject := msg.Subject
	spanName := opTypePublish + " " + subject

	ctx, span := p.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(publishAttributes(subject, "", len(msg.Data))...),
	)
	defer span.End()

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	p.prop.Inject(ctx, headerCarrier(msg.Header))

	future, err := p.js.PublishMsgAsync(msg, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	return future, nil
}

// Compile-time check that Publisher doesn't accidentally claim to implement JetStream.
var _ interface{ JetStream() jetstream.JetStream } = (*Publisher)(nil)
