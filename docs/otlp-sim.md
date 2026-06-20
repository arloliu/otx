# OTLP Simulator CLI (otlp-sim)

A command-line tool for simulating OpenTelemetry traces and logs. Useful for testing OTLP collectors, observability backends like Tempo or Signoz, and validating trace visualization.

## Installation

```bash
go install github.com/arloliu/otx/cmd/otlp-sim@latest
```

Or build from source:
```bash
cd cmd/otlp-sim
go build -o otlp-sim .
```

## Quick Start

```bash
# Send 5 quick traces to local collector
otlp-sim quick --count 5

# Run continuous simulation for 5 minutes
otlp-sim run --duration 5m --rate 2

# List available scenarios
otlp-sim list
```

## Modes

### quick - Immediate Trace Generation

Sends a specified number of traces immediately without timing delays. Best for quick visualization testing.

```bash
otlp-sim quick [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--endpoint` | `localhost:4317` | OTLP endpoint |
| `--http` | `false` | Use HTTP instead of gRPC |
| `--insecure` | `true` | Skip TLS verification |
| `--scenario` | `payment` | Scenario name |
| `--scenario-file` | | Custom YAML scenario file |
| `--count` | `10` | Number of traces to send |
| `--logs` | `false` | Enable log generation |
| `--service-name` | | Override service name |

**Examples:**
```bash
# Send 20 payment traces
otlp-sim quick --count 20 --scenario payment

# Send to HTTP endpoint with logs
otlp-sim quick --endpoint localhost:4318 --http --logs

# Use custom scenario file
otlp-sim quick --scenario-file ./my-scenario.yaml --count 5
```

### run - Continuous Simulation

Simulates real-world timing with controlled rate and duration. Best for load testing and realistic simulations.

```bash
otlp-sim run [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--endpoint` | `localhost:4317` | OTLP endpoint |
| `--http` | `false` | Use HTTP instead of gRPC |
| `--insecure` | `true` | Skip TLS verification |
| `--scenario` | `payment` | Scenario name |
| `--scenario-file` | | Custom YAML scenario file |
| `--duration` | `1m` | Total simulation time |
| `--rate` | `1` | Traces per second |
| `--jitter` | `20` | Timing variation percentage (0-100) |
| `--logs` | `false` | Enable log generation |
| `--service-name` | | Override service name |

**Examples:**
```bash
# Run for 10 minutes at 5 traces/sec
otlp-sim run --duration 10m --rate 5

# E-commerce scenario with high jitter
otlp-sim run --scenario ecommerce --duration 30m --rate 10 --jitter 40

# Edge IoT scenario with logs
otlp-sim run --scenario edge-iot --duration 1h --rate 0.5 --logs
```

### list - Show Available Scenarios

Lists all built-in scenarios with descriptions.

```bash
otlp-sim list
```

## Built-in Scenarios

| Scenario | Description | Services | Spans |
|----------|-------------|----------|-------|
| `payment` | Online payment system flow | 6 | 7 |
| `edge-iot` | Edge device management | 4 | 6 |
| `ecommerce` | E-commerce order flow | 4 | 7 |
| `health-check` | Simple connectivity test | 1 | 1 |

### payment
Simulates an online payment flow: payment-gateway → payment-service → fraud-detection → ml-service → payment-processor → notification-service. Includes gRPC, HTTP, and async messaging spans.

### edge-iot
Simulates edge device telemetry: device-gateway → device-registry → telemetry-processor → rule-engine. High-volume, low-latency patterns with Redis and TimescaleDB.

### ecommerce
Simulates order creation: api-gateway → order-service → inventory-service/pricing-service. Includes database and messaging spans.

### health-check
Single HTTP request span for verifying OTLP connectivity. Minimal overhead for testing collector setup.

## Custom Scenarios

Create custom scenarios using YAML:

```yaml
name: my-custom-scenario
description: Custom API flow

services:
  - name: api-gateway
  - name: user-service

rootSpan:
  name: GET /api/v1/users
  service: api-gateway
  kind: SERVER
  duration: 50ms
  attributes:
    http.request.method: GET
    http.route: /api/v1/users
    http.response.status_code: "200"
  logs:
    - level: INFO
      message: "Request received"
  children:
    - name: SELECT users
      service: user-service
      kind: CLIENT
      duration: 15ms
      attributes:
        db.system: postgresql
        db.namespace: users
        db.query.text: "SELECT * FROM users WHERE id = ?"
```

**Load custom scenario:**
```bash
otlp-sim quick --scenario-file ./my-scenario.yaml --count 10
```

### Scenario YAML Structure

```yaml
name: string              # Required: scenario name
description: string       # Optional: description

services:                 # Flat list of participating services
  - name: string          # Required: service name
    attributes:           # Optional: service-level attributes
      key: value

rootSpan:                 # Single root span; children form the trace tree
  name: string            # Required: span name
  service: string         # Required: service name (must appear in services)
  kind: string            # SERVER|CLIENT|PRODUCER|CONSUMER|INTERNAL
  duration: duration      # e.g., 50ms, 1s
  errorRate: float64      # Error probability 0.0-1.0
  errorStatus: string     # Status message when error is triggered
  attributes:             # OpenTelemetry attributes
    key: value
  logs:                   # Optional log entries
    - level: string       # DEBUG|INFO|WARN|ERROR (case-sensitive; unrecognized falls back to INFO)
      message: string
      attributes:
        key: value
  children:               # Nested child spans (recursive SpanTemplate)
    - name: string
      service: string
      kind: string        # SERVER|CLIENT|PRODUCER|CONSUMER|INTERNAL
      duration: duration
```

## Environment Variables

The CLI respects standard OpenTelemetry environment variables:

| Variable | Description | Overrides Flag |
|----------|-------------|----------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint | `--endpoint` |
| `OTEL_EXPORTER_OTLP_INSECURE` | Skip TLS (`true`/`false`) | `--insecure` |
| `OTEL_SERVICE_NAME` | Default service name | `--service-name` |

**Precedence:** Environment variables > CLI flags > Defaults

**Note:** The export protocol (gRPC vs HTTP/protobuf) is selected only via the `--http` flag, not via an environment variable.

**Example:**
```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
export OTEL_SERVICE_NAME=payment-simulator

otlp-sim quick --count 100
```

## Use Cases

### Testing Collector Setup
```bash
# Verify collector accepts traces
otlp-sim quick --scenario health-check --count 1
```

### Load Testing
```bash
# Sustained load for 30 minutes
otlp-sim run --duration 30m --rate 100 --scenario payment
```

### Demo/Presentation
```bash
# Realistic simulation with varied timing
otlp-sim run --duration 15m --rate 5 --jitter 30 --logs
```

### Integration Testing
```bash
# Generate traces for test assertions
otlp-sim quick --count 50 --scenario ecommerce
```
