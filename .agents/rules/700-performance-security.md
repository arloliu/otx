# 700 - Performance and Security

Apply when editing the per-request / per-log paths (span helpers, HTTP/gRPC middleware,
NATS handlers, `zaplog` core), endpoint/config handling, or any code touching credentials
or network transport.

## Performance

otx is mostly setup/config code, but a few paths run on every request or log line — keep
those allocation-light:

- **Span helpers & middleware** (`Start*`, `SetAttributes`, the HTTP/gRPC interceptors) run
  per request. Avoid per-call allocations beyond the attributes the caller asked for;
  pre-size attribute slices with `make([]attribute.KeyValue, 0, n)` when the count is known.
- **`zaplog` core** sits on every log call and delegates to zapwire's OTLP data plane.
  Preserve that single-encode, bounded-queue, counted-drop contract — don't add buffering
  or extra conversions in front of it.
- **Config & provider construction** run once at startup — favor clarity over
  micro-optimization there.
- **Profile before optimizing:** use `pprof` to find the real bottleneck, don't guess.

## Security

- **Validate external config.** Endpoints, headers, timeouts, and sampler args come from
  YAML/env; normalize and bound them. Endpoint classification lives in `internal/endpoint` —
  reuse it, don't re-parse ad hoc. Fail closed on a nil or invalid config.
- **Transport:** use TLS for OTLP/HTTP/gRPC export to remote collectors; honor the
  `Insecure`/scheme semantics already in the config. Support NATS credential-based auth
  where applicable.
- **Never log or leak secrets.** Endpoints may embed credentials or headers — error and
  debug output must not echo them.
- **Never commit secrets** (test fixtures included).
