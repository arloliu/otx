# Changelog

All notable changes to otx (`github.com/arloliu/otx`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v1.1.0 — 2026-06-20

Feature release: zap-native OTLP logging, OTLP endpoint validation, and a new
default export protocol. No exported symbols were removed (a minor release), but
review the default-protocol change under **Changed** before upgrading.

### Added

- `zaplog` sub-package: builds a zapwire OTLP log core/writer from one
  `TelemetryConfig` (OTLP/HTTP or OTLP/gRPC) and adds a context-aware `Logger`
  (`InfoCtx` / `DebugCtx` / `WarnCtx` / `ErrorCtx`, plus `Attach`, `Wrap`, `New`,
  `NewCore`).
- Exported `BuildResource`, so logs, traces, and metrics share one resource
  identity; logs also gain independent protocol and minimum-level config.
- `LogsConfig.DrainTimeout` — bounds the time `Sync` / `Close` spend draining the
  log queue on shutdown (default 0 keeps the unbounded barrier).
- OTLP endpoint scheme validation (`internal/endpoint`): classifies bare
  `host:port`, `http://`, and `https://`; the URL scheme overrides `Insecure`;
  other schemes are rejected with an actionable error.
- Helpers and sentinels: `BuildPropagator`, `LookupBaggage`, `BoolPtr`,
  `http.WithOTelOptions`, `ErrTracesDisabled`, `ErrUnknownExporter`, and NATS
  `TracedMessageBatch.Stop` / `FetchContext` / `NextContext`.

### Changed

- **Behavior:** the default OTLP protocol is now `http/protobuf` on
  `localhost:4318` (was gRPC on `:4317`). Set the protocol and endpoint
  explicitly to use gRPC. The deprecated `ExporterConfig` keeps its legacy gRPC
  default.
- Provider and exporter constructors validate endpoints up front (invalid config
  fails fast); an unknown exporter type now errors instead of silently falling
  back to OTLP; NATS `Next()` parents header-less messages under the receive span
  like `Fetch()`.
- Updated the OpenTelemetry SDK and exporters, gRPC, NATS, and zapwire
  (`zapwire/otlp` v0.5.0).
- Docs: added the zap-logging guide and the zapwire-integration design doc.

### Fixed

- `NewLoggerProvider` with a URL-form OTLP endpoint (`http://host:port`) now
  sends logs to `/v1/logs` instead of `/`.
- `OTEL_RESOURCE_ATTRIBUTES` / `OTEL_EXPORTER_OTLP_HEADERS` now accept the
  OTel-standard `key=value` form (the legacy `key:value` form still works).
- Restored otx's HTTP span naming after otelhttp v0.69: the supplied operation
  names the span again (a caller's `WithSpanNameFormatter` still wins).
- Protocol-aware OTLP endpoint defaults resolve consistently across every
  resolver and the config loader.
- NATS `WithProcessSpans(false)` is honored (was a no-op).
- The NATS message batch no longer leaks its forwarder goroutine on early exit
  and is race-free on first use.
- Configured-but-unwired propagators (b3, jaeger, …) now warn instead of
  silently dropping context.
- `zaplog` `NewCore` / `Attach` no longer panic on nil input, and disabled-level
  context logging no longer allocates.
- Plaintext export to a remote host now warns.
- Fixed several non-compiling documentation examples.

### Performance

- Cached span-kind options, pre-sized baggage maps, and one-time HTTP middleware
  construction.

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
