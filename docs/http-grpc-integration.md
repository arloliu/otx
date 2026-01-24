# HTTP and gRPC Integration

OTX provides middleware for HTTP and gRPC that automatically:
- Creates spans for incoming/outgoing requests
- Propagates trace context via headers
- Records standard semantic convention attributes

## HTTP Server

### Basic Middleware

```go
import (
    "net/http"
    otxhttp "github.com/arloliu/otx/http"
)

func main() {
    // Setup OTX providers first...

    mux := http.NewServeMux()
    mux.HandleFunc("/api/orders", handleOrders)

    // Wrap with tracing middleware
    handler := otxhttp.Middleware()(mux)

    http.ListenAndServe(":8080", handler)
}
```

### Handler Wrapper

For individual handlers with custom operation names:

```go
mux.Handle("/api/orders", otxhttp.Handler(
    http.HandlerFunc(handleOrders),
    "orders.list",  // Custom operation name
))
```

### With Explicit Providers

For testing or multi-tenant scenarios:

```go
handler := otxhttp.MiddlewareWithProviders(
    tracerProvider,
    meterProvider,
    propagator,
)(mux)
```

## HTTP Client

### Basic Client

```go
import otxhttp "github.com/arloliu/otx/http"

// Create traced HTTP client
client := otxhttp.NewClient(
    otxhttp.WithTimeout(30 * time.Second),
    otxhttp.WithMaxIdleConnsPerHost(10),
)

// Use like standard http.Client
resp, err := client.Get("https://api.example.com/users")
```

### Client Options

```go
client := otxhttp.NewClient(
    otxhttp.WithTimeout(30 * time.Second),
    otxhttp.WithDialTimeout(5 * time.Second),
    otxhttp.WithTLSHandshakeTimeout(5 * time.Second),
    otxhttp.WithResponseHeaderTimeout(10 * time.Second),
    otxhttp.WithMaxIdleConns(100),
    otxhttp.WithMaxIdleConnsPerHost(10),
    otxhttp.WithMaxConnsPerHost(100),
    otxhttp.WithIdleConnTimeout(90 * time.Second),
)
```

### Wrap Existing Transport

```go
client := &http.Client{
    Transport: otxhttp.Transport(http.DefaultTransport),
    Timeout:   30 * time.Second,
}
```

### With Explicit Providers

```go
client := otxhttp.NewClientWithProviders(
    tracerProvider,
    meterProvider,
    propagator,
    otxhttp.WithTimeout(30 * time.Second),
)
```

## gRPC Server

### Basic Setup

```go
import (
    "google.golang.org/grpc"
    otxgrpc "github.com/arloliu/otx/grpc"
)

func main() {
    // Setup OTX providers first...

    srv := grpc.NewServer(
        grpc.StatsHandler(otxgrpc.ServerHandler()),
    )

    // Register your services
    pb.RegisterOrderServiceServer(srv, &orderServer{})

    srv.Serve(listener)
}
```

### With Explicit Providers

```go
srv := grpc.NewServer(
    grpc.StatsHandler(otxgrpc.ServerHandlerWithProviders(
        tracerProvider,
        meterProvider,
        propagator,
    )),
)
```

## gRPC Client

### Basic Setup

```go
conn, err := grpc.NewClient(
    "order-service:50051",
    grpc.WithStatsHandler(otxgrpc.ClientHandler()),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := pb.NewOrderServiceClient(conn)
```

### With Explicit Providers

```go
conn, err := grpc.NewClient(
    "order-service:50051",
    grpc.WithStatsHandler(otxgrpc.ClientHandlerWithProviders(
        tracerProvider,
        meterProvider,
        propagator,
    )),
)
```

## Context Propagation

### HTTP Headers

OTX automatically propagates trace context via W3C headers:

```
traceparent: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
tracestate: key=value
baggage: tenant.id=abc123
```

### Manual Injection/Extraction

For custom scenarios:

```go
// Inject into HTTP headers
headers := make(http.Header)
otx.InjectHTTP(ctx, headers)

// Extract from HTTP headers
ctx = otx.ExtractHTTP(ctx, req.Header)

// Inject into gRPC metadata
md := metadata.New(nil)
otx.InjectGRPC(ctx, md)

// Extract from gRPC metadata
md, _ := metadata.FromIncomingContext(ctx)
ctx = otx.ExtractGRPC(ctx, md)
```

## Semantic Conventions

The middleware automatically sets [HTTP semantic convention](https://opentelemetry.io/docs/specs/semconv/http/) attributes:

### Server Spans

| Attribute | Example |
|-----------|---------|
| `http.request.method` | `"GET"` |
| `url.path` | `"/api/orders"` |
| `url.scheme` | `"https"` |
| `http.response.status_code` | `200` |
| `server.address` | `"api.example.com"` |
| `server.port` | `443` |

### Client Spans

| Attribute | Example |
|-----------|---------|
| `http.request.method` | `"POST"` |
| `url.full` | `"https://api.example.com/orders"` |
| `http.response.status_code` | `201` |
| `server.address` | `"api.example.com"` |

## Best Practices

### 1. Use Route Templates for Span Names

```go
// ✅ Good: Low cardinality
mux.Handle("/users/{id}", otxhttp.Handler(handler, "GET /users/{id}"))

// ❌ Bad: High cardinality (different span name per user)
// Default behavior includes full path
```

### 2. Add Business Context

```go
func handleOrder(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Add business-specific attributes
    orderID := chi.URLParam(r, "orderID")
    otx.SetAttributes(ctx,
        attribute.String("order.id", orderID),
    )

    // Process order...
}
```

### 3. Handle Errors Properly

```go
func handleOrder(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    order, err := s.GetOrder(ctx, orderID)
    if err != nil {
        otx.RecordError(ctx, err)
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }

    otx.SetSuccess(ctx)
    json.NewEncoder(w).Encode(order)
}
```

### 4. Use Explicit Providers in Tests

```go
func TestHandler(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(trace.WithSyncer(exporter))

    handler := otxhttp.MiddlewareWithProviders(tp, nil, nil)(myHandler)

    // Test with httptest
    req := httptest.NewRequest("GET", "/api/orders", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    // Assert spans
    spans := exporter.GetSpans()
    require.Len(t, spans, 1)
}
```

## References

- [HTTP Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/http/)
- [RPC Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/rpc/)
- [gRPC Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/rpc/grpc/)
