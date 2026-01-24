# Configuration Reference

OTX supports configuration via YAML files and environment variables. Environment variables take precedence over file values.

## Configuration Structure

```yaml
telemetry:
  enabled: true
  serviceName: "my-service"
  version: "1.0.0"
  environment: "production"
  resourceAttributes:
    team: "platform"
    region: "us-east-1"

  otlp:
    endpoint: "localhost:4317"
    protocol: "grpc"
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
export OTEL_EXPORTER_OTLP_ENDPOINT=collector:4317

# Signal-specific overrides
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=traces-collector:4317
export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=logs-collector:4317
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=metrics-collector:4317
```

## Sampling Strategies

| Sampler | Use Case |
|---------|----------|
| `always_on` | Development, debugging |
| `always_off` | Disable tracing entirely |
| `traceidratio` | Production with fixed sample rate |
| `parentbased_always_on` | Honor parent decisions, sample roots |
| `parentbased_traceidratio` | Production with parent-based sampling |

### Recommended Production Setup

```yaml
sampling:
  sampler: "parentbased_traceidratio"
  samplerArg: 0.1  # 10% of root spans
```

## Validation

OTX validates configuration at load time:

- `serviceName`: Required when enabled
- `samplerArg`: Must be between 0.0 and 1.0
- `protocol`: Must be `grpc`, `http/protobuf`, or `http`
- `exporter`: Must be `otlp`, `console`, `stdout`, or `none`
- `timeout`: Must be non-negative
- `interval`: Must be positive

Invalid configuration will return an error from `LoadConfig` or `ParseConfig`.
