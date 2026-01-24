# Testing with OTX

This guide covers strategies for testing code that uses OTX tracing.

## In-Memory Exporter

The OpenTelemetry SDK provides an in-memory exporter for testing:

```go
import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"

    "github.com/arloliu/otx"
)

func TestProcessOrder(t *testing.T) {
    // Setup in-memory exporter
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

    // Initialize OTX with test provider
    otx.InitTracing(tp.Tracer("test"), otx.DefaultNamer{})

    // Run code under test
    ctx := context.Background()
    err := ProcessOrder(ctx, "order-123")
    require.NoError(t, err)

    // Get recorded spans
    spans := exporter.GetSpans()

    // Assert span count
    require.Len(t, spans, 1)

    // Assert span properties
    span := spans[0]
    assert.Equal(t, "ProcessOrder", span.Name)
    assert.Equal(t, codes.Ok, span.Status.Code)
}
```

## Asserting Span Properties

### Span Name and Kind

```go
span := spans[0]
assert.Equal(t, "ProcessOrder", span.Name)
assert.Equal(t, trace.SpanKindInternal, span.SpanKind)
```

### Span Status

```go
// Success
assert.Equal(t, codes.Ok, span.Status.Code)

// Error
assert.Equal(t, codes.Error, span.Status.Code)
assert.Contains(t, span.Status.Description, "expected error")
```

### Attributes

```go
func findAttribute(span tracetest.SpanStub, key string) (attribute.Value, bool) {
    for _, attr := range span.Attributes {
        if string(attr.Key) == key {
            return attr.Value, true
        }
    }
    return attribute.Value{}, false
}

func TestOrderAttributes(t *testing.T) {
    // ... setup and run ...

    span := spans[0]

    // Check string attribute
    val, ok := findAttribute(span, "order.id")
    require.True(t, ok)
    assert.Equal(t, "order-123", val.AsString())

    // Check int attribute
    val, ok = findAttribute(span, "order.items")
    require.True(t, ok)
    assert.Equal(t, int64(5), val.AsInt64())
}
```

### Events

```go
func TestOrderEvents(t *testing.T) {
    // ... setup and run ...

    span := spans[0]

    // Find event by name
    var foundEvent *trace.Event
    for _, event := range span.Events {
        if event.Name == "order.validated" {
            foundEvent = &event
            break
        }
    }

    require.NotNil(t, foundEvent)
    assert.NotZero(t, foundEvent.Time)
}
```

### Parent-Child Relationships

```go
func TestSpanHierarchy(t *testing.T) {
    // ... setup and run code that creates child spans ...

    spans := exporter.GetSpans()
    require.Len(t, spans, 2)

    // Find parent and child (spans are in order of ending)
    var parent, child tracetest.SpanStub
    for _, s := range spans {
        if s.Name == "ProcessOrder" {
            parent = s
        } else if s.Name == "ValidateOrder" {
            child = s
        }
    }

    // Assert parent-child relationship
    assert.Equal(t, parent.SpanContext.TraceID(), child.SpanContext.TraceID())
    assert.Equal(t, parent.SpanContext.SpanID(), child.Parent.SpanID())
}
```

## Testing HTTP Handlers

```go
import (
    "net/http/httptest"

    otxhttp "github.com/arloliu/otx/http"
)

func TestHTTPHandler(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

    // Create handler with test provider
    handler := otxhttp.MiddlewareWithProviders(tp, nil, nil)(
        http.HandlerFunc(myHandler),
    )

    // Create test request
    req := httptest.NewRequest("GET", "/api/orders", nil)
    rec := httptest.NewRecorder()

    // Execute
    handler.ServeHTTP(rec, req)

    // Assert response
    assert.Equal(t, http.StatusOK, rec.Code)

    // Assert spans
    spans := exporter.GetSpans()
    require.Len(t, spans, 1)
    assert.Equal(t, trace.SpanKindServer, spans[0].SpanKind)
}
```

## Testing NATS Handlers

```go
import otxnats "github.com/arloliu/otx/nats"

func TestNATSHandler(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

    var processedMsg *otxnats.TracedMsg

    handler := otxnats.MessageHandlerWithTracingProviders(
        func(msg *otxnats.TracedMsg) {
            processedMsg = msg
            msg.Ack()
        },
        tp,
        nil,
    )

    // Create mock message
    msg := &mockMsg{
        subject: "orders.created",
        data:    []byte(`{"id": "123"}`),
    }

    // Execute handler
    handler(msg)

    // Assert message was processed
    require.NotNil(t, processedMsg)

    // Assert spans
    spans := exporter.GetSpans()
    require.Len(t, spans, 1)
    assert.Equal(t, trace.SpanKindConsumer, spans[0].SpanKind)
}
```

## Testing Context Propagation

```go
func TestContextPropagation(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})

    // Create parent span
    ctx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")

    // Inject into headers
    headers := make(http.Header)
    otx.InjectHTTP(ctx, headers)

    parentSpan.End()

    // Simulate receiving in another service
    newCtx := otx.ExtractHTTP(context.Background(), headers)

    // Create child span
    _, childSpan := tp.Tracer("test").Start(newCtx, "child")
    childSpan.End()

    // Assert trace continuity
    spans := exporter.GetSpans()
    require.Len(t, spans, 2)

    parent := spans[0]
    child := spans[1]

    assert.Equal(t, parent.SpanContext.TraceID(), child.SpanContext.TraceID())
}
```

## Test Helpers

Create reusable test helpers:

```go
// testutil/tracing.go
package testutil

import (
    "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"

    "github.com/arloliu/otx"
)

type TracingFixture struct {
    Exporter *tracetest.InMemoryExporter
    Provider *trace.TracerProvider
}

func NewTracingFixture() *TracingFixture {
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

    otx.InitTracing(tp.Tracer("test"), otx.DefaultNamer{})

    return &TracingFixture{
        Exporter: exporter,
        Provider: tp,
    }
}

func (f *TracingFixture) Reset() {
    f.Exporter.Reset()
}

func (f *TracingFixture) Spans() tracetest.SpanStubs {
    return f.Exporter.GetSpans()
}

func (f *TracingFixture) SpanByName(name string) (tracetest.SpanStub, bool) {
    for _, span := range f.Exporter.GetSpans() {
        if span.Name == name {
            return span, true
        }
    }
    return tracetest.SpanStub{}, false
}
```

### Usage

```go
func TestWithFixture(t *testing.T) {
    fix := testutil.NewTracingFixture()
    defer fix.Reset()

    // Run test
    ProcessOrder(context.Background(), "123")

    // Assert
    span, ok := fix.SpanByName("ProcessOrder")
    require.True(t, ok)
    assert.Equal(t, codes.Ok, span.Status.Code)
}
```

## Testing Without Tracing

For unit tests that don't need tracing assertions:

```go
func TestBusinessLogic(t *testing.T) {
    // OTX gracefully handles nil tracer
    otx.InitTracing(nil, nil)

    // Or use noop provider
    ctx := context.Background()

    // Code works without tracing setup
    result := ProcessOrder(ctx, "123")
    assert.NotNil(t, result)
}
```

## Best Practices

1. **Reset exporter between tests** to avoid span pollution
2. **Use explicit providers** in HTTP/gRPC/NATS tests
3. **Assert span relationships** for distributed tracing tests
4. **Check error status** not just error events
5. **Create test helpers** for common assertions
6. **Test sampling behavior** if using ratio-based sampling

## References

- [OpenTelemetry Go Testing](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace/tracetest)
- [tracetest.SpanStub](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace/tracetest#SpanStub)
