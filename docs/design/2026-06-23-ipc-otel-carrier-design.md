# IPC OTel carrier — design (otx)

**Status:** ✅ implemented in subpackage `otx/carrier` — codex design-review
consensus (round 5) + whole-branch code review clean · **Date:** 2026-06-23 ·
**Branch:** `feat/ipc-otel-carrier`

**Relates to:** otx's existing W3C text propagation (`propagation.go` —
`InjectGRPC`/`ExtractGRPC`, `BuildPropagator`) and baggage helpers
(`baggage.go`). This design adds a *binary*, marshalable representation of the
same OTel propagation state for in-process plugin/IPC boundaries that the text
propagators do not serve well.

---

## 1. Motivation

OTel propagation in otx today is **text + context-bound**: `InjectGRPC`/
`ExtractGRPC` (propagation.go:112-121) move trace context and baggage through
gRPC *metadata* as W3C strings, and every baggage helper (baggage.go) operates
on `context.Context`. That is the right model for network RPC between services.

A new scenario does not fit it: an **in-process plugin architecture** where a
host process talks to plugin processes over a **UDS-based gRPC** transport. The
host must hand the plugin its OTel propagation state (span context + baggage) so
the plugin can continue the trace and read baggage. Requirements the text path
does not meet cleanly:

- The caller wants to carry the OTel state **as an opaque blob inside their own
  message** (a `bytes` field, a pipe frame, shared memory) — not only as gRPC
  metadata headers. That demands a self-contained `[]byte`, i.e. the standard
  `encoding.BinaryMarshaler` / `BinaryUnmarshaler` pair.
- It is a **hot path**: the host may invoke plugins frequently, so propagation
  must be allocation-light, and a context carrying *no* OTel info must cost
  almost nothing to detect and skip.
- Host and plugins may be **built against different otx versions**, so the wire
  format needs explicit versioning and forward-compatible framing.

## 2. Goal

A single value type — `Carrier` (§11) — that is:

1. **Fast in-process / in-memory.** Holds OTel's own already-fast types so
   in-process reads of span context and baggage cost nothing to decode.
2. **Marshalable for IPC / remote.** Implements the `BinaryCodec` contract:

   ```go
   type BinaryCodec interface {
       encoding.BinaryMarshaler
       encoding.BinaryUnmarshaler
   }
   ```

3. **Cheap to skip when empty.** A context with no OTel info is detected without
   allocation or serialization, on both the producing and consuming sides.
4. **Interoperable on demand.** Public methods convert to/from the W3C text forms
   (`traceparent` / `tracestate` / `baggage`) for debugging and for bridging to
   the existing text propagators or a future polyglot peer — within the W3C
   model's own fidelity limits (§5.6).

Non-goal: replacing the text propagators. `Carrier` is additive; the W3C text
path (`InjectGRPC`/`ExtractGRPC`) is unchanged.

## 3. Scope

**In scope (decided):** SpanContext (TraceID, SpanID, TraceFlags, TraceState,
remote flag) + Baggage (members incl. W3C properties). The struct, the binary
codec, context bridges, fast-empty checks, W3C interop methods.

**Out of scope (declined this round, see §10):** gRPC interceptors and binary
metadata helpers (caller wires transport itself); additional OTel signals
(sampling hints, metric exemplars); compression; a non-Go reference codec.

## 4. Invariants

The design is judged against these. Each is exercised by a test in §9.

- **I1 — binary round-trip fidelity.** For any `Carrier` `c` **whose baggage is
  W3C-conformant** (acceptable to `baggage.Parse`: ≤ 64 members and ≤ 8192
  serialized bytes), `UnmarshalBinary(MarshalBinary(c))` yields a `Carrier`
  semantically equal to `c`'s **canonical form** (§5.2): identical
  TraceID/SpanID/full TraceFlags byte/remote bit, identical TraceState string,
  and identical baggage members including properties and values. This invariant
  is about the **binary** codec only; the W3C text methods (§5.6) are
  intentionally lossy per the W3C model and are **not** covered by I1. The empty
  carrier is included: `MarshalBinary` yields `nil`, and `UnmarshalBinary(nil)`
  reproduces the empty carrier (§5.3). **Excluded by design:** baggage that
  exceeds the W3C limits — constructible only by bypassing `baggage.Parse` via
  repeated `baggage.SetMember`, which does not enforce them (§6 note) — decodes
  to OTel's own partial-parse result, *identical to what the stock W3C `Baggage`
  extractor installs* (empty → dropped for > 8192 bytes; the truncated
  first-64-member subset for > 64 members), rather than round-tripping in full.
  This is outside I1 by design — the binary path is never weaker than the text
  path — and §9 tests it.
- **I2 — cheap negative.** When a context carries neither a valid span context
  nor any baggage, the producer performs **zero serialization and zero
  allocation** beyond the two `context.Value` lookups, and the consumer performs
  **zero parsing** to reach a `false` result.
- **I3 — untrusted-input safety.** `UnmarshalBinary` and `HasOTel` never panic
  for *any* input bytes. Total input is bounded by `MaxBytes` (§5.3),
  checked before any allocation; every field is bounded by the remaining buffer,
  so allocation is `O(min(len(input), MaxBytes))`. `MaxBytes` is
  set above the largest valid carrier, so the bound never rejects a conformant
  carrier — I3 does not fight I1.
- **I4 — forward compatibility.** A decoder silently skips TLV tags it does not
  recognize. A newer producer that adds a new *tag* never breaks an older
  consumer's decode of the fields it does understand, and vice versa. A higher
  *version byte* is a breaking change and is rejected (§6), not skipped.

## 5. Design

### 5.1 Two-layer model (the core idea)

`Carrier` separates the **in-memory representation** from the **wire
representation**:

- **In-memory:** the struct stores OTel's own value types —
  `trace.SpanContext` (`TraceID [16]byte`, `SpanID [8]byte`, plus flags and an
  immutable `TraceState`) and `baggage.Baggage` (already parsed, O(1)
  `Member(key)` lookup). In-process callers read these directly; there is
  nothing to decode.
- **Wire:** `MarshalBinary` / `UnmarshalBinary` translate that to/from compact
  bytes, and only at the IPC boundary.

The two layers are independent: in-process use never pays serialization cost;
IPC use crosses the boundary exactly once per direction.

### 5.2 The `Carrier` struct and API surface

All symbols below live in package `carrier` (import `github.com/arloliu/otx/carrier`),
so callers write `carrier.New`, `carrier.FromContext`, `carrier.HasOTel`, etc. (§8).

```go
// Carrier is a portable, marshalable snapshot of OTel propagation state
// (span context + baggage). It is an immutable value; the zero value is the
// empty carrier. Copying a Carrier is cheap and safe for concurrent reads.
type Carrier struct {
    sc  trace.SpanContext // unexported; immutable OTel value
    bag baggage.Baggage   // unexported; immutable OTel value
}

// --- construction / context bridges (the only seams to context.Context) ---

// FromContext extracts the OTel propagation state from ctx. ok is false when
// ctx carries neither a valid span context nor any baggage — the hot-path
// guard (invariant I2). When ok is false the returned Carrier is the empty
// carrier. A context with baggage but an invalid/absent span returns ok=true.
func FromContext(ctx context.Context) (c Carrier, ok bool)

// New builds a Carrier from explicit OTel values. It CANONICALIZES: if
// sc.IsValid() is false (either ID zero) the span context is treated as absent
// for all subsequent operations (marshal, W3C, Context). This makes the API
// round-trip — New(invalid, bag) marshals and decodes identically.
func New(sc trace.SpanContext, bag baggage.Baggage) Carrier

// Context returns a child of ctx carrying this Carrier's span context (as a
// REMOTE span context, matching OTel extract semantics) and baggage. If the
// Carrier IsEmpty(), Context returns ctx unchanged (installs no context values).
func (c Carrier) Context(ctx context.Context) context.Context

// --- fast checks ---

// IsEmpty reports whether the Carrier holds no valid span context and no
// baggage. A few array compares; no allocation.
func (c Carrier) IsEmpty() bool

// HasOTel is a cheap, conservative pre-filter over a marshaled Carrier: it
// reports whether b *could* contain OTel info. It returns false ONLY for nil,
// empty, or a buffer whose first byte is not the current version, or a
// version-only buffer (len <= 1). It NEVER returns false for a well-formed
// non-empty Carrier (no false negatives), but MAY return true for malformed
// bytes (false positives are acceptable — UnmarshalBinary then rejects them).
// It does NOT validate; it is a hot-path skip filter, not a decode.
func HasOTel(b []byte) bool

// --- in-memory accessors (zero-cost) ---

func (c Carrier) SpanContext() trace.SpanContext
func (c Carrier) Baggage() baggage.Baggage

// --- BinaryCodec ---

func (c Carrier) MarshalBinary() ([]byte, error)          // = AppendBinary(nil)
func (c *Carrier) UnmarshalBinary(data []byte) error      // accepts nil/empty (→ empty carrier)
func (c Carrier) AppendBinary(b []byte) ([]byte, error)   // Go 1.24+; zero-alloc into caller buffer

// --- W3C text interop (lossy per the W3C model — see §5.6) ---

func (c Carrier) W3C() (traceparent, tracestate, baggage string)
func ParseW3C(traceparent, tracestate, baggage string) (Carrier, error)
```

Compile-time interface assertions (rule 200):

```go
var (
    _ encoding.BinaryMarshaler   = Carrier{}
    _ encoding.BinaryUnmarshaler = (*Carrier)(nil)
    _ encoding.BinaryAppender    = Carrier{}
)
```

**Canonical form.** A `Carrier`'s canonical form is: span context present iff
`sc.IsValid()`; tracestate present iff a valid span context is present and the
tracestate string is non-empty (an orphan tracestate without a span is dropped);
baggage present iff `bag.Len() > 0`. `MarshalBinary`, `W3C`, `Context`, and
`IsEmpty` all operate on the canonical form, so the API round-trips even for
inputs built via `New` from non-canonical OTel values.

`AppendBinary` is included because otx targets Go 1.25 and the hot path benefits
from encoding into a reused buffer with no per-call allocation; `MarshalBinary`
is the thin `AppendBinary(nil)` wrapper the interface requires.

### 5.3 Wire format (compact, versioned, forward-compatible)

Constants:

```go
const formatVersion = 1 // unexported — internal format detail, not public API

// MaxBytes bounds UnmarshalBinary input. Exported so callers can reject
// an oversized blob before decoding. It is set well above the largest carrier a
// conformant producer can emit — 24,675 bytes: version 1 + core TLV (1+1+26) +
// tracestate TLV (1+3 + 32*(256 key + 1 "=" + 256 value) + 31 commas = 16,447) +
// baggage TLV (1+2+8192) (tracestate.go:13,50; baggage.go:22) — so it NEVER
// rejects a valid carrier (preserving I1); it only stops oversized/hostile input.
const MaxBytes = 65536
```

OTel already enforces the tracestate and baggage limits *semantically*:
`ParseTraceState` caps at 32 members (tracestate.go:212) and `baggage.Parse` at
8192 bytes / 64 members (baggage.go:517,456). The decoder does **not** add
tighter per-field byte caps — an earlier draft's 512-byte tracestate cap was
below OTel's real maximum and would have rejected valid carriers, breaking I1.
Instead it bounds total input by `MaxBytes` and lets the OTel parsers
enforce the semantic limits: a tracestate that fails `ParseTraceState` is dropped,
and baggage uses the `baggage.Parse` result iff non-empty (§6). Every field
allocation therefore stays bounded by `MaxBytes`.

A 1-byte version followed by tag-length-value (TLV) entries. Tag and length are
unsigned varints encoded with `encoding/binary.PutUvarint` and decoded with
`binary.Uvarint`:

```
byte 0        : format version (currently formatVersion = 1)
then, repeated: tag (uvarint) · len (uvarint) · value (len bytes)

  tag 1  SpanContext core  (fixed 26-byte value; emitted iff sc.IsValid()):
           [0:16]  TraceID
           [16:24] SpanID
           [24]    TraceFlags (full W3C byte; bit0 = sampled)
           [25]    otx flags  (bit0 = remote)
  tag 2  TraceState  : the W3C tracestate string (emitted iff valid core + non-empty)
  tag 3  Baggage     : the W3C baggage string    (emitted iff bag.Len() > 0)
```

- **Compact.** A typical sampled span with no baggage is `1 (version) + 1 (tag)
  + 1 (len=26) + 26` = **29 bytes**, with trace/span IDs as raw bytes (no hex).
- **TraceState / Baggage carried as their W3C canonical strings** (the hybrid
  decision): they are inherently key-value strings, and OTel already produces
  and validates them via `TraceState.String()` / `baggage.Baggage.String()` and
  `trace.ParseTraceState` / `baggage.Parse`. We reuse that rather than maintain
  a bespoke codec, and it makes the W3C methods (§5.6) nearly free.
- **TraceState is a separate TLV, not folded into the SpanContext entry**, so the
  core stays fixed-size and memcpy-able and a no-tracestate carrier omits it.
- **The full TraceFlags byte is preserved** (lossless), unlike the W3C text path
  which masks it (§5.6).
- **Field evolution policy.** A new *optional* field is a new *tag* (skippable by
  old decoders — I4). Changing an existing field's layout (e.g. widening the
  core) is a *breaking* change and bumps the *version byte*. Within version 1 the
  core value length is exactly 26.
- **Empty carrier — canonical encoding (closes P0).** `MarshalBinary` /
  `AppendBinary` on the canonical-empty carrier return `nil` (append nothing).
  `UnmarshalBinary` accepts `nil`, an empty slice, and a version-only buffer
  (`[]byte{1}`) and decodes each to the empty carrier with **no error** — so
  `UnmarshalBinary(MarshalBinary(empty))` round-trips (I1), satisfying the Go
  `BinaryUnmarshaler` contract. `HasOTel` returns false for all three.

Only canonical-present fields are emitted; tags are written in ascending order.

### 5.4 Fast-path emptiness checks (invariant I2)

- **Producer.** `FromContext` reads `trace.SpanContextFromContext(ctx)` and
  `baggage.FromContext(ctx)` and returns `ok = sc.IsValid() || bag.Len() > 0`.
  Both are `context.Value` lookups; `bag.Len()` is a slice-length read. The hot
  path:

  ```go
  if c, ok := carrier.FromContext(ctx); ok {
      blob, _ := c.MarshalBinary()
      // attach blob to the outgoing message
  } // else: attach nothing — no marshal, no allocation
  ```

- **Consumer.** Primary mechanism: no OTel info ⇒ producer attaches no blob ⇒
  consumer sees a nil/zero-length field and skips with `len(b) == 0` — zero
  parsing. When a blob is always present, `HasOTel(b)` peeks the header only.

### 5.5 Context bridges and remote semantics

`FromContext` and `Context` are the **only two seams** between `Carrier` and
`context.Context`; the type is otherwise self-contained.

- `FromContext` observes context state via `trace.SpanContextFromContext` +
  `baggage.FromContext`.
- `Context` mutates a derived context via `trace.ContextWithRemoteSpanContext`
  (so the injected span context is marked **remote**, matching how OTel's own
  `propagation.TraceContext.Extract` treats an extracted context) +
  `baggage.ContextWithBaggage`. On an **empty** carrier it returns `ctx`
  unchanged — it installs neither a span-context nor a baggage value.

**Deliberate asymmetry, documented to preempt confusion:** `UnmarshalBinary`
preserves the remote bit verbatim, so the **binary** round-trip is identity (I1).
`Context` always injects as remote, so `Carrier`→`Context`→`FromContext` may
report `remote = true` on a context that was local at the producer. I1 is scoped
to the binary codec precisely so this projection does not contradict it. This
mirrors OTel propagator inject/extract behavior and is correct for a cross-process
boundary.

### 5.6 W3C text interop (lossy by the W3C model — closes the W3C P1)

`W3C()` follows OTel's stock `propagation.TraceContext` / `propagation.Baggage`
semantics exactly, which means it is **intentionally lossy** relative to the
binary codec:

- `traceparent` is built only when the canonical span context is valid; an
  invalid/absent span yields an empty `traceparent`.
- The `traceparent` flags byte is **masked to the W3C-defined bits** (sampled,
  and random per OTel) — exactly what `TraceContext.Inject` emits
  (trace_context.go:49-64). Private/high flag bits the binary format preserves
  are **not** representable in W3C text and are dropped here. `traceparent` is
  `00-<32 lowercase hex traceID>-<16 lowercase hex spanID>-<2 hex masked flags>`.
- `tracestate` is `TraceState.String()`; `baggage` is `baggage.Baggage.String()`.

`ParseW3C` inverts via `trace.ParseTraceState` and `baggage.Parse`, and parses
`traceparent` into a `SpanContextConfig` (marking the result remote). `ParseW3C`
∘ `W3C` round-trips for any context expressible in W3C text, but **not** for
private flag bits — that is a property of the W3C model, not a defect, and is
called out so no caller expects binary-grade fidelity from the text path. Empty
input strings yield absent components.

These methods exist for debugging, for bridging to the existing text
propagators, and as the documented interop path if a non-otx peer appears.

## 6. Error handling and security (untrusted cross-process input)

`UnmarshalBinary` decodes bytes produced by **another process** and is treated as
hostile (rule 700). Invariant I3 governs it. The decoder applies these rules in
order; "drop" means the component is treated as absent with no error, "fail"
means return the named sentinel and leave the receiver unchanged.

| Condition | Result |
|---|---|
| `nil` / empty / version-only (`len(data) <= 1` with `data[0]==1`) | empty carrier, **no error** |
| `len(data) > MaxBytes` | **fail** `ErrMalformed` (checked first, O(1)) |
| `data[0] != formatVersion` | **fail** `ErrUnsupportedVersion` |
| tag or len `binary.Uvarint` returns `n <= 0` (incomplete / overflow) | **fail** `ErrMalformed` |
| declared `len` > remaining bytes | **fail** `ErrMalformed` |
| duplicate known tag (1, 2, or 3) | **fail** `ErrMalformed` |
| trailing partial TLV (cannot read a full tag+len+value) | **fail** `ErrMalformed` |
| tag 1 declared len ≠ 26 | **fail** `ErrMalformed` |
| tag 1 decodes to `!sc.IsValid()` (zero/partial IDs) | **drop** span context |
| tag 2 present without a valid tag 1 core | **drop** tracestate |
| tag 2 fails `trace.ParseTraceState` (incl. > 32 members) | **drop** tracestate |
| tag 3 — use `baggage.Parse` result **iff non-empty** (mirrors stock W3C extract) | empty (e.g. > 8192 bytes) → baggage absent; partial (> 64 members) → kept |
| unknown tag | **skip** via its length prefix (I4) |

Rationale notes:

- **Total input is bounded by `MaxBytes` (checked first, O(1))**, so every
  field is ≤ `MaxBytes` and no decode can be forced into an unbounded
  allocation. Tighter per-field byte caps are deliberately NOT added: OTel's real
  tracestate maximum — 32 members × (256-byte key + "=" + 256-byte value) + 31
  commas ≈ 16,447 bytes — exceeds any small byte cap, so such a cap would reject
  valid carriers and break I1. Semantic limits are left to `ParseTraceState` /
  `baggage.Parse`: a tracestate that fails to parse is dropped, and baggage uses
  the parse result iff non-empty (next bullet) — the span context and the other
  field always survive.
- **`binary.Uvarint`'s `n == 0` (buffer too small) and `n < 0` (64-bit overflow)
  both map to `ErrMalformed`.** Non-minimal (non-canonical) varints are tolerated
  — they are harmless once the value is bounded — and this is stated explicitly.
- **Well-framed-but-semantically-invalid optional fields are dropped, not fatal**,
  mirroring how OTel's text propagators tolerate junk on extract. Structural
  corruption (framing, caps, duplicates, bad version) is fatal.
- **Over-limit baggage degrades exactly like stock W3C extract.** OTel's
  `baggage.SetMember` (used by otx's `SetBaggage`) does not enforce the 64-member
  / 8192-byte limits that `baggage.New` / `baggage.Parse` do (baggage.go:637-660
  vs :456,:517), so an application can accumulate a `Baggage` exceeding them. The
  decoder runs `baggage.Parse` on tag 3 and uses the result **iff non-empty** —
  the same rule as the stock `propagation.Baggage` extractor
  (propagation/baggage.go:68-74): a > 8192-byte baggage parses to empty and is
  **dropped**; a > 64-member baggage parses to its truncated first-64-member
  subset and is **kept** (`baggage.Parse` sets a truncation error but still
  returns the partial — baggage.go:80-82). The span context and tracestate always
  survive. This is the single case I1 excludes (§4); it reproduces
  otx-`SetBaggage`'s pre-existing limit behavior faithfully rather than
  introducing new semantics. (The > 64 subset's member order is non-deterministic,
  inherited from `Baggage.String()` — OTel's own text propagators share it.)
- **No panics** for any input — guaranteed by `FuzzUnmarshalBinary` and
  `FuzzHasOTel` (§9).

Sentinel errors (rule 200): `ErrMalformed`, `ErrUnsupportedVersion`.

`MarshalBinary` / `AppendBinary` cannot fail in practice (inputs are valid OTel
values whose `String()` always succeeds); they return the interface-required
`error` as always-`nil`.

## 7. Concurrency

`Carrier` is an immutable value with unexported `trace.SpanContext` /
`baggage.Baggage` fields (both themselves immutable). A `Carrier` value is safe
for concurrent reads and for `MarshalBinary`/`AppendBinary` from multiple
goroutines. `UnmarshalBinary` mutates its pointer receiver and must not race with
reads of the same value — the standard `BinaryUnmarshaler` contract; no internal
locking is needed or added.

## 8. Placement, naming, file layout

The type lives in its **own focused subpackage `otx/carrier`**, not the root
`otx` package. `otx` is a large multi-purpose package (config, providers,
exporters, span helpers, `propagation.go`, `baggage.go`), so a bare
`otx.FromContext` / `otx.HasOTel` / `otx.ParseW3C` would not signal "Carrier" at
the call site. A dedicated subpackage makes every free function self-describing —
`carrier.FromContext`, `carrier.New`, `carrier.HasOTel`, `carrier.ParseW3C`,
`carrier.ErrMalformed` — matching OTel's own idiom (`baggage.FromContext`) and
otx's existing subpackage structure (`grpc/`, `http/`, `nats/`, `zaplog/`).

- New files in package `carrier`:
  - `carrier/carrier.go` — the type, constants, codec, bridges, W3C methods.
  - `carrier/carrier_test.go` — round-trip, fast-check, W3C-equivalence,
    forward-compat, decoder-semantics, hostile-input tables.
  - `carrier/carrier_fuzz_test.go` — `FuzzUnmarshalBinary`, `FuzzHasOTel`.
- The constructor is `carrier.New` (not `NewCarrier` — that would stutter); the
  size bound is `carrier.MaxBytes`. The central type is `carrier.Carrier`; the
  stutter is the accepted cost of clean function names (cf. stdlib `list.List`,
  `ring.Ring`) and does not trip the linter.
- No new dependency: `trace`, `baggage`, `propagation`, `encoding`,
  `encoding/binary` are all already in the module (OTel v1.44.0).

## 9. Testing plan (compiles against current source — rule 800 §6)

All tests use only today's exported otx API plus the OTel API verified present in
v1.44.0; no new seam or mock is required.

- **I1 binary round-trip identity** across the matrix: empty / span-only /
  baggage-only / both / tracestate present / **baggage with properties**.
  Baggage comparison is **property-aware and order-insensitive over members**:
  compare the member *set* (since `Baggage.Members()` returns map-insignificant
  order) and, per member, the **ordered property slice** from `Member.Properties()`
  (a copy) — do **not** use `AllBaggage`, which drops properties (baggage.go:82-91).
  Also: `New(invalid-span, bag)` marshals/decodes identically to its
  canonical form (API round-trip, not just `FromContext`). **Regression guard for
  the removed byte cap:** a `TraceState` with 32 members serializing to > 512
  bytes (and a baggage string near the 8192-byte limit) round-trips losslessly.
- **I1 exclusion (degradation matches stock W3C extract, not full round-trip):** a
  `Baggage` built via repeated `baggage.SetMember` past the limits marshals
  successfully; on decode, a > 8192-byte baggage yields **empty** baggage
  (dropped) while a > 64-member baggage yields a **non-empty truncated** baggage
  (OTel's first-64 subset) — span context and tracestate retained in both,
  matching `propagation.Baggage` extract (§4/§6 contract).
- **Empty-encoding contract (P0):** `MarshalBinary` of the zero carrier returns
  `nil`; `UnmarshalBinary(nil)`, `UnmarshalBinary([]byte{})`, and
  `UnmarshalBinary([]byte{1})` each yield the empty carrier with no error.
- **I4 forward-compat:** decode bytes with (a) an unknown **trailing** tag, (b)
  an unknown tag **interleaved** before a known tag, (c) an **unknown-only**
  frame (→ empty carrier, no error), (d) a **missing optional** tag; assert known
  fields decode intact. A **higher version byte** returns `ErrUnsupportedVersion`.
- **Decoder semantics (P1):** partial IDs (valid TraceID, zero SpanID → span
  dropped); tracestate-only frame (orphan → dropped); baggage-only with invalid
  span (→ `ok`/baggage retained, span absent); duplicate known tag →
  `ErrMalformed`; trailing partial TLV → `ErrMalformed`; tag-1 len ≠ 26 →
  `ErrMalformed`.
- **I3 hostile input:** table — truncated header, length overrun, incomplete
  uvarint (`n==0`), overflow uvarint (`n<0`), non-canonical uvarint (tolerated),
  total length > `MaxBytes` (→ `ErrMalformed`), over-limit tracestate
  (> 32 members → dropped) and baggage (per §6: > 8192 bytes → dropped, > 64
  members → truncated non-empty kept), span retained — each returns the right
  result and never panics. `FuzzUnmarshalBinary` and `FuzzHasOTel` for panic
  coverage and the bounded-allocation guarantee.
- **W3C interop:** `Carrier.W3C()` matches OTel's stock `TraceContext` /
  `Baggage` output for the same context — comparing the `baggage` string
  **order-insensitively** (parse both sides, compare member sets, since
  `Baggage.String()` order is map-insignificant), not by raw equality;
  `ParseW3C ∘ W3C` round-trips for W3C-expressible contexts; `TraceFlags(0xff)`
  is masked in `W3C()` (lossy) while preserved in the binary round-trip;
  not-sampled flags and an invalid span (→ empty `traceparent`) behave per OTel.
- **I2 cheap negative:** `FromContext(context.Background())` returns `ok=false`;
  `FromContext` with baggage + invalid span returns `ok=true`; `Context()` on an
  empty carrier returns `ctx` unchanged. Benchmarks assert
  `testing.AllocsPerRun == 0` for `FromContext(Background)` and for
  `HasOTel(nil / empty / version-only / malformed)`.
- **Benchmarks:** `MarshalBinary`, `AppendBinary` (zero-alloc into a reused
  buffer), `UnmarshalBinary`, `FromContext` — to hold the hot-path budget.

Validation gate before commit (rule 500): `go fix ./...` on touched packages →
`make lint` → `make test` (race + nested integration module).

## 10. Out of scope / deferred

- **gRPC interceptors / binary metadata helper.** Transport wiring is the
  caller's; otx ships struct + codec + context glue only. Revisit if multiple
  callers reimplement the same plumbing.
- **Additional OTel signals** (explicit sampling decision, metric exemplars).
  Scope is SpanContext + Baggage, matching the W3C propagation surface.
- **Compression** of large baggage — premature; profile first (rule 700).
- **Non-Go reference codec** — the W3C methods are the interop path until a
  polyglot peer actually exists.

## 11. Resolved — naming & placement

The public type is **`carrier.Carrier`** in the `otx/carrier` subpackage (§8).
The mild overlap with OTel's `TextMapCarrier` concept is accepted: `Carrier` is
the marshalable propagation *payload*, not a transport medium. Free functions are
namespaced by the package (`carrier.New`, `carrier.FromContext`,
`carrier.HasOTel`, `carrier.ParseW3C`), so each reads clearly at the call site;
the `carrier.Carrier` type stutter is the accepted trade for that clarity.
