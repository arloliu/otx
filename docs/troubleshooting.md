# Troubleshooting Guide

Common issues and solutions when using OTX.

## Traces Not Appearing

### Issue: No traces in backend

**Check 1: OTX is enabled**
```bash
echo $OTX_ENABLED  # Should be "true"
```

Or in YAML:
```yaml
telemetry:
  enabled: true  # Must be true
```

**Check 2: Exporter endpoint is correct**
```bash
echo $OTEL_EXPORTER_OTLP_ENDPOINT
# For gRPC: host:port (e.g., localhost:4317)
# For HTTP: full URL (e.g., http://localhost:4318/v1/traces)
```

**Check 3: Protocol matches endpoint**
```bash
echo $OTEL_EXPORTER_OTLP_PROTOCOL
# "grpc" for port 4317
# "http/protobuf" for port 4318
```

**Check 4: Network connectivity**
```bash
# For gRPC
nc -zv localhost 4317

# For HTTP
curl -v http://localhost:4318/v1/traces
```

**Check 5: Sampling is not off**
```bash
echo $OTEL_TRACES_SAMPLER
# Should NOT be "always_off" unless intentional
```

### Issue: Traces appear in logs but not backend

**Cause**: Using console exporter instead of OTLP

```yaml
# Wrong
traces:
  exporter: "console"

# Correct
traces:
  exporter: "otlp"
```

## Disconnected Traces

### Issue: Child spans have different trace ID

**Cause 1: Context not propagated**

```go
// ❌ Wrong: Creates new root span
func processAsync() {
    ctx, span := otx.Start(context.Background(), "Process")
    defer span.End()
}

// ✅ Correct: Propagate context
func processAsync(ctx context.Context) {
    ctx, span := otx.Start(ctx, "Process")
    defer span.End()
}
```

**Cause 2: Goroutine without context**

```go
// ❌ Wrong
go func() {
    doWork(context.Background())
}()

// ✅ Correct
go func(ctx context.Context) {
    doWork(ctx)
}(ctx)
```

### Issue: HTTP calls not linked

**Cause**: Not using traced HTTP client

```go
// ❌ Wrong: Default client
resp, _ := http.Get(url)

// ✅ Correct: OTX client
client := otxhttp.NewClient()
resp, _ := client.Get(url)
```

### Issue: gRPC calls not linked

**Cause**: Missing stats handler

```go
// ❌ Wrong: No tracing
conn, _ := grpc.NewClient(addr)

// ✅ Correct: With stats handler
conn, _ := grpc.NewClient(addr,
    grpc.WithStatsHandler(otxgrpc.ClientHandler()),
)
```

## Context Propagation Issues

### Issue: Baggage not reaching downstream

**Check 1: Propagators configured**
```bash
echo $OTEL_PROPAGATORS
# Should include "baggage": "tracecontext,baggage"
```

**Check 2: Baggage set correctly**
```go
// ✅ Must use returned context
ctx = otx.MustSetBaggage(ctx, "tenant.id", "abc")

// ❌ Wrong: Ignoring returned context
otx.MustSetBaggage(ctx, "tenant.id", "abc")  // ctx unchanged
```

### Issue: Trace context lost in NATS

**Cause**: Not using traced publisher

```go
// ❌ Wrong: Direct publish
js.Publish("subject", data)

// ✅ Correct: Traced publisher
publisher := otxnats.NewPublisher(js)
publisher.Publish(ctx, "subject", data)
```

## Initialization Errors

### Issue: `ErrServiceNameRequired`

**Cause**: Service name not configured when telemetry is enabled

```yaml
telemetry:
  enabled: true
  serviceName: ""  # ❌ Empty

# Fix
telemetry:
  enabled: true
  serviceName: "my-service"  # ✅ Required
```

### Issue: `ErrDisabled` returned

**Cause**: This is expected when telemetry is disabled

```go
tp, err := otx.NewTracerProvider(ctx, cfg)
if err != nil {
    if errors.Is(err, otx.ErrDisabled) {
        // Expected when disabled - not a real error
        return
    }
    log.Fatal(err)
}
```

### Issue: Panic on nil handler

**Cause**: Passing nil to NATS handler

```go
// ❌ Panics
handler := otxnats.MessageHandlerWithTracing(nil)

// ✅ Correct
handler := otxnats.MessageHandlerWithTracing(func(msg *otxnats.TracedMsg) {
    // Process message
})
```

## Performance Issues

### Issue: High memory usage

**Possible causes**:

1. **Not ending spans**
```go
// ❌ Span never ends, accumulates memory
ctx, span := otx.Start(ctx, "Operation")
// Missing: defer span.End()
```

2. **Not calling Shutdown**
```go
tp, _ := otx.NewTracerProvider(ctx, cfg)
// ❌ Missing shutdown - buffers never flushed
defer tp.Shutdown(ctx)  // ✅ Add this
```

### Issue: Slow request handling

**Cause**: Synchronous exporting

```yaml
# Default uses batching - should be fine
# If using console exporter in production, switch to OTLP
traces:
  exporter: "otlp"  # Batched async export
```

### Issue: Too many spans

**Solution**: Adjust sampling

```yaml
sampling:
  sampler: "parentbased_traceidratio"
  samplerArg: 0.1  # Sample 10% of root spans
```

## Validation Errors

### Issue: Invalid sampler argument

```
Error: samplerArg must be between 0 and 1
```

**Fix**:
```yaml
sampling:
  samplerArg: 0.5  # Must be 0.0 to 1.0
```

### Issue: Invalid exporter type

```
Error: exporter must be one of: otlp, console, stdout, none
```

**Fix**:
```yaml
traces:
  exporter: "otlp"  # Valid values only
```

### Issue: Invalid protocol

```
Error: protocol must be one of: grpc, http/protobuf, http
```

**Fix**:
```yaml
otlp:
  protocol: "grpc"  # or "http/protobuf"
```

## Shutdown Issues

### Issue: Traces lost on shutdown

**Cause**: Not allowing time for flush

```go
// ❌ Wrong: Immediate exit
tp.Shutdown(context.Background())
os.Exit(0)

// ✅ Correct: Allow flush time
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
tp.Shutdown(ctx)
```

### Issue: Shutdown hangs

**Cause**: Network issues with collector

```go
// Use timeout to prevent hanging
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := tp.Shutdown(ctx); err != nil {
    log.Printf("Shutdown error (timeout?): %v", err)
}
```

## Debug Logging

Enable OTel SDK debug logging:

```go
import "go.opentelemetry.io/otel"

// Set error handler for debugging
otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
    log.Printf("OTel error: %v", err)
}))
```

## Common Misconfigurations

| Symptom | Likely Cause | Fix |
|---------|--------------|-----|
| No traces | `enabled: false` | Set `enabled: true` |
| No traces | Wrong endpoint format | gRPC: `host:port`, HTTP: `http://host:port/path` |
| Disconnected traces | `context.Background()` in handler | Pass context through |
| Missing baggage | `propagators` missing `baggage` | Add `baggage` to propagators |
| Validation error | Value out of range | Check docs for valid values |
| Panic on init | Nil parameter | Check for nil before passing |

## Getting Help

1. **Check logs** for OTX or OTel errors
2. **Enable debug logging** with custom error handler
3. **Verify configuration** matches documentation
4. **Test connectivity** to collector endpoint
5. **Review context propagation** in code

## References

- [OpenTelemetry Troubleshooting](https://opentelemetry.io/docs/collector/troubleshooting/)
- [OTLP Specification](https://opentelemetry.io/docs/specs/otlp/)
