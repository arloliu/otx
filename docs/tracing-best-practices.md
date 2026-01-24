# Tracing Best Practices

This guide covers patterns and anti-patterns for effective distributed tracing with OTX.

## Context Propagation

Context is the backbone of distributed tracing. Breaking the context chain breaks your traces.

### ✅ Always Pass Context

```go
func ProcessOrder(ctx context.Context, order Order) error {
    ctx, span := otx.Start(ctx, "ProcessOrder")
    defer span.End()

    // Pass ctx to all downstream calls
    if err := s.validateOrder(ctx, order); err != nil {
        return err
    }

    return s.saveOrder(ctx, order)
}
```

### ❌ Never Use Background Context Mid-Flow

```go
func ProcessOrder(ctx context.Context, order Order) error {
    ctx, span := otx.Start(ctx, "ProcessOrder")
    defer span.End()

    // ❌ WRONG: This breaks the trace!
    go s.sendNotification(context.Background(), order)

    return nil
}
```

### ✅ Correct Async Pattern

```go
func ProcessOrder(ctx context.Context, order Order) error {
    ctx, span := otx.Start(ctx, "ProcessOrder")
    defer span.End()

    // ✅ Correct: Create a linked span for async work
    go func() {
        // Create new root span linked to parent
        asyncCtx, asyncSpan := otx.StartInternal(context.Background(), "SendNotification",
            trace.WithLinks(trace.LinkFromContext(ctx)),
        )
        defer asyncSpan.End()

        s.sendNotification(asyncCtx, order)
    }()

    return nil
}
```

## Span Lifecycle

### Always Defer End()

```go
func DoWork(ctx context.Context) error {
    ctx, span := otx.Start(ctx, "DoWork")
    defer span.End()  // ✅ Guaranteed to run

    // Even if this panics, span.End() is called
    return riskyOperation(ctx)
}
```

### Avoid Long-Running Spans

```go
// ❌ Bad: Span lives for entire server lifetime
func StartServer(ctx context.Context) {
    ctx, span := otx.Start(ctx, "Server")
    defer span.End()

    http.ListenAndServe(":8080", handler)
}

// ✅ Good: Create spans per-request
func HandleRequest(w http.ResponseWriter, r *http.Request) {
    ctx, span := otx.StartServer(r.Context(), "HandleRequest")
    defer span.End()

    // Process request
}
```

## Span Granularity

### Right Level of Detail

```go
// ❌ Too granular: Creates noise
func ProcessItems(ctx context.Context, items []Item) {
    for _, item := range items {
        ctx, span := otx.Start(ctx, "ProcessItem")
        processItem(item)
        span.End()
    }
}

// ✅ Better: One span with attributes
func ProcessItems(ctx context.Context, items []Item) {
    ctx, span := otx.Start(ctx, "ProcessItems")
    defer span.End()

    otx.SetAttributes(ctx, attribute.Int("items.count", len(items)))

    for _, item := range items {
        processItem(ctx, item)
    }
}

// ✅ Also good: Events for significant items
func ProcessItems(ctx context.Context, items []Item) {
    ctx, span := otx.Start(ctx, "ProcessItems")
    defer span.End()

    for _, item := range items {
        if item.Priority == "high" {
            otx.AddEvent(ctx, "processing.high_priority",
                attribute.String("item.id", item.ID),
            )
        }
        processItem(ctx, item)
    }
}
```

### Trace Meaningful Boundaries

Create spans at:
- Service entry points (HTTP handlers, gRPC methods)
- External calls (databases, APIs, queues)
- Significant business operations
- Long-running operations

Skip spans for:
- Simple utility functions
- Getters/setters
- Pure computations under 1ms

## Error Handling

### Record Errors Properly

```go
func ProcessOrder(ctx context.Context, orderID string) error {
    ctx, span := otx.Start(ctx, "ProcessOrder")
    defer span.End()

    order, err := s.repo.GetOrder(ctx, orderID)
    if err != nil {
        if errors.Is(err, ErrNotFound) {
            // Business error: Set attributes, not error status
            otx.SetAttributes(ctx, attribute.Bool("order.not_found", true))
            return err
        }
        // Infrastructure error: Record as error
        otx.RecordError(ctx, err)
        return err
    }

    // Success path
    otx.SetSuccess(ctx)
    return nil
}
```

### Distinguish Error Types

| Error Type | Action | Example |
|------------|--------|---------|
| Infrastructure | `RecordError()` | DB connection failed |
| Validation | Attributes | Invalid input format |
| Business logic | Attributes | Order already exists |
| Not found | Attributes | Resource doesn't exist |

## Attributes

### Use Semantic Conventions

```go
import semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

// ✅ Good: Standard attribute names
otx.SetAttributes(ctx,
    semconv.HTTPRequestMethodKey.String("POST"),
    semconv.HTTPResponseStatusCode(201),
)

// ❌ Bad: Custom names for standard concepts
otx.SetAttributes(ctx,
    attribute.String("method", "POST"),
    attribute.Int("status", 201),
)
```

### Namespace Custom Attributes

```go
// ✅ Good: Namespaced
otx.SetAttributes(ctx,
    attribute.String("order.id", orderID),
    attribute.String("order.status", "pending"),
    attribute.String("customer.tier", "premium"),
)

// ❌ Bad: Ambiguous
otx.SetAttributes(ctx,
    attribute.String("id", orderID),
    attribute.String("status", "pending"),
)
```

### Avoid High-Cardinality Attributes

```go
// ❌ Bad: Unbounded cardinality
otx.SetAttributes(ctx,
    attribute.String("user.email", email),
    attribute.String("request.body", body),
    attribute.String("timestamp", time.Now().String()),
)

// ✅ Good: Bounded cardinality
otx.SetAttributes(ctx,
    attribute.String("user.tier", "premium"),  // Limited values
    attribute.Int("request.size", len(body)),  // Numeric
    attribute.String("user.region", "us-east"), // Enumerated
)
```

## Performance Considerations

### Check Sampling Before Expensive Operations

```go
func ProcessData(ctx context.Context, data LargeData) {
    ctx, span := otx.Start(ctx, "ProcessData")
    defer span.End()

    // Only compute expensive attributes if span is recording
    if span.IsRecording() {
        checksum := computeExpensiveChecksum(data)
        otx.SetAttributes(ctx, attribute.String("data.checksum", checksum))
    }

    // Always process
    process(data)
}
```

### Batch Attribute Setting

```go
// ✅ Good: Single call with multiple attributes
otx.SetAttributes(ctx,
    attribute.String("order.id", orderID),
    attribute.String("order.status", status),
    attribute.Int("order.items", itemCount),
    attribute.Float64("order.total", total),
)

// ❌ Less efficient: Multiple calls
otx.SetAttributes(ctx, attribute.String("order.id", orderID))
otx.SetAttributes(ctx, attribute.String("order.status", status))
otx.SetAttributes(ctx, attribute.Int("order.items", itemCount))
```

## Testing

### Use In-Memory Exporter

```go
func TestProcessOrder(t *testing.T) {
    // Setup
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
    otx.InitTracing(tp.Tracer("test"), otx.DefaultNamer{})

    // Execute
    ctx := context.Background()
    err := ProcessOrder(ctx, "order-123")

    // Assert spans
    spans := exporter.GetSpans()
    require.Len(t, spans, 1)
    assert.Equal(t, "ProcessOrder", spans[0].Name)
    assert.Equal(t, codes.Ok, spans[0].Status.Code)
}
```

## Checklist

Before each release:

- [ ] All service entry points have spans
- [ ] External calls (DB, HTTP, gRPC) are traced
- [ ] Context is propagated to all goroutines
- [ ] Errors are properly recorded
- [ ] Success is explicitly marked
- [ ] Span names are low cardinality
- [ ] Attributes use semantic conventions
- [ ] No sensitive data in spans or attributes
- [ ] Sampling is configured for production load

## References

- [Semantic Conventions](semantic-conventions.md)
- [OpenTelemetry Tracing Specification](https://opentelemetry.io/docs/specs/otel/trace/)
- [Context Propagation](https://opentelemetry.io/docs/specs/otel/context/)
