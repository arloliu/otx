// Package nats provides OpenTelemetry instrumentation for NATS JetStream.
//
// This package provides wrappers for JetStream publish and consume operations
// that automatically create spans following OTel messaging semantic conventions.
//
// # Publisher Usage
//
// Wrap a JetStream client to add tracing to publish operations:
//
//	js, _ := jetstream.New(nc)
//	publisher := nats.NewPublisher(js)
//
//	// Traced publish - context propagated via headers
//	publisher.Publish(ctx, "orders.created", data)
//
// # Consumer Usage
//
// Wrap a Consumer to add tracing to consume operations:
//
//	consumer, _ := stream.CreateConsumer(ctx, cfg)
//	traced := nats.WrapConsumer(consumer, "ORDERS")
//
//	msgs, _ := traced.Fetch(10)
//	for msg := range msgs.Messages() {
//	    processOrder(msg.Context(), msg.Data())
//	    msg.Ack()
//	}
//	if err := msgs.Error(); err != nil {
//	    log.Error("fetch error", err)
//	}
//
// # Callback-Style Consumption
//
// Use MessageHandlerWithTracing for callback-style consumption:
//
//	consumer.Consume(nats.MessageHandlerWithTracing(func(msg *nats.TracedMsg) {
//	    processOrder(msg.Context(), msg.Data())
//	    msg.Ack()
//	}, nats.WithStream("ORDERS")))
//
// # Standalone Trace Extraction
//
// For applications that cannot fully adopt TracedConsumer but need to extract
// propagated trace context and create process spans, use NewTracedMsg with
// StartProcessSpan. The stream name is automatically extracted from message metadata:
//
//	consumer.Consume(func(msg jetstream.Msg) {
//	    tracedMsg := nats.NewTracedMsg(msg)
//	    ctx, endSpan := tracedMsg.StartProcessSpan()  // Stream auto-detected from metadata
//	    defer endSpan(nil)
//
//	    if err := processOrder(ctx, msg.Data()); err != nil {
//	        endSpan(err)  // Records error on span
//	        msg.Nak()
//	        return
//	    }
//	    msg.Ack()
//	})
//
// If the stream name cannot be auto-detected, use WithStream option:
//
//	ctx, endSpan := tracedMsg.StartProcessSpan(nats.WithStream("ORDERS"))
//
// For simpler cases where you only need the context (no process span):
//
//	tracedMsg := nats.NewTracedMsg(msg)
//	ctx := tracedMsg.Context()  // Contains extracted trace context
//
// # Semantic Conventions
//
// This package follows the OpenTelemetry messaging semantic conventions:
//   - Producer spans use kind PRODUCER with name "publish {subject}"
//   - Receive spans use kind CLIENT with name "receive {stream}"
//   - Process spans use kind CONSUMER with name "process {stream}"
//
// For more details, see https://opentelemetry.io/docs/specs/semconv/messaging/
package nats
