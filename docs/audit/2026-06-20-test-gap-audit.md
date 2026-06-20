# otx test-gap audit — integration & end-to-end coverage

**Date:** 2026-06-20
**Branch:** `test/integration-test-gaps`
**Method:** 8 analysis agents (7 subsystems + a cross-component/infra lens) mapped coverage
gaps from source + tests; every claimed gap was adversarially verified against the actual
`*_test.go` (grep/read) to confirm it is genuinely uncovered. 37 raw → **34 confirmed, 3 rejected**.

| Kind | Confirmed | high | medium | low |
|---|---|---|---|---|
| integration | 25 | 8 | 9 | 8 |
| e2e | 5 | 3 | 1 | 1 |
| unit | 4 | 0 | 1 | 3 |
| **total** | **34** | **12** | **13** | **9** |

## The headline

otx **does** have genuine on-the-wire integration testing — but **only in `zaplog`**
(`zaplog/core_test.go` + `helper_test.go` stand up real OTLP HTTP and gRPC capture servers and
assert exported `ExportLogsServiceRequest`s). Everywhere else, the core data paths are asserted
only with mocks or one-sided in-process tests:

- **NATS** instrumentation is **100% mock-based** (`mockMsg`/`mockConsumer`/`mockBatch`/
  `mockJetStream`) — no real broker, ever.
- **gRPC** tests build a bufconn client+server but **never invoke an RPC**, so zero spans are
  produced and the in-memory exporter is never read.
- **HTTP** tests exercise client and server **separately** (bare `httptest.Server` / synthetic
  `httptest.NewRequest`) — never a real client→server round-trip.
- **Trace & metric export** is never shipped to a listener — only logs are wire-tested. The
  `nop` exporter + in-memory `SpanRecorder` bypass the `WithBatcher`→OTLP→wire pipeline.

There is no `test-integration` / `test-race` make target, and no embedded NATS server harness.
(The "no integration infrastructure at all" framing was *rejected* — `zaplog` proves the pattern
exists; it just isn't applied elsewhere.)

**Hard constraint (load-bearing):** `github.com/arloliu/otx` must **not** gain a dependency on
`github.com/nats-io/nats-server/v2`. The server is a heavy transitive tree, and pulling it into
the root `go.mod` — even as a test-only require — would inflict it on every downstream importer of
otx. The embedded-NATS tests therefore live in a **separate nested Go module** (own `go.mod`); see
"Test infrastructure decisions" below.

---

## Build-out status

Tracking which gaps have landed (root module, no new dependency unless noted):

- **Closed:** H6, H7, M6 (`propagation_test.go`); M4, M5, M13 (`config_env_test.go`,
  `config_validation_test.go`; M5 included a production fix — see below); H1, M1, M2, M3, M12
  (`exporter_e2e_test.go` — OTLP/HTTP + OTLP/gRPC capture servers); H5, M11
  (`http/roundtrip_test.go`); H4, M10 (`grpc/roundtrip_test.go` — bufconn + bundled health RPC);
  H2, H3 (`nats/internal/integration/` nested module — embedded NATS server; run via `make test`,
  which races both modules, or `make test-integration` for the nested module alone). Infra landed:
  Makefile targets, CI steps, dependabot for both modules.
  L1, L9 (`internal/tracker/tracker_test.go` — namer reaches span name; concurrent Set/Start under
  `-race`); L8 (`config_errors_test.go`); L10 (`http/buildtransport_test.go`);
  M8, M9 (`nats/internal/integration/methods_test.go` — the previously 0%-covered shipping methods
  PublishAsync/PublishAsyncMsg, FetchContext/FetchBytesContext/FetchNoWaitContext, the
  MessagesWithContext iterator with Next/Stop/Drain, and the handler-callback Consume, all against
  the real broker);
  H8 (`zaplog/grpc_trace_test.go` — trace_id/span_id + resource identity verified over the gRPC log
  transport, not just HTTP).
- **Pending (low value):** L3/L4/L5 (mock-coverable low-risk passthroughs); L6 (otlp-sim engine as an
  e2e harness).

**Defects discovered while building coverage (beyond the original findings):**

- **D1 (fixed):** map-typed env vars rejected the OTel `key=value` form — see M5.
- **D2 (fixed):** when the OTLP endpoint is given in **URL form** (`http://host:port`, no path), the
  **log** HTTP exporter (`otlploghttp` v0.20.0) POSTed to `/` instead of `/v1/logs`, so
  `NewLoggerProvider` logs would 404 against a real collector. The **trace/metric** exporters
  (v1.x, shared `otlpconfig`) correctly default the path; `otlploghttp`'s `WithEndpointURL` uses the
  URL path verbatim. **Fixed** in `exporter.go` (`withLogsPath`): otx appends `/v1/logs` to a
  URL-form log endpoint that carries no explicit path (bare `host:port` and explicit-path URLs
  unchanged; forward-safe if the SDK later defaults it). Covered by `exporter_logspath_test.go` +
  `exporter_e2e_test.go`.

  *Root cause (otlploghttp `v0.20.0` config.go):* path resolution chains resolvers that each
  short-circuit with `if s.Set { return s }`, including the `fallback("/v1/logs")` default.
  `WithEndpointURL` does `c.path = newSetting(u.Path)`, and `newSetting` marks the setting `Set:true`
  even when the URL path is empty (`Value:""`). So the `/v1/logs` fallback is skipped and the client
  POSTs to `Path:""` → `/`. `WithEndpoint(host)` leaves the path unset (fallback fills it) and the
  env-var endpoints run through path converters, so only the programmatic `WithEndpointURL` with a
  path-less URL regresses. The trace/metric exporters are a separate codebase (shared `otlpconfig`,
  `otlptrace`/`otlpmetric` v1.x) whose `WithEndpointURL` defaults the empty path, which is why they
  are unaffected.

  *Why not bump otlplog (the rejected Option B):* "bump" = raise the dependency to a newer version.
  But `otlploghttp` `v0.20.0` is the **latest published release** (otel core `v1.44.0` is also
  latest) and still has this behavior — there is no newer version to upgrade to. Even a future
  release fixing it would itself be an upstream behavior change; `withLogsPath` is currently the only
  available fix and remains correct (no double-append) if the SDK later defaults the path.

---

## High severity (8 distinct; dedup of 12 findings)

### H1 — Trace & metric export never verified over the wire *(e2e/integration)*
`exporter.go:212,292,362` (builders), `provider.go:82,137,192` (pipeline)
`buildOTLP{Trace,Log,Metric}Exporter` construct real SDK clients and the providers wire them,
but **no test emits a span/metric through an otx provider and asserts it arrives at a listener**.
`provider_test.go` uses `nop`/`none` only; `exporter_test.go` asserts option *shape* via mock
constructors (the test comment itself admits "the scheme→TLS flip happens inside the SDK …
which the helper cannot observe"). Logs *are* wire-tested (`zaplog`), so this is an asymmetry.
**Fix:** add `exporter_e2e_test.go` — an `httptest.Server` decoding `ExportTraceServiceRequest`
(mirror `zaplog/helper_test.go`'s gzip-aware decoder), point `NewTracerProvider` at it, emit a
span, `ForceFlush`/`Shutdown`, assert `/v1/traces` received `service.name` + span. Repeat for
metrics. *(covers the two overlapping findings: "no e2e OTLP export" + "TracerProvider never
exports a span".)*

### H2 — NATS publish→consume round-trip: trace context never crosses the wire *(integration)*
`publisher.go:64-139`, `consumer.go:269-280`, `handler.go:58-63`
Producer-inject and consumer-extract are each tested in isolation against an **in-process
`nats.Header` map**; no test publishes to a broker and consumes the delivered message. Mocks
can't reveal JetStream header case-folding, `Nats-*` reserved-header collisions, or whether the
stream store/delivery strips `traceparent`/`tracestate`.
**Fix (nested module `nats/internal/integration`):** embedded in-process `server.NewServer` +
JetStream (temp `StoreDir`); real `NewPublisher`/`WrapConsumer`; start a parent span, `Publish`,
`FetchContext`/`Next`, assert the consumed `TracedMsg.Context()` shares the producer `TraceID` and
the process span parents under it (via `SpanRecorder`).

### H3 — NATS Ack/Nak/Term/DoubleAck & redelivery never exercised *(integration)*
`message.go:41-52`, `handler.go:107-115`, `consumer_test.go:33-39`
Mock `Ack`/`Nak`/`Term`/`DoubleAck` return `nil` unconditionally. No test acks/naks a delivered
message or asserts redelivery. The panic-recovery path is tested only for span error recording,
never for the real consequence (un-acked message redelivered → second process span). `TracedMsg`
embeds `jetstream.Msg`, so these pass straight to the broker, unverified.
**Fix (nested module `nats/internal/integration`):** embedded JetStream — Nak (or panic) without
Ack, re-fetch, assert redelivery + a second process span; assert Ack advances the consumer (no
redelivery).

### H4 — gRPC: handlers never produce a span; cross-RPC propagation unverified *(integration)*
`grpc/handler.go:19-76,102-106`, `grpc/handler_test.go:21-128`
All three tests build a bufconn client+server then stop at `assert.NotNil(conn)`. `grpc.NewClient`
is lazy — **no RPC is invoked**, so otelgrpc's stats handlers fire on nothing; the in-memory
exporter is never queried. The entire reason the package exists (SERVER/CLIENT span emission +
client→server parent linkage) is untested.
**Fix:** register `google.golang.org/grpc/health` (already a dep — no proto codegen), invoke
`Check()` over bufconn with separate client/server `TracerProvider`s, assert a Client-kind and
Server-kind span share a `TraceID` and `server.Parent.SpanID == client.SpanID`. Add a
no-client-handler negative case (server span roots independently). *(covers the duplicated
"bufconn never invokes RPC" + "propagation across RPC" findings.)*

### H5 — HTTP: no client→server trace-propagation round-trip *(e2e)*
`http/client.go:153-205`, `http/middleware.go:24-111`
Every test wires one side only: client tests hit a *bare* `httptest.Server` (un-instrumented
handler); middleware tests use synthetic `httptest.NewRequest` (no client, no real header) → the
server span is always a root. No test sends an otx client through an otx-instrumented
`httptest.NewServer` and checks parentage.
**Fix:** one shared exporter+`TracerProvider`+`TraceContext{}`; wrap the handler with
`MiddlewareWithProviders`, serve via `httptest.NewServer`, call with `NewClientWithProviders`,
assert two spans share a `TraceID` and `server.Parent.SpanID == client.SpanID` (find spans by
`SpanKind`, not index). Assert `r.Header.Get("traceparent") != ""` inside the handler.

### H6 — gRPC `InjectGRPC`/`ExtractGRPC` + `metadataCarrier` have zero tests *(integration)*
`propagation.go:104-137`
No caller, no test anywhere. The carrier adapter's multi-value `Get`, `Set`, and `Keys` are
entirely unexercised — a regression (empty `Keys`, wrong `Get` value) would silently break gRPC
context propagation.
**Fix:** in `propagation_test.go`, inject into a `metadata.MD`, assert `traceparent` present,
`ExtractGRPC` round-trips the `SpanContext`; plus a direct `metadataCarrier` unit case. (In-process;
no server needed.)

### H7 — Baggage never survives an Inject→carrier→Extract boundary *(integration)*
`propagation.go:82` (composes `Baggage{}`), `baggage_test.go`
All baggage tests operate on the **same** context object; none serializes through a carrier.
`propagation_diag_test.go` only checks `"baggage"` is in `Fields()` — a weak proxy that passes
even if values silently fail to round-trip.
**Fix:** `SetBaggage` → `InjectHTTP(h)` → `ExtractHTTP` → assert `GetBaggage`/`AllBaggage` recover
the value; plus a "dropped when only tracecontext configured" case. (In-process `http.Header`.)

### H8 — zaplog gRPC transport never verifies `trace_id`/`span_id` on a `LogRecord` *(e2e)*
`zaplog/core.go:146-148`, `core_test.go:114-163`
The one gRPC capture-server test logs via plain `logger.Info` with **no span context** and asserts
only the body + `service.name`. Every `trace_id`/`span_id` assertion runs against the **HTTP**
transport. The gRPC path is a separate hand-rolled OTLP/gRPC client, so HTTP coverage doesn't
transitively prove it.
**Fix:** extend the gRPC suite — `ctxWithTrace` + `InfoCtx`, assert `GetTraceId()`/`GetSpanId()`
on the captured `ExportLogsServiceRequest`; add a gRPC resource-parity case.

---

## Medium severity (13)

- **M1** `exporter.go:454-512` — custom headers / timeout / **gzip compression** never verified
  on the wire (only option-shape via mocks). *Fix:* assert `Content-Encoding: gzip` + custom header
  on the H1 httptest server; body gzip-decodes to a valid request.
- **M2** `provider.go:85,139,192` — **Shutdown flush-on-shutdown, double-Shutdown idempotency,
  Shutdown error propagation** untested at the otx provider level (`span_test.go` discards the
  error on `nop`). *Fix:* emit N spans without flush, `Shutdown`, assert all N arrived; failing-
  exporter wrapper for error surfacing.
- **M3** `exporter.go:213,293,363` — **gRPC-vs-HTTP transport selection** and **TLS-vs-insecure on
  the wire** never observed against a server (only option shape). *Fix:* plaintext grpc capture
  server for `Protocol:grpc`; `httptest.NewTLSServer` for `https://`/`Insecure=false`.
- **M4** `config.go` (~30 env-tagged fields) — **YAML+env precedence matrix** tested for exactly
  one field (`OTEL_SERVICE_NAME`). `OTEL_EXPORTER_OTLP_{ENDPOINT,PROTOCOL,INSECURE}`,
  `OTEL_TRACES_SAMPLER[_ARG]`, `OTEL_PROPAGATORS`, etc. unverified through `LoadConfig`. *Fix:*
  table-driven file-A/env-B "env wins" + default-fill, covering the `*bool` and `float` cases.
- **M5** `config.go:34,95,267` — **map-typed env parsing** (`OTEL_RESOURCE_ATTRIBUTES`,
  `OTEL_EXPORTER_OTLP_HEADERS`) never exercised. *Reproduction upgraded this from a coverage gap to
  a **defect**:* the docs (and the OTel spec) mandate `key=value`, but the underlying loader (fuda
  `convertMap`) only accepts `key:value`, so the standard
  `OTEL_RESOURCE_ATTRIBUTES=service.version=1.0.0` made `LoadConfig` fail. **Fixed:** otx now parses
  these two env vars itself (`config_loader.go` `parseMapEnv`/`applyMapEnvOverrides`), accepting the
  spec `key=value` form (first `=` wins) and still accepting legacy `key:value`; env replaces file,
  and headers apply only when their block exists (prior behavior preserved). Covered by
  `config_env_test.go`.
- **M6** `propagation.go:95,100` — **`InjectHTTP`/`ExtractHTTP` round-trip** has zero direct
  coverage (public W3C-over-HTTP API; thin but core). *Fix:* in-process header round-trip asserting
  `TraceID`/`SpanID`/sampled.
- **M7** `zaplog/core.go:138-140` — **`DrainTimeout` bounded-drain** is wiring-tested only against
  a fast server; the actual time-bound + `DroppedLogs() > 0` on a stalled collector is unproven.
  *Fix:* blocking `httptest` handler, small `DrainTimeout`, assert `Sync`/`Close` returns in-bound
  and dropped > 0.
- **M8** `publisher.go`, `consumer.go:118-257` — Publisher/Consumer tests run against **hand-copied
  reimplementations** (`testPublisher`/`testConsumer`), not the shipping code; `PublishAsync*`,
  `FetchBytesContext`, `FetchNoWaitContext` have **0% real coverage**. *Fix:* re-point assertions at
  `NewPublisher`/`WrapConsumer` (existing mocks suffice — not actually integration) + cover the 0%
  methods.
- **M9** `consumer.go:201-223,261-266`, `message.go:243-273` — continuous-consumption iterator
  (`MessagesWithContext`/`TracedMessagesContext.Next/Stop/Drain`, `Consume`) has no real coverage;
  **Drain in-flight-then-stop ordering** is the consequential untested piece.
- **M10** `grpc/handler.go:90-106` — **nil-provider global fallback** exercised only at
  construction; no test proves a span lands in the *global* exporter at runtime. *Fix:* folds into
  the H4 RPC test with a global `InMemoryExporter`.
- **M11** `http/transport.go:67` — **outbound `traceparent` injection** never asserted at the wire
  (handlers discard `r.Header`). *Fix:* capture `r.Header.Get("traceparent")` in the H5 handler.
- **M12** cross-signal — **traces & logs never verified to reach a collector with identical
  resource identity** (the keystone design promise of exported `BuildResource`). Resource parity is
  asserted on the *log* side only. *Fix:* combined trace+log capture, assert `ResourceSpans` attrs
  == `ResourceLogs` attrs.
- **M13** `config.go:22,100-247` — **struct-tag validation** (`oneof`/`gte`/`lte`/`required_if`)
  test coverage existed only for `logs.protocol`/`logs.minLevel`. *Correction:* reproduction showed
  the audit's claim that `required_if` on `ServiceName` "is enforced only later at build time" was
  **wrong** — `ParseConfig` already rejects `enabled:true` with no `serviceName`, and also catches
  `samplerArg` out of `[0,1]`, bad `otlp.protocol`, negative `otlp.timeout`, and bad
  `traces.exporter`. So M13 was a pure coverage gap, now closed by negative table tests in
  `config_validation_test.go`.

## Low severity (9)

`L1` custom `SpanNamer` never verified to reach the recorded span name (`tracker.go:47`) ·
`L2` zaplog resource parity asserted by hardcoded literals, not vs `BuildResource` output ·
`L3` `FetchContext` parent-ctx-cancel + `batch.Error()` non-nil passthrough untested (mock-coverable) ·
`L4` `headerCarrier` multi-value/reserved-header behavior (low risk — NATS doesn't canonicalize) ·
`L5` transport pool/timeout options not verified to survive `NewClient`→otel wrapping ·
`L6` otlp-sim `Engine` is a ready-made E2E harness never pointed at a listener ·
`L7` config struct-tag negatives (see M13 overlap) — *escalated to M13* ·
`L8` malformed-input / missing-file paths in `LoadConfig`/`ParseConfig` untested ·
`L9` `internal/tracker` has no test file; concurrent `Set`/`Start` never run under `-race` ·
`L10` `buildTransport` opaque-`RoundTripper` branch (`client.go:228`) untested.

---

## Rejected by verification (3) — do not act on these

- **"No integration infrastructure at all"** — *false*: `zaplog`'s `newGRPCCaptureServer`
  (`core_test.go:74`) is a real embedded gRPC OTLP server asserting on-the-wire logs in the default
  suite; `grpc/handler_test.go` drives real bufconn machinery. The infra *pattern* exists; the gap
  is that it's not applied to nats/grpc-RPC/http/trace-export. (The narrow true points — no
  `integration` tag, no `-race` target, NATS is mock-only — are captured above as H2/H3 and the
  recommendation below.)
- **"NATS publish→consume only mocked (no real header serialization)"** — *downgraded/rejected as
  framed*: producer-inject and consumer-extract **are** each covered against the **real**
  `headerCarrier`/`TraceContext` with a serialized `traceparent` string
  (`publisher_test.go:400`, `consumer_test.go:226,298`, `handler_test.go:92`). The only otx-owned
  propagation code is already exercised; an embedded server mainly tests nats.go's serialization.
  *(The distinct, genuine NATS gaps — full round-trip H2, ack/redelivery H3 — remain.)*
- **"Deprecated Sampling/Exporter fallback accessors uncovered"** — *false*: both deprecated-block
  branches are exercised end-to-end via `TestSpanHelpers` and `TestNewTracerProvider` (deprecated
  top-level `Sampling`/`Exporter` with no `Traces` block).

---

## Test infrastructure decisions

Two design questions were settled before build-out. Both reduce to one principle: **keep the root
module's dependency set unchanged, and gate slow/heavy tests by module boundary rather than by a
build tag.**

### Decision 1 — one integration lane, not separate `integration`/`e2e` targets

The `integration`/`e2e` labels above are a *prioritization* lens (how much of the stack a gap
exercises), **not** a runtime property: every gap here runs in-process, so there is no "deploy a
real collector" tier to justify a second slow lane. We therefore use **one** integration concept,
gated by *"does the test need a spun-up server process?"*:

- **Default `make test` (root module, untagged).** Fast unit tests **plus** the in-process
  round-trips that need **no new dependency** — httptest OTLP capture (H1, M1, M2, M3, M12),
  HTTP client→server (H5, M11), gRPC bufconn + `google.golang.org/grpc/health` RPC (H4, M10),
  and the pure in-process propagation gaps (H6, H7, M6, M4, M5, M13, L*). This matches the existing
  `zaplog` precedent, which already wire-tests log export in the **default** suite. No
  `//go:build integration` tag is introduced for these — they are deterministic and sub-second.
- **`make test`.** Runs `go test -race ./...` on the root module (closes the L9 tracker race) **and**
  `go test -C nats/internal/integration -race ./...` for the nested module (NATS forwarder / `Stop`
  concurrency) — so `-race` and the embedded-NATS suite are always part of the default gate.
- **`make test-integration` (nested module only).** The embedded-NATS JetStream tests (H2, H3) under
  `-race`, for focused iteration.

Rationale for no build tag: with NATS isolated in its own module (below), module boundary already
excludes the heavy tests from the default `go test ./...`. A tag would be redundant machinery, and
the in-process tests are fast enough to keep in the default suite where they won't rot.

### Decision 2 — embedded in-process nats-server, isolated in a nested module

For a real NATS broker we use the **embedded in-process server** (`nats-server/v2/server`:
`server.NewServer(&server.Options{Port: -1, JetStream: true, StoreDir: t.TempDir()})` →
`go s.Start()` → `s.ReadyForConnections(...)`), **not** testcontainers. Embedded needs no Docker
(critical for CI/sandbox), starts in ~100–300 ms, is deterministic, is what nats.go's own tests
use, and gives full fidelity for the only things otx asserts (client-side header propagation,
ack/redelivery, fetch semantics). Testcontainers would add Docker as a hard requirement and a
second heavy dependency for zero added fidelity here.

**The server dependency must never reach the root `go.mod`.** It is confined to a **nested Go
module** so `github.com/arloliu/otx` and all its downstream importers stay free of `nats-server`:

```
nats/internal/integration/
  go.mod          # module github.com/arloliu/otx/nats/internal/integration
                  #   require github.com/arloliu/otx (replace => ../../..)
                  #   require github.com/nats-io/nats-server/v2  ← lives ONLY here
                  #   require github.com/nats-io/nats.go, go.opentelemetry.io/otel/..., testify
  server_test.go        # embedded-server + JetStream helper
  roundtrip_test.go     # H2 — publish→consume, trace context crosses the wire
  ack_redelivery_test.go# H3 — Ack/Nak/Term/DoubleAck + redelivery → second process span
```

Properties:
- `replace github.com/arloliu/otx => ../../..` — the module tests the **shipping** `otx/nats`
  public API against the local checkout; it is never published or imported by anyone.
- It is invisible to the root module: `go test ./...`, `go build ./...`, and `make lint` do **not**
  descend into nested modules, so the root dependency graph and lint surface are unchanged.
- `make test` runs `go test -race ./...` and `go test -C nats/internal/integration -race ./...`
  (both modules, race); `make test-integration` runs only the nested module under `-race`. The
  nested module is linted separately (`make lint-integration`, via `-C`), not in `make lint`.
- Placed under `nats/internal/` to signal "not importable / test-only." Import direction is legal:
  Go's `internal` rule only blocks packages *outside* the subtree rooted at `nats/` from importing
  `nats/internal/...`; a module *inside* that subtree importing the public `github.com/arloliu/otx/nats`
  is fine.

This is the standard isolation pattern for heavy test-only deps (otel-contrib, grpc-go interop use
per-component `go.mod`s the same way). It fully satisfies the dependency policy: no new entry in the
root `go.mod`, nothing to approve there.

### Nested-module `go.mod` authoring

A `replace` to a local path still requires a matching `require` line (a bare `replace` without a
`require` is a `go mod tidy` error on Go ≥1.17). Use a placeholder version — the `replace` overrides
it, and the module is never published:

```go.mod
module github.com/arloliu/otx/nats/internal/integration

go 1.25.0

require (
    github.com/arloliu/otx v0.0.0-00010101000000-000000000000 // replaced below
    github.com/nats-io/nats-server/v2 vX.Y.Z
    github.com/nats-io/nats.go vX.Y.Z          // keep pinned to the root module's version
    github.com/stretchr/testify v1.11.1
    go.opentelemetry.io/otel/sdk vX.Y.Z        // sdk/trace + tracetest for SpanRecorder
)

replace github.com/arloliu/otx => ../../..
```

Run `go mod tidy` inside the nested module after authoring; the `v0.0.0-...` pseudo-version is the
conventional placeholder for a locally-replaced require.

### CI & maintenance wiring (must land with the targets)

The nested module is invisible to the root module by design — which also means **CI will silently
never run it** unless explicitly wired. Required changes:

- **`Makefile`:** make `test` race both modules
  (`go test -race ./...` && `go test -C nats/internal/integration -race ./...`) and add
  `test-integration: go test -C nats/internal/integration -race ./...` for the nested module alone.
- **`.github/workflows/ci.yml`:** today it runs only `make lint` + `make test`. Ensure `make test`
  (now race + both modules) runs, plus a lint step for the nested module. Use the
  repo's linter wrapper, not a bare PATH binary: `make lint` runs
  `go tool -modfile=linter.go.mod golangci-lint run`, so the nested step should be the same wrapper
  with `-C nats/internal/integration` on the `go` command (or an equivalent `cd`), since `make lint`
  does not descend into nested modules.
- **Version skew:** with two `go.mod`s, the nested module's `nats.go` / OTel SDK versions can drift
  from the root's. There is no dependabot config in the repo today. Either add a `.github/dependabot.yml`
  covering **both** module directories (`/` and `/nats/internal/integration`), or adopt a documented
  manual discipline: when the root module bumps `nats.go` or an OTel dependency, re-run `go mod tidy`
  in the nested module in the same change. Keep the shared deps (`nats.go`, `testify`, OTel SDK)
  pinned to the root module's versions to avoid testing against a different client than ships.

## Build-out order

1. **In-process, no-new-dep gaps first** (highest coverage per line, zero infra risk):
   H6, H7, M6, M4, M5, M13, L* — pure propagation/config round-trips in the root module.
2. **OTLP capture-server helper (traces+metrics)** — generalize `zaplog/helper_test.go`'s gzip-aware
   decoder to `ExportTraceServiceRequest`/`ExportMetricsServiceRequest`. Unblocks **H1, M1, M2, M3,
   M12** in the root default suite.
3. **HTTP & gRPC round-trip harnesses** — `httptest.NewServer` both sides; bufconn + `grpc/health`.
   Unblocks **H4, H5, M10, M11** in the root default suite.
4. **Make `make test` race both modules and add `make test-integration`**, then scaffold the nested
   module and land **H2, H3** plus **M8/M9** (PublishAsync/PublishAsyncMsg, the fetch variants, the
   Messages iterator, and Consume) against the embedded broker — the highest-fidelity place to
   exercise the shipping API.
