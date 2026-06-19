# 100 - Project Map

- Project: otx ("OpenTelemetry eXtensions") — config-driven wrapper over the official
  OpenTelemetry SDK for Go services (traces, logs, metrics, propagation, span ergonomics).
- Module: `github.com/arloliu/otx`  |  Go 1.25  |  golangci-lint v1.62.0 (`make lint`).
- Design: `docs/design/2026-06-12-zapwire-integration-design.md`; user guides in `docs/`.

## Structure
- Root `otx`: public API — `TelemetryConfig` + loader (YAML/env), providers
  (`NewTracerProvider`, `NewMeterProvider`, `NewLoggerProvider`), exporters, sampling,
  span helpers (`Start`, `StartServer`, …), baggage, propagation, span naming (`SpanNamer`).
- `grpc/`: gRPC server/client `stats.Handler` tracing+metrics instrumentation (otelgrpc).
- `http/`: HTTP middleware, client, and transport (otelhttp).
- `nats/`: NATS JetStream publisher/consumer tracing (carrier, attributes, handler).
- `zaplog/`: adapts `TelemetryConfig` to zapwire's OTLP log data plane; context-aware
  logging surface.
- `internal/endpoint/`: OTLP endpoint classification/validation, shared by config,
  `exporter.go`, and `zaplog`.
- `internal/tracker/`: span namer + global tracing state.
- `cmd/otlp-sim/`: CLI that simulates traces/logs against a collector.

## Dependency policy
See the root `AGENTS.md`. otx is heavy by design (OTel SDK + exporters, grpc, protobuf, contrib,
NATS, `zapwire/otlp`); single module. **Permanent rule: otx MAY depend on zapwire; zapwire
must NEVER depend on otx.** Check `go.mod` and ask before adding any dependency.
