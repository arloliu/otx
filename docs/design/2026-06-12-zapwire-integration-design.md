# zapwire integration — design (otx)

**Status:** ✅ implementation-ready (codex design-review consensus, pass 3) · **Date:** 2026-06-12 · **Branch:** `worktree-zapwire-integration`

**Relates to:** `github.com/arloliu/zapwire` (the zap log data plane; its `otlp/` subpackage
design lives at `zapwire/docs/design/2026-06-11-otlp-logs-design.md`, PR zapwire#4) and
otx's existing `NewLoggerProvider` (provider.go — the OTel SDK logs pipeline).

---

## 1. Boundary analysis (why these are two libraries)

| | **otx** | **zapwire** (+ `otlp/`) |
|---|---|---|
| Role | Telemetry **control plane**: config-driven wrapper over the official OTel SDK | Log **data plane**: zap-native shipping to log processors |
| Owns | TracerProvider, MeterProvider, LoggerProvider, sampling, W3C propagation, HTTP/gRPC middleware, NATS integration, span ergonomics | zap encoders/cores/WriteSyncers for Fluent/NDJSON/syslog/OTLP; bounded queues, counted drops, at-most-once |
| Dependency weight | Heavy by design (otel SDK + exporters, grpc, protobuf, contrib, NATS) | Near-zero (root: stdlib+zap; `otlp/`: + `otel/trace` only) |
| Logging stance | Logger-agnostic, no zap anywhere | zap-only by definition |

The libraries compose at exactly one seam: **otx creates and propagates the span context;
zapwire consumes it.** An otx middleware/`Start*` helper puts a `trace.SpanContext` into
ctx; zapwire's `otlp.SpanContext(ctx)` / `zap.Any("context", ctx)` / injectors extract it
via `trace.SpanContextFromContext` — the same stable `go.opentelemetry.io/otel/trace` API
both already depend on. Neither imports the other today, and the **dependency direction
rule is permanent: otx MAY depend on zapwire; zapwire must NEVER depend on otx.**

### The one genuine overlap: two OTLP-logs pipelines

A zap service can reach OTLP two ways:

1. **SDK path:** zap → contrib otelzap bridge → `otx.NewLoggerProvider` (sdk/log batch
   processor) → `otlploghttp`/`otlploggrpc`. Full SDK feature set and a gRPC option, but
   double-converts every record (zap field → `log.KeyValue` → proto) and drags
   grpc+protobuf into the binary.
2. **zapwire path:** zap → `zapwire/otlp.NewCore` → its own async HTTP exporter. Single
   encode pass (~228 ns / 8 allocs per record, byte-identity protobuf), no SDK/grpc/
   protobuf, at-most-once with counted drops — but OTLP/HTTP only.

**Routing rule (to document in both repos):** services that log with zap ship logs via the
zapwire path; `otx.NewLoggerProvider` remains for non-zap paths (slog bridge, direct OTel
log API) or when gRPC OTLP for logs is a hard requirement. Never run both for one logger.

### Correlation invariant

Logs↔traces joining requires **identical resource identity** (`service.name` at minimum)
on both pipelines. Today otx's `buildResource` and zapwire's `WithServiceName`/
`WithResource` are configured independently — the integration's main job is to derive both
from one config.

Trace sampling stays trace-side: otx samples traces; zapwire logs everything (emitting
trace IDs even for unsampled spans, flags omitted) — the OTel-recommended shape. The log
cost lever is level gating, not trace sampling.

## 2. Goal

One otx config (`TelemetryConfig`), one resource identity, one shutdown story — with zap
logs flowing over zapwire's lean OTLP exporter, and the `InfoCtx`-style ergonomics that
zapwire deliberately leaves to the app layer provided here, once.

## 3. Design

### 3.1 New subpackage `otx/zaplog` (same module)

A small adapter package inside the otx module (no separate go.mod: otx is already heavy;
`zapwire/otlp` adds only zap + `otel/trace`, both light).

**Resource access (pass-1 P0):** `buildResource` is unexported in package `otx`
(`provider.go:168`), so a subpackage cannot call it. This work therefore **exports a thin
wrapper in package `otx`**:

```go
// BuildResource returns the OTel resource derived from cfg — the same
// resource NewTracerProvider/NewLoggerProvider/NewMeterProvider attach.
// Exported for advanced wiring (custom SDK components, otx/zaplog).
func BuildResource(ctx context.Context, cfg *TelemetryConfig) (*resource.Resource, error)
```

`zaplog` consumes `otx.BuildResource` and translates `resource.Attributes()` into
zapwire options (§3.3). Providers keep calling the unexported form internally.

```go
package zaplog // github.com/arloliu/otx/zaplog

// NewCore builds a zapwire OTLP core + writer from otx telemetry config.
// Resource identity (service.name + resource attributes) is derived from the
// same buildResource used for traces/metrics, so logs and traces join.
func NewCore(ctx context.Context, cfg *otx.TelemetryConfig, level zapcore.LevelEnabler,
    opts ...otlp.Option) (zapcore.Core, *otlp.Writer, error)

// Attach tees the OTLP core onto an existing zap logger (zap.WrapCore +
// zapcore.NewTee). The returned logger writes to both sinks; level gating on
// the OTLP side is whatever `level` NewCore received (use zap.AtomicLevel for
// a runtime cost dial).
func Attach(logger *zap.Logger, core zapcore.Core) *zap.Logger

// Logger wraps *zap.Logger with ctx-aware methods built on zapwire's
// injectors — the app-layer surface zapwire deliberately omits.
type Logger struct{ *zap.Logger }

func (l Logger) DebugCtx(ctx context.Context, msg string, fields ...zap.Field)
func (l Logger) InfoCtx(ctx context.Context, msg string, fields ...zap.Field)
func (l Logger) WarnCtx(ctx context.Context, msg string, fields ...zap.Field)
func (l Logger) ErrorCtx(ctx context.Context, msg string, fields ...zap.Field)
// (sugared variants over otlp.InjectTraceKVs as needed)
```

`InfoCtx` etc. are one-liners over `otlp.InjectTraceFields(ctx, fields...)` — they work on
ANY core composition (the injected field is consumed by the otlp encoder and rendered
legibly by tee'd console/JSON cores).

### 3.2 Endpoint, protocol & transport-option mapping (pass-1 P1 ×2)

otx endpoints are gRPC-style `host:port` (default `localhost:4317`, default protocol
`grpc` on the shared `OTLPConfig`); zapwire/otlp needs `http(s)://host:port` and is
HTTP-only. Today `LogsConfig` has only `Enabled`/`Exporter`/`Endpoint` (`config.go:126`)
— there is **no per-signal logs protocol field**, and the real fallback chain runs
through `GetOTLPConfig()`, which folds the deprecated `Exporter.*` fields into the shared
shape (`config.go:335`). The design therefore:

**Adds `LogsConfig.Protocol`** (mirrors the per-signal `Endpoint` override pattern):

```go
// Protocol overrides OTLP.Protocol for logs.
Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" env:"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL" validate:"omitempty,oneof=grpc http/protobuf http"`
```

`OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` is the OTel-spec env name. The field is also wired
into `resolveLogExporterParams` so the SDK path (`NewLoggerProvider`) honors it —
keeping the two pipelines config-compatible. This is deliberately **logs-only**:
`TracesConfig`/`MetricsConfig` keep no per-signal protocol field — the asymmetry exists
because zaplog's HTTP-only transport needs a way to diverge from a grpc-default shared
protocol without affecting traces/metrics; it is not a precedent for adding per-signal
protocol elsewhere.

**Mapping rules for `zaplog.NewCore`** (all "effective" values resolved through the same
chain `resolveLogExporterParams` uses). The base is `GetOTLPConfig()`, whose semantics
are **wholesale, not field-merge** (`config.go:337-353`): it returns `cfg.OTLP` as-is
when non-nil, and converts the deprecated `Exporter` block only when `cfg.OTLP == nil`
— a config setting `OTLP` at all never falls back to `Exporter` for individual fields.
`Logs.*` fields then overlay that base per-field:

1. Effective endpoint: `Logs.Endpoint` if set, else `GetOTLPConfig().Endpoint`
   (= `OTLP.Endpoint`, or deprecated `Exporter.Endpoint` only when no `OTLP` block —
   backward compatible with existing otx configs).
2. Effective protocol: `Logs.Protocol` if set, else `GetOTLPConfig().Protocol` (same
   wholesale rule). Must be `http/protobuf` (or the `http` alias); if it resolves to
   `grpc`, return a **clear error** ("zaplog requires logs protocol http/protobuf; set
   telemetry.logs.protocol, or use otx.NewLoggerProvider for gRPC") — no silent
   4317→4318 port rewriting.
3. Scheme: bare `host:port` + effective `Insecure: true` → `http://`; otherwise
   `https://`. An endpoint already carrying a scheme passes through (zapwire validates
   and appends `/v1/logs` only to empty or `/` paths).
4. **Config-derived transport options are part of `NewCore`, not caller homework**: the
   effective `Headers` → `otlp.WithHeaders`, `Timeout` → `otlp.WithTimeout`,
   `Compression: gzip` → `otlp.WithCompression(otlp.Gzip)`. Without this, services with
   OTLP auth headers or gzip would silently change behavior when switching pipelines.
5. **Option precedence:** config-derived options are applied FIRST, then the caller's
   `opts ...otlp.Option` — zapwire options apply in slice order, so explicit caller
   options win over config (the same prepend-defaults discipline zapwire itself uses).

### 3.3 Resource identity — attribute parity, not byte parity (pass-1 P1)

Translate `otx.BuildResource(ctx, cfg)` output into `otlp.WithServiceName(...)` +
`otlp.WithResource(zapFields...)` (attribute.KeyValue → zap.Field: STRING/BOOL/INT64/
FLOAT64 + slice forms). One source of truth; logs and traces carry the same identity.

**De-duplication rule (pass-2 P1):** zapwire's envelope ALWAYS emits `service.name` from
`WithServiceName`, and otx's resource also contains a `service.name` attribute — mapping
all attributes into `WithResource` would emit it twice. `zaplog` extracts `service.name`
from the resource for `WithServiceName` and EXCLUDES it from the `WithResource` fields;
a test asserts exactly one `service.name` in the encoded request.

The parity claim is **resource attribute parity**: `service.name`, `service.version`,
`deployment.environment`, and configured `ResourceAttributes` (`provider.go:174-184`)
match across signals — which is what backends join on. It is NOT byte parity:
`buildResource` attaches `resource.WithSchemaURL(semconv.SchemaURL)`, and zapwire's
envelope does not encode `ResourceLogs.schema_url` (`zapwire/otlp/envelope.go`); the
schema URL is omitted from the log resource. This is documented, harmless to current
backends, and a candidate `otlp.WithSchemaURL` zapwire enhancement later if ever needed.
otx also synthesizes no `service.instance.id`/host attributes — none appear on either
signal unless configured.

### 3.4 Lifecycle (sharpened per pass-1 P2)

`NewCore` returns the `*otlp.Writer`; the caller owns it. **Recommended shutdown:
`w.Sync()` then `w.Close()`.** `Sync` is the flush barrier with the full retry budget;
`Close` alone drains with single attempts (fast, but a transiently failing collector
loses the final batch). `logger.Sync()` reaches the writer through the tee and is
equivalent to `w.Sync()` for the OTLP side. `DroppedLogs()` remains readable after
`Close` for shutdown reporting. Independent of provider shutdowns (`tp.Shutdown` etc.);
no ordering constraint.

### 3.5 Config additions (minimal)

`LogsConfig` gains two optional fields (`Protocol` — §3.2 — and `MinLevel`):

```yaml
telemetry:
  logs:
    enabled: true
    protocol: http/protobuf   # per-signal override; OTEL_EXPORTER_OTLP_LOGS_PROTOCOL
    minLevel: warn            # zap level gating the OTLP core (default: info)
```

`minLevel` materializes as the `zapcore.LevelEnabler` default in `NewCore` when the
caller passes **nil**: `zaplog` resolves nil → `Logs.MinLevel` → `info` and never passes
nil through to `otlp.NewCore` (whose core stores the enabler directly — a nil would
panic in `Check`; pass-1 P2). Invalid `minLevel` strings fail config validation. A
`zap.AtomicLevel` escape hatch stays available by passing an explicit enabler — the
cost-control story (zapwire guide: "Cost control: send only warn+ to OTel") driven
from config.

### 3.5b `Logger` wrapper contract (pass-1 P2)

`Logger` embeds `*zap.Logger`, so every zap method remains reachable. The wrapper-aware
surface is exactly: `DebugCtx`/`InfoCtx`/`WarnCtx`/`ErrorCtx` plus wrapper-preserving
`With(fields ...zap.Field) Logger` and `Named(name string) Logger`. Everything else
(`Sugar`, `WithOptions`, `DPanic`/`Panic`/`Fatal`, `Sync`, …) intentionally falls
through to the embedded `*zap.Logger` — callers needing ctx-aware panic/fatal use
`l.Logger.Fatal(msg, otlp.InjectTraceFields(ctx, fields...)...)` directly. Sugared ctx
variants are deferred (YAGNI) — `otlp.InjectTraceKVs` already covers sugared call sites.

### 3.6 What NOT to do

- No zap→sdk/log bridge alongside zaplog (double pipeline, double cost).
- No otx import inside zapwire — ever (zapwire's dependency policy is load-bearing).
- No re-wrapping of zapwire options: `NewCore` passes `opts ...otlp.Option` through, so
  zapwire features (gzip, headers, retry tuning, `WithTraceCorrelationAttributes` for
  OTLP→non-OTLP conversion pipelines) stay reachable without otx churn.

### 3.7 Version note

otx pins otel v1.39.x; zapwire/otlp requires `otel/trace` v1.44.0 → MVS lifts otx's
`otel/trace` (stable v1.x API, no breakage expected; verify with the full otx test suite
when implementing).

## 4. Implementation sketch

1. Package `otx`: export `BuildResource` (thin wrapper over `buildResource`);
   `LogsConfig.Protocol` + `LogsConfig.MinLevel` fields with env tags + validation;
   wire `Logs.Protocol` into `resolveLogExporterParams` (SDK path honors it too).
2. `zaplog/core.go`: effective-config resolution (endpoint/protocol/insecure/headers/
   timeout/compression via the `GetOTLPConfig()` + `Logs.*` overlay chain), scheme
   mapping, resource translation, `NewCore` (config-derived options first, caller opts
   after).
3. `zaplog/logger.go`: `Attach`, `Logger` ctx methods + wrapper-preserving `With`/`Named`.
4. Tests (from the pass-1 review):
   - endpoint precedence table reflecting GetOTLPConfig's WHOLESALE semantics:
     `Logs.Endpoint` overlay; `OTLP` block present (deprecated `Exporter` ignored
     entirely); `OTLP` nil + deprecated `Exporter.Endpoint`; empty default; full-URL
     pass-through vs `/v1/logs` append.
   - protocol/security/options table: per-signal override, grpc rejection error, `http`
     alias, `Insecure` scheme selection, headers, timeout, gzip.
   - resource parity: concrete attribute keys/values from `BuildResource`
     (`service.name`, `service.version`, `deployment.environment`, `ResourceAttributes`)
     present in the encoded request; **exactly one `service.name`** (extracted for
     `WithServiceName`, excluded from `WithResource` — pass-2 P1); schema URL
     documented-absent; no synthesized `service.instance.id`/host attrs.
   - level defaults: nil → `Logs.MinLevel` → `info`; explicit enabler wins; invalid
     `minLevel` fails validation; nil never reaches `otlp.NewCore`.
   - Attach/injection: tee preserves original output; OTLP core consumes
     `InjectTraceFields`; sticky `zap.Any("context", ctx)` works through the zapwire
     custom core; non-OTLP tee'd cores render `span_context` legibly.
   - shutdown: `w.Sync()`+`w.Close()`, `Close` without `Sync`, `DroppedLogs` after close.
   - httptest end-to-end: otx-created span ctx (`otx.Start`) → `InfoCtx` → captured OTLP
     body carries the trace_id.
5. Docs: routing rule in `docs/getting-started.md` + a `docs/zap-logging.md` guide;
   cross-link from zapwire's guide.

## 5. Decisions (resolved 2026-06-12)

1. **OTLP-only.** `zaplog` fronts only `zapwire/otlp`; the other zapwire processors
   (fluent/syslog/ndjson) don't need otx's config surface and stay direct.
2. **Home: `otx/zaplog`.** Approved for implementation here; if `shared/logging` later
   becomes the blessed app-layer logger it can consume or absorb this package — the
   design transfers unchanged.
3. **`minLevel` default: `info`** — least surprise; the cost-safe `warn` pattern is
   documented, not imposed.

## 6. Dependency bootstrap note

`github.com/arloliu/zapwire/otlp` has no release tag yet (zapwire PR #4 open). Until it
merges and gains an `otlp/vX.Y.Z` tag, otx pins a pseudo-version against the pushed
branch commit (`go get github.com/arloliu/zapwire/otlp@<commit>`); bump to the tag after
merge. No `replace` directive is committed.

## 7. Review pass-1 resolutions

| Finding | Resolution |
|---|---|
| **P0** `zaplog` cannot call unexported `buildResource` (provider.go:168) | Export `otx.BuildResource` thin wrapper (§3.1); providers keep the unexported form internally. |
| **P1** Design named non-existent `logs.protocol`; precedence omitted the deprecated `Exporter.*` fallback chain | `LogsConfig.Protocol` added for real (OTel env `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL`), wired into `resolveLogExporterParams` too; effective endpoint/protocol resolved via `GetOTLPConfig()` + `Logs.*` overlays incl. deprecated fields (§3.2). |
| **P1** Config-driven `Headers`/`Timeout`/`Compression` would be silently dropped | Config-derived `WithHeaders`/`WithTimeout`/`WithCompression` are part of `NewCore`; applied before caller opts so explicit options win (§3.2 rules 4-5). |
| **P1** "Exact resource parity" unachievable — zapwire omits `ResourceLogs.schema_url` | Claim narrowed to **resource attribute parity** (what backends join on); schema-URL absence documented; `otlp.WithSchemaURL` noted as a possible future zapwire enhancement (§3.3). |
| **P2** Shutdown contract, nil-level obligation, `Logger` method-set ambiguity | `w.Sync()` then `w.Close()` recommended (retry budget vs single-attempt drain); nil level resolved by zaplog before reaching `otlp.NewCore`; `Logger` wrapper contract pinned (§3.4, §3.5, §3.5b). |

## 8. Review pass-2 resolutions

| Finding | Resolution |
|---|---|
| **P1** Precedence table misrepresented `GetOTLPConfig()` as field-merge | §3.2 now states the wholesale rule (`cfg.OTLP` returned as-is when non-nil; deprecated `Exporter` converted only when `OTLP == nil`) with `Logs.*` per-field overlays on top; test matrix rows updated to encode it. |
| **P1** `service.name` would be emitted twice (resource attrs + WithServiceName) | §3.3 de-duplication rule: extract `service.name` for `WithServiceName`, exclude from `WithResource`; exactly-one-`service.name` test added. |
| **P2** `Logs.Protocol` per-signal asymmetry undocumented | §3.2 notes the field is deliberately logs-only (HTTP-only transport needs divergence from a grpc-default shared protocol) and not a precedent for traces/metrics. |
