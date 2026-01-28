# OTX (OpenTelemetry Extensions)

[![Go Reference](https://pkg.go.dev/badge/github.com/arloliu/otx.svg)](https://pkg.go.dev/github.com/arloliu/otx)
[![Go Report Card](https://goreportcard.com/badge/github.com/arloliu/otx)](https://goreportcard.com/report/github.com/arloliu/otx)


The `otx` package provides a unified, configuration-driven wrapper around OpenTelemetry (OTel) for Go services. It simplifies the setup of tracing, logging, metrics, context propagation, and span management while enforcing consistent conventions.

## Features

- **🔧 Unified Configuration** - Single config struct for traces, logs, and metrics with YAML/env var support
- **🚀 Zero-Boilerplate Setup** - One-line provider initialization with sensible defaults
- **📡 Multi-Protocol Export** - OTLP over gRPC or HTTP, with console/stdout options for development
- **🔗 Automatic Context Propagation** - W3C TraceContext and Baggage propagation out of the box
- **🌐 HTTP/gRPC Middleware** - Drop-in middleware for automatic request tracing
- **📨 NATS JetStream Integration** - Publisher and consumer wrappers with trace context injection
- **🏷️ Semantic Conventions** - Built-in helpers following OpenTelemetry naming standards
- **🎯 Span Kind Helpers** - `StartServer`, `StartClient`, `StartProducer`, `StartConsumer` for accurate service maps
- **⚡ Baggage Utilities** - Simple API for cross-service context propagation
- **🛡️ Graceful Shutdown** - Proper lifecycle management with flush-on-shutdown
- **🧪 Testing Support** - Noop providers and test utilities for unit testing
- **📊 Backend Agnostic** - Works with Jaeger, Zipkin, Datadog, Grafana Tempo, Honeycomb, and more

## Documentation

| Guide | Description |
|-------|-------------|
| [Getting Started](docs/getting-started.md) | Quick 5-minute setup guide |
| [Configuration](docs/configuration.md) | Complete configuration reference |
| [Semantic Conventions](docs/semantic-conventions.md) | OpenTelemetry standards and best practices |
| [Tracing Best Practices](docs/tracing-best-practices.md) | Patterns for effective tracing |
| [HTTP/gRPC Integration](docs/http-grpc-integration.md) | Middleware setup and usage |
| [NATS Integration](docs/nats-integration.md) | JetStream publisher/consumer tracing |
| [Testing](docs/testing.md) | Testing strategies with OTX |
| [Troubleshooting](docs/troubleshooting.md) | Common issues and solutions |
| [OTLP Simulator CLI](docs/otlp-sim.md) | CLI tool for simulating traces and logs |

## Core Concepts

-   **Zero-Config Defaults**: Works out of the box with sensible defaults (noop if disabled).
-   **Service Identity**: Uses the configured `ServiceName` to identify the source of traces.
-   **Unified Transport**: Supports OTLP (gRPC/HTTP) for exporting traces to backends like Jaeger, Datadog, or Grafana Tempo.
-   **Context Propagation**: Automatically handles W3C TraceContext and Baggage across process boundaries.
-   **Framework Agnostic**: No dependency on any DI framework; integrates easily with fx, wire, or plain Go.

## Installation & Setup

### 1. Configuration

Add the `TelemetryConfig` to your application's config struct:

```go
type AppConfig struct {
    Telemetry *otx.TelemetryConfig `yaml:"telemetry"`
}
```

Example `config.yaml`:
```yaml
telemetry:
  enabled: true
  serviceName: "my-service"  # OTEL_SERVICE_NAME
  environment: "production"  # OTEL_DEPLOYMENT_ENVIRONMENT
  exporter:
    type: "otlp"  # OTEL_TRACES_EXPORTER
    endpoint: "localhost:4317"  # OTEL_EXPORTER_OTLP_ENDPOINT
    protocol: "grpc"  # OTEL_EXPORTER_OTLP_PROTOCOL
  sampling:
    sampler: "parentbased_traceidratio"  # OTEL_TRACES_SAMPLER
    samplerArg: 1.0  # OTEL_TRACES_SAMPLER_ARG (sampling probability)
  propagation:
    propagators: "tracecontext,baggage"  # OTEL_PROPAGATORS
```

### 2. Direct Initialization

Initialize the providers directly in your application:

```go
import "github.com/arloliu/otx"

func main() {
    ctx := context.Background()
    cfg := loadConfig() // your config loading logic

    // Create TracerProvider
    tp, err := otx.NewTracerProvider(ctx, cfg.Telemetry)
    if err != nil && !errors.Is(err, otx.ErrDisabled) {
        log.Fatal(err)
    }
    if tp != nil {
        defer tp.Shutdown(ctx)
        // Initialize global tracer for otx.Start() helpers
        otx.InitTracing(tp.Tracer("otx"), otx.DefaultNamer{})
    }

    // ... rest of your application
}
```

### 3. Integration with Fx

Since `otx` is framework-agnostic, you can define your own `FxProviders` in your application (e.g., in `internal/config/di.go`):

```go
// FxProviders provides the OTX components to the fx application.
var FxProviders = fx.Options(
	fx.Provide(ProvideNamer),
	fx.Provide(ProvideTracerProvider),
	fx.Provide(ProvideLoggerProvider),
	fx.Provide(ProvideMeterProvider),
	fx.Provide(ProvideOTelServiceName),
)

// otelServiceNameResult is the fx output struct for ProvideOTelServiceName.
type otelServiceNameResult struct {
	fx.Out

	ServiceName string `name:"otel_service_name"`
}

// ProvideOTelServiceName extracts the service name from TelemetryConfig for use by other packages.
func ProvideOTelServiceName(cfg *otx.TelemetryConfig) otelServiceNameResult {
	return otelServiceNameResult{ServiceName: cfg.ServiceName}
}

	// ProvideNamer creates a SpanNamer.
	func ProvideNamer() otx.SpanNamer {
		return otx.DefaultNamer{}
	}

	// ProvideTracerProvider creates and registers the global TracerProvider.
	func ProvideTracerProvider(
		lc fx.Lifecycle,
		cfg *otx.TelemetryConfig,
		namer otx.SpanNamer,
	) (oteltrace.TracerProvider, error) {
		ctx := context.Background()
		tp, err := otx.NewTracerProvider(ctx, cfg)
		if err != nil {
			if errors.Is(err, otx.ErrDisabled) {
				return nil, nil
			}
			return nil, err
		}
		if tp != nil {
			otx.InitTracing(tp.Tracer("otx"), namer)
		}
		registerTracerShutdownHook(tp, lc)
		return tp, nil
	}
```

## Usage Guide

### Starting Spans

We provide helper functions to handle the boilerplate of starting spans. These automatically use the global tracer and configured naming strategy.

| Helper | Use Case | Span Kind | Interpretation |
| :--- | :--- | :--- | :--- |
| `otx.Start` | Generic operations | `Internal` | Default block of logic |
| `otx.StartServer` | Handling a request | `Server` | Inbound from queue/RPC |
| `otx.StartClient` | Calling a service | `Client` | Outbound request |
| `otx.StartInternal` | Background tasks | `Internal` | Cron jobs, loop processing |
| `otx.StartProducer` | Publishing messages | `Producer` | Kafka/NATS publishing |
| `otx.StartConsumer` | Processing messages | `Consumer` | Queue message handling |

**Example: Business Logic**
```go
func (s *Service) ProcessOrder(ctx context.Context, orderID string) error {
    // Starts a child span. Parent is extracted from ctx.
    ctx, span := otx.Start(ctx, "ProcessOrder")
    defer span.End()

    otx.SetAttributes(ctx, attribute.String("order.id", orderID))

    if err := s.validate(ctx, orderID); err != nil {
        // Records error and sets span status to Error
        otx.RecordError(ctx, err)
        return err
    }

    return nil
}
```

**Example: Background Job (Root Span)**
```go
func (w *Worker) Run() {
    // Starts a NEW trace (Root Span) because ctx is background
    ctx, span := otx.StartInternal(context.Background(), "DailyCleanup")
    defer span.End()

    // ...
}
```

### Middleware

The `otx/middleware` package provides drop-in wrappers for transport instrumentation.

#### gRPC Middleware

The gRPC middleware wraps `otelgrpc` stats handlers for server and client-side tracing.

**Basic Usage (uses global providers):**
```go
// Server - uses global TracerProvider, MeterProvider, and Propagator
srv := grpc.NewServer(
    grpc.StatsHandler(middleware.GRPCServerHandler()),
)

// Client - uses global providers
conn, err := grpc.NewClient(addr,
    grpc.WithStatsHandler(middleware.GRPCClientHandler()),
)
```

**With Explicit Providers (for testing or multi-tenant scenarios):**
```go
// Server with explicit providers
srv := grpc.NewServer(
    grpc.StatsHandler(middleware.GRPCServerHandlerWithProviders(
        tracerProvider,   // trace.TracerProvider
        meterProvider,    // metric.MeterProvider
        propagator,       // propagation.TextMapPropagator
    )),
)

// Client with explicit providers
conn, err := grpc.NewClient(addr,
    grpc.WithStatsHandler(middleware.GRPCClientHandlerWithProviders(
        tracerProvider,
        meterProvider,
        propagator,
    )),
)
```

#### HTTP Middleware

**Basic Usage (uses global providers):**
```go
// Server Middleware
http.Handle("/api", middleware.HTTPMiddleware()(myHandler))

// Client Transport
client := &http.Client{
    Transport: middleware.HTTPTransport(http.DefaultTransport),
}
```

**With Explicit Providers (for testing or multi-tenant scenarios):**
```go
// Server Middleware with explicit providers
http.Handle("/api", middleware.HTTPMiddlewareWithProviders(
    tracerProvider,
    meterProvider,
    propagator,
)(myHandler))

// Client Transport with explicit providers
client := &http.Client{
    Transport: middleware.HTTPTransportWithProviders(
        http.DefaultTransport,
        tracerProvider,
        meterProvider,
        propagator,
    ),
}
```

## Provider Lifecycle and Shutdown

All providers (`TracerProvider`, `LoggerProvider`, `MeterProvider`) hold resources (connections, buffers) that must be released on application shutdown.

### Shutdown Requirements

| Provider | Must Call Shutdown? | Consequence if Not Called |
|----------|---------------------|---------------------------|
| `TracerProvider` | **Yes** | Pending spans may be lost; connections leak |
| `LoggerProvider` | **Yes** | Pending logs may be lost; connections leak |
| `MeterProvider` | **Yes** | Final metrics may be lost; connections leak |

### Recommended Shutdown Order

Shutdown providers in reverse order of creation to ensure all telemetry is flushed:

```go
func main() {
    ctx := context.Background()

    // Create providers
    tp, _ := otx.NewTracerProvider(ctx, cfg)
    lp, _ := otx.NewLoggerProvider(ctx, cfg)
    mp, _ := otx.NewMeterProvider(ctx, cfg)

    // Shutdown in reverse order (LIFO)
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        if mp != nil { mp.Shutdown(shutdownCtx) }
        if lp != nil { lp.Shutdown(shutdownCtx) }
        if tp != nil { tp.Shutdown(shutdownCtx) }
    }()

    // ... application code
}
```

### Shutdown Behavior

- `Shutdown(ctx)` is **safe to call multiple times** (idempotent)
- `Shutdown(ctx)` respects the context deadline/timeout
- After shutdown, providers become no-ops (safe but ineffective)

## Best Practices

1.  **Always Defer End()**: Spans must be ended to be exported. `defer span.End()` is the safest way.
2.  **Pass Context**: Context is the glue. It carries the TraceID. If you break the context chain (e.g., using `context.Background()` inside a handler), you break the trace.
3.  **Use Attributes, Not Logs**: For structured data (IDs, status codes), use `otx.SetAttributes`. Use logs for high-volume text.
4.  **Error Handling**: Use `otx.RecordError(ctx, err)` to ensure the span is marked as failed in the monitoring UI.
5.  **Mark Success**: Use `otx.SetSuccess(ctx)` at the end of successful operations to explicitly mark the span status.
6.  **Kind Matters**: Use `StartClient` / `StartServer` / `StartProducer` / `StartConsumer` correctly. This helps backends generate dependency graphs (Service Map).
7.  **Access Current Span**: Use `otx.Span(ctx)` to get the current span for advanced operations.
8.  **Always Shutdown Providers**: Call `Shutdown(ctx)` on all providers before application exit to flush pending telemetry.

### Span Naming Best Practices

Based on conventions from Kubernetes, OpenTelemetry Contrib, gRPC-Go, Jaeger, and go-kit:

| Category | Convention | Examples |
|----------|------------|----------|
| **HTTP Server** | `METHOD /route` | `GET /users/{id}`, `POST /api/orders` |
| **HTTP Client** | `METHOD` or `METHOD /path` | `GET`, `POST /api/users` |
| **gRPC Server** | `package.Service/Method` | `myapp.OrderService/CreateOrder` |
| **gRPC Client** | `package.Service/Method` | `payment.PaymentService/Charge` |
| **Database** | `operation table` or `collection.operation` | `SELECT users`, `orders.find` |
| **Messaging** | `operation destination` | `publish orders.created`, `receive ORDERS` |
| **Cloud SDK** | `Service.Operation` | `S3.GetObject`, `DynamoDB.PutItem` |
| **Internal** | Descriptive name | `ProcessOrder`, `validate.input` |

**Key Principles:**
- **Low cardinality**: Use route templates (`/users/{id}`) not actual paths (`/users/12345`)
- **Include HTTP method**: `GET /users` not just `/users`
- **Use consistent delimiters**: Dots for packages, slashes for routes
- **Be descriptive but concise**: `findTraceIDs` over `find` or `findTraceIDsFromDatabaseByServiceName`

## Troubleshooting

-   **"My traces are disconnected!"**: Check if you are passing `ctx` correctly through all functions. Ensure you aren't using `context.TODO()` or `context.Background()` mid-flow.
-   **"No traces appear"**:
    -   Check `OTX_ENABLED=true`.
    -   Check `OTEL_EXPORTER_OTLP_ENDPOINT` is reachable.
    -   Check logs for `otx` errors (initialization failures).
-   **"Timestamps are wrong"**: Ensure system clocks are synchronized, especially in distributed environments.

### Environment Variables

All configuration options can be set via environment variables. OTX follows the [OpenTelemetry Environment Variable Specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/).

| Variable | Description | Default |
|----------|-------------|---------|
| `OTX_ENABLED` | Enable/disable OTX telemetry system | `false` |
| `OTEL_SERVICE_NAME` | Service name for telemetry identification | (required if enabled) |
| `OTEL_SERVICE_VERSION` | Service version (e.g., git commit, semver) | - |
| `OTEL_DEPLOYMENT_ENVIRONMENT` | Deployment environment (production, development) | `development` |
| `OTEL_RESOURCE_ATTRIBUTES` | Additional resource attributes (comma-separated key=value) | - |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector endpoint | `localhost:4317` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | OTLP protocol: `grpc`, `http/protobuf`, `http` | `grpc` |
| `OTEL_EXPORTER_OTLP_HEADERS` | Custom headers (comma-separated key=value) | - |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | Exporter timeout | `10s` |
| `OTEL_EXPORTER_OTLP_INSECURE` | Disable TLS for OTLP connection | `true` |
| `OTEL_EXPORTER_OTLP_COMPRESSION` | Compression: `gzip`, `none` | - |
| `OTEL_TRACES_EXPORTER` | Trace exporter: `otlp`, `console`, `stdout`, `none` | `otlp` |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Override endpoint for traces only | - |
| `OTEL_TRACES_SAMPLER` | Sampler type (see below) | `parentbased_always_on` |
| `OTEL_TRACES_SAMPLER_ARG` | Sampler argument (ratio 0.0-1.0) | `1.0` |
| `OTEL_LOGS_EXPORTER` | Log exporter: `otlp`, `console`, `stdout`, `none` | `otlp` |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | Override endpoint for logs only | - |
| `OTEL_METRICS_EXPORTER` | Metrics exporter: `otlp`, `console`, `stdout`, `none` | `otlp` |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Override endpoint for metrics only | - |
| `OTEL_METRIC_EXPORT_INTERVAL` | Metrics export interval | `60s` |
| `OTEL_PROPAGATORS` | Context propagators (comma-separated) | `tracecontext,baggage` |

**Sampler Types** (`OTEL_TRACES_SAMPLER`):
- `always_on` - Sample all traces
- `always_off` - Sample no traces
- `traceidratio` - Sample based on trace ID ratio
- `parentbased_always_on` - Parent-based with always_on root (default)
- `parentbased_always_off` - Parent-based with always_off root
- `parentbased_traceidratio` - Parent-based with ratio root

**Propagator Types** (`OTEL_PROPAGATORS`):
- `tracecontext` - W3C Trace Context
- `baggage` - W3C Baggage
- `b3`, `b3multi` - Zipkin B3 (requires contrib package)
- `jaeger`, `xray`, `ottrace` - Other formats (require contrib packages)