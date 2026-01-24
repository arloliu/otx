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
    defer tp.Shutdown(ctx)

    // Initialize global tracer
    otx.InitTracing(tp.Tracer("my-service"), otx.DefaultNamer{})

    // Your application code
    runApp(ctx)
}
```

### 2. Create Your First Span

```go
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
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
```

```go
cfg, err := otx.LoadConfig("config.yaml")
// or use ParseConfig for embedded config
```

## Next Steps

- [Configuration Reference](configuration.md) - All configuration options
- [Tracing Best Practices](tracing-best-practices.md) - Span naming, attributes
- [Semantic Conventions](semantic-conventions.md) - OpenTelemetry standards
- [HTTP/gRPC Integration](http-grpc-integration.md) - Middleware setup
