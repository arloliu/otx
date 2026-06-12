# Zap Logging

The `otx/zaplog` package ships [zap](https://github.com/uber-go/zap) logs to any
OTLP receiver over [zapwire](https://github.com/arloliu/zapwire)'s lean OTLP
exporter (HTTP or gRPC), deriving the resource identity (service.name, version, environment,
resource attributes) from the same `TelemetryConfig` your traces use — so logs
and traces join in the backend.

## Overview

- One config: `zaplog.NewCore` reads your existing `otx.TelemetryConfig`.
- One resource identity: logs carry the same attributes as traces and metrics.
- Trace correlation from `context.Context`: the active span's `trace_id` /
  `span_id` land in the OTLP `LogRecord` fields, not as string attributes.
- Single encode pass, no SDK log bridge, no grpc-go dependency on the log data path,
  at-most-once delivery with counted drops (`Writer.DroppedLogs`).

## Routing Rule

A zap service can reach OTLP two ways. **Use exactly one per logger:**

- **zaplog path** (this package): zap → zapwire OTLP/HTTP or OTLP/gRPC. The
  default for zap services — lean, single-encode, no grpc-go on the data path.
- **`otx.NewLoggerProvider`**: the OTel SDK log pipeline. Use it for non-zap
  paths (slog bridge, the direct OTel log API).

Never run both for one logger — that double-ships every record.

## Quick Start

```go
package main

import (
    "context"

    "github.com/arloliu/otx"
    "github.com/arloliu/otx/zaplog"
    "go.uber.org/zap"
)

func ptr[T any](v T) *T { return &v }

func main() {
    ctx := context.Background()

    cfg := &otx.TelemetryConfig{
        Enabled:     ptr(true),
        ServiceName: "checkout",
        Version:     "1.2.3",
        Environment: "production",
        OTLP: &otx.OTLPConfig{
            Endpoint: "collector:4318", // default port for OTLP/HTTP
            Insecure: ptr(true),        // bare host:port -> http://
            // Protocol defaults to "http/protobuf" — no override needed.
        },
        Logs: &otx.LogsConfig{Enabled: ptr(true)},
    }

    // zaplog.New is the one-call setup: builds the OTLP core, tees it onto
    // base (or OTLP-only when base is nil), and returns a ctx-aware Logger.
    base, _ := zap.NewProduction()
    log, w, err := zaplog.New(ctx, cfg, base, nil) // nil level -> cfg.Logs.MinLevel -> info
    if err != nil {
        panic(err)
    }
    defer func() { _ = w.Sync(); _ = w.Close() }() // see Shutdown below

    // ctx-aware methods inject the active span context for trace correlation.
    ctx, span := otx.Start(ctx, "handleCheckout")
    defer span.End()
    log.InfoCtx(ctx, "checkout completed", zap.String("order.id", "A-1001"))
}
```

`zaplog` supports both OTLP/HTTP and OTLP/gRPC. The shared `OTLP.Protocol`
defaults to `http/protobuf`, so zaplog works out of the box with no extra
configuration. To use gRPC, set `OTLP.Protocol: grpc` (or the per-signal
`telemetry.logs.protocol: grpc` / env `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=grpc`).
Default ports follow the protocol: `4318` for http/protobuf, `4317` for grpc.

`zaplog`'s gRPC transport is zapwire's hand-rolled stdlib client — it does not
add grpc-go to your binary's data path. Endpoint forms and scheme precedence are
unchanged for both protocols (see Endpoint mapping below). For gRPC, header
values must be printable ASCII without leading or trailing whitespace; values
that violate this are rejected at construction with an actionable zapwire error.
Reserved gRPC metadata key prefixes (`grpc-`, `-bin`, `content-type`, `te`) are
also rejected at construction. Headers that work on HTTP may therefore fail on
gRPC if they contain non-ASCII or whitespace-padded values.

### Endpoint mapping

otx endpoints accept `host:port` (canonical, both protocols) or a scheme-bearing
`http(s)://` URL; zaplog maps bare endpoints to the `http(s)://` form zapwire
needs:

- bare `host:port` + `insecure: true` → `http://host:port`
- bare `host:port` + `insecure: false` → `https://host:port`
- bare host without a port → protocol default port is appended automatically
  (`4317` for gRPC, `4318` for http/protobuf) before the scheme is prefixed
- an endpoint already carrying a scheme passes through unchanged

**HTTP/protobuf path semantics:** zapwire appends `/v1/logs` when the path is
empty; custom paths (e.g. `http://collector:4318/custom/path`) are passed
through unchanged.

**gRPC path semantics:** gRPC always posts to the fixed OTel method path
(`/opentelemetry.proto.collector.logs.v1.LogsService/Export`). Endpoint paths,
query parameters, and fragments are **rejected at construction** with an
actionable zapwire error — use a bare `host:port` or a path-free `http(s)://`
URL for gRPC.

## Attach to an Existing Logger

`zaplog.New` accepts an existing base logger and tees the OTLP core onto it
automatically. Pass `nil` as base for OTLP-only mode (no console/JSON sink).

For **manual composition** — for example when you need a runtime-adjustable
`*zap.AtomicLevel` dial or want to share one core across multiple pipelines —
build the pieces yourself:

```go
lvl := zap.NewAtomicLevelAt(zapcore.WarnLevel)  // adjust live with lvl.SetLevel(...)
core, w, err := zaplog.NewCore(ctx, cfg, &lvl)
// ...
base := zap.Must(zap.NewProduction())     // your existing logger
teed := zaplog.Attach(base, core)         // writes to both sinks
log := zaplog.Wrap(teed)                  // ctx-aware Logger over the tee'd logger
```

`zaplog.New` also accepts an explicit `zapcore.LevelEnabler` (including
`*zap.AtomicLevel`), so the one-call path covers the runtime-dial case too.

Tee'd non-OTLP cores receive the trace context as an ordinary `span_context`
field and render it legibly; the OTLP core consumes it into the `LogRecord`
trace fields.

## Cost Control with `minLevel`

Logging everything to OTel can be expensive. Trace sampling does **not** gate
logs — the cost lever is level gating. Send only `warn` and above to OTLP while
keeping `debug`/`info` on a local sink:

```yaml
telemetry:
  logs:
    enabled: true
    minLevel: warn   # zaplog OTLP core emits warn and above; default: info
    # protocol defaults to http/protobuf; set explicitly only when OTLP.Protocol is grpc
```

`minLevel` sets the OTLP core's `LevelEnabler` when you pass `nil` as the level
to `NewCore`. For a runtime dial, pass an explicit `*zap.AtomicLevel` instead and
adjust it live.

## Shutdown

`NewCore` returns the `*otlp.Writer`; the caller owns it. The recommended
shutdown is **`w.Sync()` then `w.Close()`**:

```go
_ = w.Sync()  // flush barrier with the full retry budget
_ = w.Close() // drain remaining records (single attempt each) and release
```

`Sync` flushes everything enqueued before the call with the full retry budget;
`Close` alone drains with a single attempt per batch (fast, but a transiently
failing collector loses the final batch). `logger.Sync()` reaches the writer
through the tee. `w.DroppedLogs()` stays readable after `Close` for shutdown
reporting. Writer shutdown is independent of `tp.Shutdown` / `lp.Shutdown` — no
ordering constraint.

## Further Reading

- zapwire's [OTLP guide](https://github.com/arloliu/zapwire) covers the data
  plane in depth: delivery semantics, retry/backoff tuning, headers, gzip, and
  the trace-context compatibility matrix. Any `otlp.Option` passes through
  `zaplog.NewCore`'s variadic `opts` (caller options override config-derived
  ones), so those features stay reachable.
