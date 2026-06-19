# otx codebase audit — API contract, error handling, performance

**Date:** 2026-06-19
**Branch:** `audit/api-contract-errors-perf`
**Baseline:** `go build ./...` and `go vet ./...` clean.
**Method:** 7 per-subsystem readers (root config / provider / span / zaplog / nats / grpc / http),
each auditing three dimensions, followed by independent adversarial verification of every
finding (read-the-code refute pass). 33 raw findings → **31 confirmed (presented as 30 numbered
entries — M2 consolidates the two "insecure default" findings), 2 rejected** (after external Codex
review rounds 1–2, which reinstated L-C4, added L-H6, and corrected several fixes — see "Review
history" at the end).

All proposed fixes are **backward-compatible** (additive option / new method / new sentinel /
doc clarification / internal-only change). None changes an existing exported signature or the
default wire behavior. Where a "correct" default (e.g. secure-by-default) would be a breaking
change, the fix is deferred to a future major and mitigated now with docs + warnings.

Counts below are by **numbered entry** (30 entries):

| Dimension | Entries | medium | low |
|---|---|---|---|
| api-contract | 19 | 3 | 16 |
| error-handling | 6 | 2 | 4 |
| performance | 5 | 0 | 5 |
| **total** | **30** | **5** | **25** |

Notes: entries include L-C4 (reinstated from the rejected list) and L-H6 (found during review).
**M2 consolidates two underlying confirmed findings** (`exporter.go:42`, medium + `config.go:80-107`,
low), so the underlying confirmed total is **31** across these 30 entries.

No `high` severity issues survived verification. No build/vet breakage.

---

## Medium severity (5)

### M1 — Configured b3/jaeger/xray/ottrace propagators silently dropped *(error-handling)*
`propagation.go:15-24,43-48`

`knownPropagators` marks `b3,b3multi,jaeger,xray,ottrace` as known, so the unknown-propagator
warning loop skips them — but `buildPropagator` only ever installs `TraceContext` + `Baggage`.
Result: `OTEL_PROPAGATORS=b3` yields an **empty composite propagator and no diagnostic** —
cross-service context propagation silently breaks while looking configured. The package
docstring listing b3 etc. as "Supports" values makes it actively misleading. (Default
`tracecontext,baggage` works fine, so only non-W3C opt-in users are hit.)

**Fix (BC):** keep the names known but add a branch that calls `otel.Handle(...)` with
`"propagator X requires go.opentelemetry.io/contrib/propagators/...; not installed, ignoring"`.
Optionally add a registration hook option so callers can supply contrib propagators. Purely
additive — only new warning output.

### M2 — Insecure (plaintext) is the OTLP export default *(api-contract)*
`exporter.go:42`, `config.go:80-107` *(same root issue, two finders)*

`baseExporterParams` sets `Insecure: true` unconditionally and `IsInsecure()` returns true when
unset (`default:"true"`). This **inverts the OTel spec** (`OTEL_EXPORTER_OTLP_INSECURE` defaults
false), which `config.go:14-15` claims to follow. A bare `host:port` endpoint + omitted insecure
flag → telemetry (incl. credential-bearing headers) sent plaintext to a remote collector.
Mitigations already present: an `https://` scheme forces TLS regardless; defaults are localhost.

**Fix (BC):** do **not** flip the wire default now (that's a major-version change). Instead:
(1) document the plaintext default prominently on the `Insecure` field + constructors;
(2) emit a one-time `otel.Handle` warning when `Insecure` is unset against a **non-loopback**
host; (3) optionally add an opt-in `WithSecureByDefault`-style option. Flip the default in v2.

### M3 — Constructors mutate OTel globals; propagator only set by `NewTracerProvider` *(api-contract)*
`provider.go:70-73,114,159`

`New*` constructors call `otel.SetTracerProvider` / `global.SetLoggerProvider` /
`otel.SetMeterProvider` as hidden side effects. More importantly, the **global propagator is
installed only inside `NewTracerProvider`** (and that function early-returns `ErrDisabled` when
traces are off). A service that exports **metrics/logs but not traces** never gets a propagator,
so `InjectHTTP/ExtractHTTP/InjectGRPC` fall back to the no-op composite and lose context
propagation.

**Fix (BC):** export a side-effect-free `BuildPropagator(cfg) propagation.TextMapPropagator`
(mirroring the existing exported `BuildResource`) and/or a `SetupPropagator(cfg)` helper so
propagation can be installed independently of traces. Document the global mutation on each
`New*`. Additive.

### M4 — Goroutine leak in `TracedMessageBatch.Messages()` on early consumer exit *(error-handling)*
`nats/message.go:166-186`

`Messages()` spawns a goroutine that forwards into an **unbuffered** `b.msgChan` with a bare
`b.msgChan <- tracedMsg`. If the caller breaks out of `for msg := range msgs.Messages()` early
(error mid-loop, `return`, panic-recover — the exact pattern in `nats/doc.go`), nobody drains the
channel and the goroutine **blocks forever**. `b.ctx` (currently `context.Background()`) is never
consulted. (Upstream jetstream's own goroutine self-terminates — this leak is otx's forwarder.)

**Fix (BC) — corrected after review:** an internal `select { case <-b.ctx.Done() }` is **not
sufficient on its own**: `b.ctx` is derived from `startFetchSpan`'s `context.Background()`
(`consumer.go:79` → `wrapBatch`), so nothing ever cancels it when the caller abandons the
channel. The fix needs a **real cancellation source**, which requires an *additive API* (still
backward-compatible):
- add an exported `Stop()` / `Close()` method on `TracedMessageBatch` that cancels an internal
  context, and have the forwarder `select` on both the send and that context's `Done()`; **and/or**
- thread a caller-cancelable context via new ctx-accepting fetch variants (see L-N1) into
  `wrapBatch`, replacing the hardcoded `context.Background()`.

A bounded buffer alone does not fix the leak (it only delays the block). Document that callers
must drain or `Stop()` the batch. No existing exported signature changes; the cancellation entry
point is new surface.
*(Related: L-N3 below — same method also has an unguarded lazy-init race.)*

### M5 — `WithProcessSpans` is a no-op public option *(api-contract)*
`nats/options.go:50-57`

`WithProcessSpans` / `options.processSpans` is documented ("enables or disables individual
message processing spans … Default is true") but `processSpans` is **only ever written, never
read** in non-test code. `handler.go` and `message.go` create process spans unconditionally, so
`WithProcessSpans(false)` does nothing — a silent contract violation in a published library.

**Fix (BC):** actually honor the flag — when `o.processSpans` is false, skip span creation in
`MessageHandlerWithTracing` and `TracedMsg.StartProcessSpanWithTracer` (return a no-op end func).
Signature and `true` default unchanged, so only callers who already opted out see the (intended)
change.

---

## Low severity (25)

### Root package — config / exporter / provider

- **L-C1** `config.go:130-132,…` — `IsEnabled()` has **opposite zero-value semantics** across types
  (Traces = default-on; Logs/Metrics/Telemetry = default-off) under one method name. Intentional
  & field-doc'd, but the terse method docs don't say so. *Fix (BC):* add a doc sentence to each;
  optionally an explicit `IsEnabledOrDefault`.
- **L-C2** `config.go:419-450` — `ValidateEndpoints` runs only for `LoadConfig`/`ParseConfig`, not
  for programmatic `TelemetryConfig` passed to `NewTracerProvider`/`NewMeterProvider`; invalid
  scheme (`grpc://host`) silently accepted on traces/metrics path, deferred to dial failure.
  *Fix:* call the (idempotent, already-exported) `cfg.ValidateEndpoints()` inside the constructors
  before building exporters. **Compat note:** BC for all *valid* configs (no signature change, no
  change for any config that loads via `LoadConfig`); it is a deliberate **fail-fast tightening**
  for previously-accepted *invalid* programmatic configs — they now error at construction instead
  of at dial time. Not behavior-preserving for invalid input by design.
- **L-C3** `config.go:452-454` — no exported helper to build the `*bool` `Enabled` fields; users
  must reimplement the unexported `boolPtr`. *Fix (BC):* add exported `BoolPtr` / `Enabled(v bool) *bool`.
- **L-E1** `exporter.go:96-98,167-169,238-240` — unknown exporter type **silently falls back to
  OTLP** (`case "otlp"` and `default:` both call the OTLP builder). Reachable via programmatic
  config that bypasses validator tags. *Fix:* `default:` returns a wrapped new sentinel
  `ErrUnknownExporter` (keep `nop`/`noop` in the explicit case). **Compat note:** no signature
  change and unaffected for valid types (empty→otlp/console/none/nop), but it is a **fail-fast
  tightening** — a programmatic config with a typo'd type that previously produced a silent
  OTLP-to-localhost exporter now errors. Intended, not behavior-preserving for invalid input.
- **L-C4** *(reinstated from rejected list after review)* `config.go:358-373` vs `377-383` — the
  deprecated `GetOTLPEndpoint` accessor falls through to `Exporter.Endpoint` when `OTLP` is
  **non-nil but empty** (line 365-369), but `GetOTLPConfig` (which the real exporter builders use)
  returns the empty `OTLP` block as-is and never falls back to `Exporter` (line 381-383). So for
  `TelemetryConfig{OTLP:&OTLPConfig{}, Exporter:&ExporterConfig{Endpoint:"x"}}` the accessor reports
  `"x"` while the builder defaults — they disagree. Severity **low**: `GetOTLPEndpoint` is
  `Deprecated` and has **no callers** in the repo, so impact is nil today; it's a latent
  consistency trap. *Fix (BC):* make `GetOTLPEndpoint` resolve through `GetOTLPConfig` (drop the
  separate `Exporter.Endpoint` fall-through) so the accessor and builders can never diverge. No
  callers → no break. *(See corrected "Rejected" section below.)*
- **L-P1** `provider.go:19-29,43-45` — missing `ErrTracesDisabled` sentinel; traces-disabled reuses
  generic `ErrDisabled`, unlike `ErrLogsDisabled`/`ErrMetricsDisabled`. *Fix (BC) — corrected:* the
  traces-specific branch (line 44) must keep satisfying `errors.Is(err, ErrDisabled)` so existing
  callers/tests (`provider_test.go:22` tests the whole-telemetry-off path at line 39, but external
  callers may match `ErrDisabled` on the traces path) don't break. Define the new sentinel as a
  **wrapper**: `var ErrTracesDisabled = fmt.Errorf("otx: traces export is disabled: %w", ErrDisabled)`
  and return it from line 44. Then `errors.Is(err, ErrTracesDisabled)` *and*
  `errors.Is(err, ErrDisabled)` both hold — fully additive, nothing breaks.

### Root package — span / baggage

- **L-S1** `span.go:54,79` — `Span(ctx)` and `SpanFromContext(ctx)` are byte-identical duplicate
  exports. *Fix (BC):* keep both, mark one `// Deprecated:`.
- **L-S2** `span.go:12-16` — `InitTracing` doc omits that nil namer is allowed (falls back to
  default) and that re-calling is safe (atomic swap). *Fix (BC):* doc-only.
- **L-S3** `span.go:25,31,37,43,49` — span-kind helpers allocate a fresh `[]SpanStartOption` +
  re-box `WithSpanKind` on every span start. *Fix (BC):* cache kind options as package vars;
  fast-path the zero-extra-opts case (2 allocs → 1, ~8ns; marginal).
- **L-B1** `baggage.go:61-68` — `AllBaggage` allocates an unsized map even when empty. *Fix (BC):*
  pre-size with `make(map, len(members))` (the `return nil for empty` variant is **not** BC).
- **L-B2** `baggage.go:48-51` — `GetBaggage` can't distinguish absent key from empty value.
  *Fix (BC):* add sibling `LookupBaggage(ctx, key) (string, bool)`.

### zaplog

- **L-Z1** `zaplog/core.go:100-106,…` — `NewCore` panics on nil `cfg` even though `GetOTLPConfig`
  is nil-safe. *Fix (BC):* early `if cfg == nil { return …, error }` guard.
- **L-Z2** `zaplog/logger.go:39-56` — `DebugCtx`/`InfoCtx`/… call `InjectTraceFields` (allocates
  slice + boxed SpanContext) **before** zap's level gate, wasting allocs on dropped logs.
  *Fix (BC):* pre-gate with `l.Logger.Check(level, msg)` then `ce.Write(InjectTraceFields(...)...)`.
- **L-Z3** `zaplog/core.go:59-61` — `NewCore` doc implies TLS mode is per-signal overlaid; only
  endpoint+protocol are (headers/timeout/compression are also wholesale). *Fix (BC):* doc-only.
- **L-Z4** `zaplog/logger.go:18-22` — exported `Attach` panics on nil `*zap.Logger` with no
  documented precondition. *Fix (BC):* document non-nil, or `if logger == nil { return zap.New(core) }`.

### nats

- **L-N1** `nats/consumer.go:71-83,98-168` — `Fetch`/`FetchBytes`/`FetchNoWait`/`Next`/`Messages`
  take **no `context.Context`** and start the receive span from `context.Background()`, so it
  can't nest under an ambient span (diverges from `Publish`). Cross-service propagation via
  headers still works; this is span nesting only. *Fix (BC):* add `FetchContext`/`NextContext`/…
  variants + internal `startFetchSpanCtx(ctx)`; old methods delegate with `Background()`,
  deprecate in docs.
- **L-N2** `nats/consumer.go:168-186` — `Next()` discards the receive-span ctx and builds message
  ctx from `context.Background()`, inconsistent with `Fetch` (only affects header-less messages).
  *Fix:* capture `ctx, span := startFetchSpan()`, pass into `extractContext`. **Compat note:**
  **signature-compatible but NOT behavior-identical** — it changes the span parentage of
  header-less messages from `Next()` (they would descend from the receive span instead of being
  roots), and diverges from the public `NewTracedMsg` helpers which still use `Background()`. Treat
  as an intentional telemetry-behavior change, not a transparent fix; weigh whether the
  inconsistency is even worth touching (it has no correctness impact when headers are present, the
  normal case).
- **L-N3** `nats/message.go:166-171` — `Messages()` lazy `b.msgChan` init is an **unguarded
  read-modify-write**: concurrent first-calls race (`-race` detectable) and can spawn two
  forwarders draining the shared batch. *Fix (BC):* `sync.Once` or build the channel eagerly in
  `wrapBatch`. *(Pairs with M4.)*
- **L-N4** `nats/publisher.go:94-96,132-134` — after PubAck, `span.SetAttributes(publishAttributes(…))`
  reallocates and re-emits 4 already-set attributes just to add `message.id`. *Fix (BC):* set only
  `attribute.String(attrMessagingMessageID, …)`.

### http

- **L-H1** `http/client.go:123-139` — `NewClient`/`NewClientWithProviders` forward **no
  `otelhttp.Option`** to the transport, so `WithSpanNameFormatter`/`WithFilter`/etc. are
  unreachable from the convenience constructors (server-side `Handler`/`Middleware` do expose
  them — asymmetric). *Fix (BC):* add `WithOTelOptions(...) ClientOption`, forward via
  `Transport(transport, config.otelOpts...)`.
- **L-H2** `http/client.go:181-193` — `buildTransport` returns a non-`*http.Transport`
  `RoundTripper` verbatim, silently dropping all transport-level otx options. *Fix (BC):* document
  the precedence honestly; optional `WithStrictTransport`.
- **L-H3** `http/client.go:101-107` — `WithTransport` doc is grammatically wrong and states the
  **reverse** precedence (says the transport overrides otx options; code applies otx options on
  top of a clone). *Fix (BC):* correct the doc comment.
- **L-H4** `http/middleware.go:72-76,95-107` — `Middleware()` builds `otelhttp.NewMiddleware(...)`
  inside the returned closure (per handler-wrap, not per request). *Fix (BC):* hoist `mw := …`
  outside the closure.
- **L-H5** `http/middleware.go:23` — `Handler` doc example `http.Handler(myHandler, "api.request")`
  doesn't compile (shadows stdlib `http.Handler` interface on a line that also needs stdlib
  `http.Handle`). *Fix (BC):* use the `otxhttp.` alias like the other examples.
- **L-H6** *(found during review)* `http/doc.go:8` — package-doc example
  `otxhttp.Middleware("api-server")(myHandler)` passes a **string** to `Middleware`, which accepts
  only `...otelhttp.Option` (signature at `middleware.go:72`) — won't compile. *Fix (BC):* drop the string arg,
  e.g. `otxhttp.Middleware()(myHandler)` (operation name is fixed to `"http.request"` internally).
  Doc-only.

---

## Rejected by verification (2) — do not act on these

> **Correction (Codex review round 1):** a third item originally rejected here —
> "GetOTLPEndpoint resolution order no longer matches the builders" — was **wrongly rejected**.
> The cascade is *not* identical to the builders in the non-nil-but-empty-`OTLP` case (verified
> `config.go:365-369` vs `381-383`). It has been **reinstated as L-C4** above (low severity, no
> callers). The two items below remain correctly rejected.

- **`grpc/handler.go:33-43,66-76`** "rigid positional provider args can't be extended" — **false**:
  nil-as-global-fallback is documented & intentional, and the variadic `...otelgrpc.Option` is the
  designed extension path (`ServerHandler(otelgrpc.WithTracerProvider(tp))`).
- **`grpc/handler.go:80-109`** "nil-provider fallback freezes globals at construction, unlike lazy
  plain handler" — **false**: otelgrpc's `newConfig` resolves globals at construction for *both*
  paths; behavior is identical. The proposed fix is a no-op.

---

## Suggested sequencing

1. **Quick BC wins (docs + internal):** M5, L-N3, L-N4, L-Z2, L-H3, L-H5, L-H6, L-Z3, L-S2, L-C4 —
   low risk, fix real bugs or correct misleading docs.
2. **Additive API:** M3 (`BuildPropagator`), M4 (`Stop()`/ctx variant for the batch leak — needs
   new surface, not internal-only), L-B2 (`LookupBaggage`), L-P1 (`ErrTracesDisabled` wrapping
   `ErrDisabled`), L-E1 (`ErrUnknownExporter`), L-C3 (`BoolPtr`), L-H1 (`WithOTelOptions`),
   L-N1 (ctx variants — also unblocks M4's cancelable context).
3. **Diagnostics for footgun defaults:** M1 (propagator warnings), M2 (insecure warning + docs;
   defer the default flip to v2).
4. **Robustness guards:** L-Z1, L-Z4, L-C2 (validate in constructors).
5. **Behavior-changing (decide explicitly before doing):** L-N2 (changes header-less span
   parentage — confirm desired before touching).

---

## Review history

- **Round 1 (Codex `xhigh`, 2026-06-19):** report `tmp/api-error-perf-audit_codex-r1_review.md`.
  No P0 factual hallucinations. Accepted all corrections after independently re-verifying each
  against source:
  - Reinstated the GetOTLPEndpoint finding as **L-C4** (rejection reasoning was wrong for the
    non-nil-empty-`OTLP` case).
  - **M4** fix corrected: `b.ctx` is `context.Background()`-derived, so an internal `select` can't
    fire — needs an additive cancellation API (`Stop()` / ctx variant).
  - **L-P1** fix corrected: define `ErrTracesDisabled` to **wrap** `ErrDisabled` so
    `errors.Is(err, ErrDisabled)` still holds.
  - **L-N2 / L-C2 / L-E1** relabeled: signature-compatible but **behavior-changing** (L-N2) or
    **fail-fast tightening for invalid input** (L-C2, L-E1), not blanket BC.
  - Added **L-H6** (`http/doc.go:8` non-compiling `Middleware("api-server")` example).
  - Confirmed the two grpc rejections stand.
- **Round 2 (Codex `xhigh`, 2026-06-19):** report `tmp/api-error-perf-audit_codex-r2_review.md`.
  **Consensus reached — zero remaining P0/P1.** All eight round-1 corrections verified LANDED
  against source; both grpc rejections re-confirmed. Two P2 bookkeeping nits fixed in this revision:
  reconciled the entry counts (30 numbered entries / 31 underlying findings; M2 consolidates two),
  and corrected L-H6's signature line reference (`middleware.go:72`, not `:61`).
