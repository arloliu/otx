package nats

import (
	"go.opentelemetry.io/otel/attribute"
)

// Messaging system identifier for NATS.
const messagingSystem = "nats"

// Attribute keys following OTel messaging semantic conventions.
const (
	attrMessagingSystem          = "messaging.system"
	attrMessagingOperationName   = "messaging.operation.name"
	attrMessagingOperationType   = "messaging.operation.type"
	attrMessagingDestinationName = "messaging.destination.name"
	attrMessagingConsumerGroup   = "messaging.consumer.group.name"
	attrMessagingMessageID       = "messaging.message.id"
	attrMessagingMessageBodySize = "messaging.message.body.size"
	attrNATSStream               = "nats.stream"
)

// Operation types per OTel messaging semantic conventions.
const (
	opTypePublish = "publish"
	opTypeReceive = "receive"
	opTypeProcess = "process"
	opTypeSend    = "send"
)

// publishAttributes returns attributes for a publish operation span.
func publishAttributes(subject string, msgID string, bodySize int) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 6)

	attrs = append(attrs,
		attribute.String(attrMessagingSystem, messagingSystem),
		attribute.String(attrMessagingOperationName, opTypePublish),
		attribute.String(attrMessagingOperationType, opTypeSend),
		attribute.String(attrMessagingDestinationName, subject),
	)

	if msgID != "" {
		attrs = append(attrs, attribute.String(attrMessagingMessageID, msgID))
	}

	if bodySize > 0 {
		attrs = append(attrs, attribute.Int(attrMessagingMessageBodySize, bodySize))
	}

	return attrs
}

// receiveAttributes returns attributes for a receive/fetch operation span.
func receiveAttributes(stream, consumerName string, bodySize int) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 6)

	attrs = append(attrs,
		attribute.String(attrMessagingSystem, messagingSystem),
		attribute.String(attrMessagingOperationName, opTypeReceive),
		attribute.String(attrMessagingOperationType, opTypeReceive),
		attribute.String(attrNATSStream, stream),
	)

	if consumerName != "" {
		attrs = append(attrs, attribute.String(attrMessagingConsumerGroup, consumerName))
	}

	if bodySize > 0 {
		attrs = append(attrs, attribute.Int(attrMessagingMessageBodySize, bodySize))
	}

	return attrs
}

// processAttributes returns attributes for a message processing span.
func processAttributes(stream, consumerName, subject, msgID string, bodySize int) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 8)

	attrs = append(attrs,
		attribute.String(attrMessagingSystem, messagingSystem),
		attribute.String(attrMessagingOperationName, opTypeProcess),
		attribute.String(attrMessagingOperationType, opTypeProcess),
		attribute.String(attrNATSStream, stream),
	)

	if subject != "" {
		attrs = append(attrs, attribute.String(attrMessagingDestinationName, subject))
	}

	if consumerName != "" {
		attrs = append(attrs, attribute.String(attrMessagingConsumerGroup, consumerName))
	}

	if msgID != "" {
		attrs = append(attrs, attribute.String(attrMessagingMessageID, msgID))
	}

	if bodySize > 0 {
		attrs = append(attrs, attribute.Int(attrMessagingMessageBodySize, bodySize))
	}

	return attrs
}
