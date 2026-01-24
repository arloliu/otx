# OpenTelemetry Semantic Conventions

OTX follows the [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/) to ensure interoperability and consistent observability across services.

## Why Semantic Conventions Matter

Semantic conventions provide:
- **Interoperability**: Tools and backends understand your telemetry
- **Consistency**: Uniform attribute names across services
- **Discoverability**: Standard queries work across your fleet
- **Future-proofing**: Compatibility with evolving OTel ecosystem

## Resource Attributes

Resources identify the entity producing telemetry. OTX automatically sets:

| Attribute | Source | Example |
|-----------|--------|---------|
| `service.name` | `ServiceName` config | `"order-service"` |
| `service.version` | `Version` config | `"1.2.3"` |
| `deployment.environment` | `Environment` config | `"production"` |

**Reference**: [Service Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/resource/#service)

### Best Practices

```yaml
serviceName: "order-service"      # Use lowercase, hyphen-separated
version: "1.2.3"                  # Use semantic versioning
environment: "production"         # production, staging, development
resourceAttributes:
  service.namespace: "ecommerce"  # Group related services
  service.instance.id: "pod-123" # Unique instance identifier
```

## Span Naming Conventions

Span names should be **low cardinality** - avoid including variable data like IDs.

### Industry Standards from Popular Projects

These conventions are based on analysis of Kubernetes, OpenTelemetry Contrib, gRPC-Go, Jaeger, and go-kit:

| Category | Convention | Real-World Examples |
|----------|------------|---------------------|
| **HTTP Server** | `METHOD /route` | `GET /api/v1/namespaces/{:namespace}/pods/{:name}` (Kubernetes) |
| **HTTP Client** | `METHOD` or `METHOD /path` | `GET`, `POST /users` (go-kit, otelhttp) |
| **gRPC Server** | `Recv.package.Service.Method` | `Recv.grpc.testing.TestService.UnaryCall` (gRPC-Go) |
| **gRPC Client** | `Sent.package.Service.Method` | `Sent.grpc.testing.TestService.FullDuplexCall` (gRPC-Go) |
| **Database** | `operation table` | `SELECT users`, `findTraceIDs` (Jaeger) |
| **MongoDB** | `collection.operation` | `users.find`, `orders.insertOne` (OTel Contrib) |
| **AWS SDK** | `ServiceID.Operation` | `S3.ListBuckets`, `DynamoDB.GetItem` (OTel Contrib) |
| **Messaging** | `operation destination` | `publish orders`, `receive ORDERS` |
| **Internal** | Descriptive name | `ProcessOrder`, `validate.input`, `queryByService` |

### General Spans

The OpenTelemetry spec does not mandate a specific case for internal span names. The key requirement is **low cardinality** (no dynamic IDs in names). Use whatever naming style is consistent with your codebase.

| Pattern | Example | Notes |
|---------|---------|-------|
| `verb object` | `"process order"` | Lowercase, follows messaging convention style |
| `VerbObject` | `"ProcessOrder"` | PascalCase if matching Go function names |
| `component.operation` | `"cache.get"` | Dotted lowercase for component context |

### HTTP Spans

**Reference**: [HTTP Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/http/)

```go
// ✅ Good: Low cardinality
spanName := otx.NameHTTP("GET", "/users/{id}")

// ❌ Bad: High cardinality (includes user ID)
spanName := "GET /users/12345"
```

Use the route template, not the actual path:

| Method | Route Template | Span Name |
|--------|----------------|-----------|
| GET | `/users/{id}` | `"GET /users/{id}"` |
| POST | `/orders` | `"POST /orders"` |
| DELETE | `/items/{itemId}` | `"DELETE /items/{itemId}"` |

### RPC/gRPC Spans

**Reference**: [RPC Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/rpc/)

Based on gRPC-Go's official instrumentation, use prefixes for directionality:

```go
// Standard format: "package.Service/Method"
spanName := otx.NameRPC("OrderService", "CreateOrder")
// Result: "OrderService/CreateOrder"

// gRPC-Go style with direction prefixes:
// Client spans: "Sent.package.Service.Method"
// Server spans: "Recv.package.Service.Method"
// Attempt spans: "Attempt.package.Service.Method"
```

| Direction | Format | Example |
|-----------|--------|--------|
| Server (incoming) | `Recv.package.Service.Method` | `Recv.myapp.UserService.GetUser` |
| Client (outgoing) | `Sent.package.Service.Method` | `Sent.payment.PaymentService.Charge` |
| Retry attempt | `Attempt.package.Service.Method` | `Attempt.myapp.UserService.GetUser` |

### Database Spans

**Reference**: [Database Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/database/)

```go
// SQL format: "OPERATION table"
spanName := otx.NameDB("SELECT", "users")
// Result: "SELECT users"
```

Based on Jaeger and OTel Contrib patterns:

| Database | Convention | Examples |
|----------|------------|----------|
| **SQL** | `OPERATION table` | `SELECT users`, `INSERT orders`, `UPDATE products` |
| **MongoDB** | `collection.operation` | `users.find`, `orders.insertOne`, `products.aggregate` |
| **Redis** | `command` | `GET`, `SET`, `HGETALL` |
| **Elasticsearch** | `operation index` | `search traces`, `index spans` |
| **Cassandra** | Descriptive name | `queryByService`, `readTrace`, `findTraceIDs` (Jaeger) |

### Messaging Spans

**Reference**: [Messaging Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/messaging/)

```go
// Format: "operation destination"
spanName := otx.NameMessaging("publish", "orders")
// Result: "publish orders"
```

OTX NATS wrappers automatically use these conventions:
- `"publish {subject}"` for producers
- `"receive {stream}"` for consumers
- `"process {stream}"` for message handlers

## Span Kinds

Choose the correct span kind for accurate service maps:

| Kind | Use Case | OTX Helper |
|------|----------|------------|
| `Internal` | Local operations, business logic | `otx.Start()`, `otx.StartInternal()` |
| `Server` | Handling incoming requests | `otx.StartServer()` |
| `Client` | Making outgoing requests | `otx.StartClient()` |
| `Producer` | Publishing messages to a queue | `otx.StartProducer()` |
| `Consumer` | Processing messages from a queue | `otx.StartConsumer()` |

```go
// Handling HTTP request
ctx, span := otx.StartServer(ctx, "HandleCreateOrder")

// Calling external API
ctx, span := otx.StartClient(ctx, "FetchUserProfile")

// Publishing to NATS
ctx, span := otx.StartProducer(ctx, "publish orders.created")

// Processing from queue
ctx, span := otx.StartConsumer(ctx, "process ORDERS")
```

## Standard Attributes

### HTTP Attributes

```go
import "go.opentelemetry.io/otel/semconv/v1.24.0"

otx.SetAttributes(ctx,
    semconv.HTTPRequestMethodKey.String("POST"),
    semconv.URLPath("/api/orders"),
    semconv.HTTPResponseStatusCode(201),
    semconv.ServerAddress("api.example.com"),
)
```

### Database Attributes

```go
otx.SetAttributes(ctx,
    semconv.DBSystemPostgreSQL,
    semconv.DBName("orders"),
    semconv.DBOperation("SELECT"),
    semconv.DBSQLTable("orders"),
)
```

### Messaging Attributes

```go
otx.SetAttributes(ctx,
    semconv.MessagingSystem("nats"),
    semconv.MessagingDestinationName("orders.created"),
    semconv.MessagingOperationPublish,
    semconv.MessagingMessageBodySize(1024),
)
```

### Custom Attributes

For domain-specific attributes, use a namespace prefix:

```go
otx.SetAttributes(ctx,
    attribute.String("order.id", orderID),
    attribute.String("order.status", "pending"),
    attribute.Int("order.items_count", len(items)),
    attribute.Float64("order.total", 99.99),
)
```

## Error Handling

**Reference**: [Span Status](https://opentelemetry.io/docs/specs/otel/trace/api/#set-status)

```go
func ProcessOrder(ctx context.Context, orderID string) error {
    ctx, span := otx.Start(ctx, "ProcessOrder")
    defer span.End()

    if err := validate(orderID); err != nil {
        // Records error event and sets status to Error
        otx.RecordError(ctx, err)
        return err
    }

    // Explicitly mark success
    otx.SetSuccess(ctx)
    return nil
}
```

### Exception Attributes

When recording errors, OTX automatically adds:
- `exception.type`: Error type name
- `exception.message`: Error message

## Events

Add events for significant occurrences within a span:

```go
otx.AddEvent(ctx, "order.validated",
    attribute.String("validation.type", "schema"),
)

otx.AddEvent(ctx, "payment.processed",
    attribute.String("payment.method", "credit_card"),
    attribute.Float64("payment.amount", 99.99),
)
```

## Baggage

Use baggage for cross-service context propagation:

```go
// Set baggage (propagates across services)
ctx = otx.MustSetBaggage(ctx, "tenant.id", tenantID)
ctx = otx.MustSetBaggage(ctx, "user.id", userID)

// Read baggage (in downstream service)
tenantID := otx.GetBaggage(ctx, "tenant.id")
```

**Best Practices**:
- Use for cross-cutting concerns (tenant ID, user ID, request ID)
- Keep values small (they travel in HTTP headers)
- Use namespaced keys (`tenant.id`, not just `id`)
- Don't use for sensitive data (baggage is not encrypted)

## Span Naming Anti-Patterns

Avoid these common mistakes:

| ❌ Bad | ✅ Good | Why |
|--------|---------|-----|
| `GET /users/12345` | `GET /users/{id}` | High cardinality from dynamic ID |
| `query` | `queryByService` | Too vague, not descriptive |
| `handleRequest` | `POST /api/orders` | Missing HTTP method and route |
| `db operation` | `SELECT users` | Too generic |
| `/api/v1/orders/abc123/items/456` | `GET /api/v1/orders/{orderId}/items/{itemId}` | Use route templates |
| `process` | `process.message` or `ProcessOrder` | Too short, add context |

## Checklist

Before deploying, verify:

- [ ] Service name follows `lowercase-hyphen` convention
- [ ] Span names are low cardinality (no IDs in names)
- [ ] HTTP spans include method: `METHOD /route`
- [ ] gRPC spans use `package.Service/Method` format
- [ ] Database spans identify the operation and table
- [ ] Span kinds match the operation type
- [ ] Errors are recorded with `otx.RecordError()`
- [ ] Success is marked with `otx.SetSuccess()`
- [ ] Attributes use semantic convention names where applicable
- [ ] Custom attributes use namespaced keys
- [ ] Baggage keys are namespaced

## References

- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)
- [Trace Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/general/trace/)
- [HTTP Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/http/)
- [Database Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/database/)
- [Messaging Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/messaging/)
- [RPC Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/rpc/)
