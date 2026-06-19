# Changelog

All notable changes to otx (`github.com/arloliu/otx`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

## v1.1.0 — 2026-06-19

Feature release: zap-native OTLP logging via zapwire, OTLP endpoint validation,
and a change to the default export protocol. No exported symbols were removed,
so this is a minor release — but review the **default-protocol behavior change**
below before upgrading.

- **Added:** `zaplog` — a sub-package that builds a zapwire OTLP log core and
  writer from a single `otx.TelemetryConfig` (OTLP/HTTP or OTLP/gRPC; selecting
  gRPC routes through zapwire's OTLP/gRPC transport). It derives the resource
  identity from the same (now-exported) `BuildResource` the trace and metric
  providers use, so logs and traces join on identical resource attributes in the
  backend, and adds a context-aware logging surface (`Logger` with `InfoCtx` /
  `DebugCtx` / `WarnCtx` / `ErrorCtx`, plus `Attach`, `Wrap`, `New`, `NewCore`).
- **Added:** `BuildResource` is now exported, and logs gain an independent
  protocol and minimum-level configuration separate from traces and metrics.
- **Added:** `LogsConfig.DrainTimeout` (`drainTimeout`) — bounds the time
  `Sync` / `Close` spend draining the log queue on shutdown, wired through to
  zapwire's `otlp.WithDrainTimeout`. Default 0 keeps the unbounded barrier.
- **Added:** OTLP endpoint scheme validation (`internal/endpoint`) — endpoints
  are classified as a bare `host:port`, an `http://` URL, or an `https://` URL,
  with the URL scheme taking precedence over the `Insecure` flag; any other
  scheme (`grpc://`, `unix://`, …) is rejected with an actionable error. The same
  helper is shared by config validation, the exporters, and `zaplog`.
- **Changed (behavior):** the default OTLP protocol for the telemetry config is
  now `http/protobuf` with a default endpoint of `localhost:4318`, replacing the
  previous OTLP/gRPC default on `:4317`. Set the protocol and endpoint explicitly
  to opt back into gRPC, and adjust deployments that relied on the gRPC default.
  (The deprecated `ExporterConfig` retains its legacy `grpc` / `localhost:4317`
  default.)
- **Changed (dependencies):** updated the OpenTelemetry SDK and exporters, gRPC,
  NATS, and zapwire; upgraded `zapwire/otlp` to v0.5.0. The v0.4.0 bump dropped
  the protocol-ambiguous `NewCore` / `NewWriter` constructors, so `zaplog`'s HTTP
  path moved to `otlp.NewHTTPCore`.
- **Fixed:** restore otx's documented HTTP span naming after otelhttp v0.69
  changed its default formatter to derive the span name from HTTP semantic
  conventions; `Handler` / `Middleware` again use the supplied operation as the
  span name (a caller-provided `WithSpanNameFormatter` still wins).
- **Fixed:** protocol-aware OTLP endpoint defaults are now resolved consistently
  across every resolver and the config loader.
- **Changed (docs):** added the zap-logging guide and the zapwire-integration
  design doc.

## v1.0.2 — 2026-02-19

- **Added:** JSON struct tags across `TelemetryConfig` (alongside the existing
  YAML tags) and comprehensive configuration tests.

## v1.0.1 — 2026-01-29

- **Fixed (otlp-sim):** the simulator CLI now applies its telemetry
  configuration, and tracks and reports export errors.

## v1.0.0 — 2026-01-28

Initial stable release. Config-driven OpenTelemetry setup for Go services:
provider construction for traces, metrics, and logs with OTLP (gRPC/HTTP) and
stdout exporters, OTel-standard sampling, W3C TraceContext and Baggage
propagation, span ergonomics (`Start`, `StartServer`, …) with pluggable span
naming, HTTP and gRPC instrumentation, NATS JetStream publisher/consumer
tracing, and the `otlp-sim` CLI.
