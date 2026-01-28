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
| `payment` | Online payment system flow | 6 | 8 |
| `edge-iot` | Edge device management | 4 | 5 |
| `ecommerce` | E-commerce order flow | 5 | 7 |
| `health-check` | Simple connectivity test | 1 | 1 |

### payment
Simulates an online payment flow: API Gateway → Payment Service → Fraud Detection → Payment Processor → Notification. Includes gRPC, HTTP, and async messaging spans.

### edge-iot
Simulates edge device telemetry: MQTT broker → gRPC processing → Redis caching → TimescaleDB storage. High-volume, low-latency patterns.

### ecommerce
Simulates order creation: Order Service → Inventory Check → Pricing Service → Event Bus. Includes database and messaging spans.

### health-check
Single HTTP request span for verifying OTLP connectivity. Minimal overhead for testing collector setup.

## Custom Scenarios

Create custom scenarios using YAML:

```yaml
name: my-custom-scenario
description: Custom API flow

services:
  - name: api-gateway
    spans:
      - name: HTTP GET /api/v1/users
        kind: server
        duration: 50ms
        attributes:
          http.method: GET
          http.route: /api/v1/users
          http.status_code: 200
        logs:
          - level: info
            message: "Request received"
            attributes:
              user.id: "{{.RequestID}}"

  - name: user-service
    spans:
      - name: SELECT users
        kind: client
        duration: 15ms
        attributes:
          db.system: postgresql
          db.operation: SELECT
          db.sql.table: users
```

**Load custom scenario:**
```bash
otlp-sim quick --scenario-file ./my-scenario.yaml --count 10
```

### Scenario YAML Structure

```yaml
name: string              # Required: scenario name
description: string       # Optional: description

services:
  - name: string          # Required: service name
    spans:
      - name: string      # Required: span name
        kind: string      # server|client|producer|consumer|internal
        duration: duration  # e.g., 50ms, 1s
        error: float64    # Error probability 0.0-1.0
        attributes:       # OpenTelemetry attributes
          key: value
        logs:             # Optional log entries
          - level: string   # trace|debug|info|warn|error|fatal
            message: string
            attributes:
              key: value
```

## Environment Variables

The CLI respects standard OpenTelemetry environment variables:

| Variable | Description | Overrides Flag |
|----------|-------------|----------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint | `--endpoint` |
| `OTEL_EXPORTER_OTLP_INSECURE` | Skip TLS (`true`/`false`) | `--insecure` |
| `OTEL_SERVICE_NAME` | Default service name | `--service-name` |

**Precedence:** CLI flags > Environment variables > Defaults

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
