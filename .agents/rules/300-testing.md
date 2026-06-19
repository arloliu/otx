# 300 - Testing

- Table-driven tests with `testify` (`require` for fatal, `assert` for soft). Use
  `t.Context()` and `t.Setenv()` (never `os.Setenv`); no emojis in test output.
- Config loader: cover YAML + env-var precedence and protocol-aware endpoint defaults;
  isolate env with `t.Setenv` so cases don't leak into one another.
- OTel pipelines: verify in-memory — `tracetest.SpanRecorder` / in-memory exporters, the
  noop providers, and `otel/sdk` test exporters — rather than asserting against a live
  collector.
- Async / concurrency (tracker, NATS consumers, provider shutdown): do NOT `time.Sleep` to
  wait for state. Observe or subscribe before triggering, then assert on the collected
  result. Run concurrency-sensitive tests with `go test -race`.
- Clean up every provider, NATS connection, and exporter in `defer`/`t.Cleanup`.
