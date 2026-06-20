# Getting Started with OTX

This guide will help you set up OTX in your Go service in under 5 minutes.

## Installation

```bash
go get github.com/arloliu/otx
```

## Minimal Setup

### 1. Create Configuration

```go
package main

import (
    "context"
    "errors"
    "log"

    "github.com/arloliu/otx"
)

func main() {
    ctx := context.Background()

    // Minimal configuration
    cfg := &otx.TelemetryConfig{
        Enabled:     otx.BoolPtr(true),
        ServiceName: "my-service",
    }

    // Create TracerProvider
    tp, err := otx.NewTracerProvider(ctx, cfg)
    if err != nil && !errors.Is(err, otx.ErrDisabled) {
        log.Fatal(err)
    }
    if tp != nil {
        defer tp.Shutdown(ctx)
        // Initialize global tracer
        otx.InitTracing(tp.Tracer("my-service"), otx.DefaultNamer{})
    }

    // Your application code
    runApp(ctx)
}
```

### 2. Create Your First Span

```go
import (
    "context"

    "go.opentelemetry.io/otel/attribute"

    "github.com/arloliu/otx"
)

func runApp(ctx context.Context) {
    ctx, span := otx.Start(ctx, "runApp")
    defer span.End()

    // Your business logic here
    processData(ctx)
}

func processData(ctx context.Context) {
    ctx, span := otx.Start(ctx, "processData")
    defer span.End()

    // Add attributes
    otx.SetAttributes(ctx,
        attribute.String("data.type", "user"),
        attribute.Int("data.count", 42),
    )

    // Mark success
    otx.SetSuccess(ctx)
}
```

### 3. Using Environment Variables

Instead of hardcoding configuration, use environment variables:

```bash
export OTX_ENABLED=true
export OTEL_SERVICE_NAME=my-service
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
```

```go
cfg, err := otx.LoadConfig("config.yaml")
// or use ParseConfig for embedded config
```

## Logging

If your service logs with [zap](https://github.com/uber-go/zap), ship logs over
the lightweight `otx/zaplog` adapter instead of the SDK log bridge. **Routing
rule:** zap services use the zaplog path (zap → zapwire OTLP, single encode pass,
no SDK log bridge); `otx.NewLoggerProvider` stays for non-zap paths (slog bridge,
the direct OTel log API). Never run both for one logger. zaplog supports both
`http/protobuf` (default, port 4318) and `grpc` (port 4317) — set
`logs.protocol: grpc` or `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=grpc` to opt in. zaplog
derives its resource identity from the same config as your traces, so logs and traces
join in the backend. See the [Zap Logging](zap-logging.md) guide.

## Next Steps

- [Configuration Reference](configuration.md) - All configuration options
- [Zap Logging](zap-logging.md) - Ship zap logs over OTLP with trace correlation
- [Tracing Best Practices](tracing-best-practices.md) - Span naming, attributes
- [Semantic Conventions](semantic-conventions.md) - OpenTelemetry standards
- [HTTP/gRPC Integration](http-grpc-integration.md) - Middleware setup
