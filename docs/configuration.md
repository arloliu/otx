# Configuration Reference

OTX supports configuration via YAML files and environment variables. Environment variables take precedence over file values.

## Configuration Structure

```yaml
enabled: true
serviceName: "my-service"
version: "1.0.0"
environment: "production"
resourceAttributes:
  team: "platform"
  region: "us-east-1"

otlp:
  endpoint: "localhost:4318"
  protocol: "http/protobuf"
  insecure: true
  timeout: 10s
  compression: "gzip"
  headers:
    Authorization: "Bearer token"

traces:
  enabled: true
  exporter: "otlp"
  endpoint: ""  # Override otlp.endpoint for traces only
  sampling:
    sampler: "parentbased_traceidratio"
    samplerArg: 0.1

logs:
  enabled: false
  exporter: "otlp"
  protocol: "http/protobuf"  # per-signal override; OTEL_EXPORTER_OTLP_LOGS_PROTOCOL
  minLevel: "warn"           # otx/zaplog OTLP core min level (default: info)

metrics:
  enabled: false
  exporter: "otlp"
  interval: 60s

propagation:
  propagators: "tracecontext,baggage"
```

## Environment Variables

See [README.md](../README.md#environment-variables) for the complete list.

### Priority Order

1. Environment variables (highest priority)
2. YAML configuration file
3. Default values (lowest priority)

### Signal-Specific Endpoints

You can override the OTLP endpoint for specific signals:

```bash
# Shared endpoint
export OTEL_EXPORTER_OTLP_ENDPOINT=collector:4318

# Signal-specific overrides
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=traces-collector:4318
export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=logs-collector:4318
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=metrics-collector:4318
```

## Sampling Strategies

| Sampler | Use Case |
|---------|----------|
| `always_on` | Development, debugging |
| `always_off` | Disable tracing entirely |
| `traceidratio` | Production with fixed sample rate |
| `parentbased_always_on` | Honor parent decisions, sample roots |
| `parentbased_always_off` | Honor parent decisions, never sample roots |
| `parentbased_traceidratio` | Production with parent-based sampling |

### Recommended Production Setup

```yaml
sampling:
  sampler: "parentbased_traceidratio"
  samplerArg: 0.1  # 10% of root spans
```

## Endpoint forms

Every endpoint field (`otlp.endpoint`, `traces.endpoint`, `logs.endpoint`,
`metrics.endpoint`, and the deprecated `exporter.endpoint`) accepts exactly two
forms:

| Form | Example | Notes |
|------|---------|-------|
| Bare `host:port` | `localhost:4318`, `[::1]:4318`, `collector` | Canonical. TLS controlled by `insecure`. |
| `http://` or `https://` URL | `https://collector:4318/v1/traces` | Scheme overrides `insecure` — `https://` is always TLS, `http://` is always plain. Path is preserved. |

Any other scheme (`grpc://`, `tcp://`, `unix://`, …) is rejected at load time
with an actionable error:

```
field otlp.endpoint: invalid endpoint scheme "grpc": transport is selected by
protocol, not the endpoint scheme; use host:port or an http(s):// URL
```

An empty endpoint is valid; the default follows the effective protocol —
`localhost:4318` for `http/protobuf` (the default), `localhost:4317` for `grpc`.

**Scheme-overrides-insecure precedence:** when a URL scheme is provided, it
wins over the `insecure` flag. `https://host:4318` is always TLS regardless of
`insecure: true`; `http://host:4318` is always plain regardless of
`insecure: false`.

## Validation

OTX validates configuration at load time:

- `serviceName`: Required when enabled
- `samplerArg`: Must be between 0.0 and 1.0 (only meaningful for `traceidratio` and `parentbased_traceidratio`; ignored by other samplers)
- `protocol`: Must be `grpc`, `http/protobuf`, or `http`
- `logs.minLevel`: Must be a zap level (`debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal`)
- `exporter`: Must be `otlp`, `console`, `stdout`, or `none`
- `timeout`: Must be non-negative
- `interval`: Must be positive
- Endpoint fields: must be bare `host:port` or an `http(s)://` URL (see above)

Invalid configuration will return an error from `LoadConfig` or `ParseConfig`.
