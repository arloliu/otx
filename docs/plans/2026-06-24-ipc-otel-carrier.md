# IPC OTel Carrier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Note (post-merge refactor):** the code ships in subpackage `otx/carrier` (files under `carrier/`); `New` replaces `NewCarrier` and `MaxBytes` replaces `MaxCarrierBytes`. Tests are `package carrier`.

**Goal:** Add `otx.Carrier`, a binary-marshalable snapshot of OTel propagation state (span context + baggage) for in-process host↔plugin IPC over UDS gRPC.

**Architecture:** A value type holding OTel's own `trace.SpanContext` and `baggage.Baggage` for zero-decode in-memory access, plus a versioned tag-length-value (TLV) binary codec (`encoding.BinaryMarshaler`/`BinaryUnmarshaler`/`BinaryAppender`), context bridges, fast emptiness checks, and W3C text interop that delegates to OTel's stock propagators.

**Tech Stack:** Go 1.25, `go.opentelemetry.io/otel/trace` + `/baggage` + `/propagation` v1.44.0, stdlib `encoding/binary`, testify.

**Spec:** `docs/design/2026-06-23-ipc-otel-carrier-design.md` (codex consensus, implementation-ready). Section refs below (§N) point at it.

## Global Constraints

- **Module:** single package `otx`, repo root. Go 1.25; OTel pinned at v1.44.0. No new dependencies (`trace`, `baggage`, `propagation`, `encoding/binary` already present).
- **Files:** all new code in `carrier.go`; tests in `carrier_test.go`; fuzz/bench in `carrier_fuzz_test.go`. Tests use `package carrier` (white-box) and testify `require`.
- **Validation gate before EVERY commit (rule 500):** `go fix ./...` on the root package → `make lint` (fix all issues; never edit `.golangci.yaml`) → `make test` (race + nested integration module). During TDD a step may run the faster `go test -race -run <Pattern> .`; the pre-commit gate still runs `make lint` + `make test`.
- **Commits:** Conventional Commits (`feat:`/`test:`/…), imperative, ≤ 50 chars subject. **Never** add `Co-Authored-By` or any attribution trailer.
- **Doc comments:** 80 soft / 120 hard line width.
- **Format version constant is `1`; `MaxBytes = 65536`; core TLV value is exactly 26 bytes (16 traceID + 8 spanID + 1 traceFlags + 1 otx-flags).**

## File Structure

- `carrier.go` — `Carrier` type, constants, sentinels, accessors, `IsEmpty`, `New`, `FromContext`, `Context`, `MarshalBinary`/`AppendBinary`/`UnmarshalBinary`, `HasOTel`, `W3C`/`ParseW3C`. One responsibility: the carrier value and its codecs.
- `carrier_test.go` — table tests for every invariant (I1–I4), decoder rules, W3C interop.
- `carrier_fuzz_test.go` — `FuzzUnmarshalBinary`, `FuzzHasOTel`, allocation benchmarks.

---

### Task 1: Carrier type, accessors, IsEmpty

**Files:**
- Create: `carrier.go`
- Test: `carrier_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `type Carrier struct{…}`; `func New(sc trace.SpanContext, bag baggage.Baggage) Carrier`; `func (c Carrier) SpanContext() trace.SpanContext`; `func (c Carrier) Baggage() baggage.Baggage`; `func (c Carrier) IsEmpty() bool`. A `Carrier` is canonical-by-observation: an invalid `sc` (either ID zero) is treated as absent by `IsEmpty` and every later operation.

- [ ] **Step 1: Write the failing test**

```go
package carrier

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// testSC returns a valid, recognizable span context (the W3C example IDs).
func testSC(t *testing.T, remote bool) trace.SpanContext {
	t.Helper()
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
		Remote:     remote,
	})
}

// testBaggage returns baggage with one member carrying a property.
func testBaggage(t *testing.T) baggage.Baggage {
	t.Helper()
	prop, err := baggage.NewKeyValueProperty("p", "1")
	require.NoError(t, err)
	m, err := baggage.NewMember("userId", "alice", prop)
	require.NoError(t, err)
	bag, err := baggage.New(m)
	require.NoError(t, err)
	return bag
}

func TestCarrierIsEmpty(t *testing.T) {
	require.True(t, Carrier{}.IsEmpty(), "zero carrier is empty")
	require.True(t, New(trace.SpanContext{}, baggage.Baggage{}).IsEmpty())
	require.False(t, New(testSC(t, false), baggage.Baggage{}).IsEmpty())
	require.False(t, New(trace.SpanContext{}, testBaggage(t)).IsEmpty())
}

func TestCarrierAccessors(t *testing.T) {
	sc := testSC(t, true)
	bag := testBaggage(t)
	c := New(sc, bag)
	require.True(t, c.SpanContext().Equal(sc))
	require.Equal(t, bag.Len(), c.Baggage().Len())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestCarrierIsEmpty|TestCarrierAccessors' .`
Expected: FAIL — `undefined: Carrier` / `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package otx — carrier.go
package carrier

import (
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// Carrier is a portable, marshalable snapshot of OTel propagation state (span
// context + baggage). It is an immutable value; the zero value is the empty
// carrier. Copying a Carrier is cheap and safe for concurrent reads. An invalid
// span context (either ID zero) is treated as absent by every operation.
type Carrier struct {
	sc  trace.SpanContext
	bag baggage.Baggage
}

// New builds a Carrier from explicit OTel values. If sc.IsValid() is
// false the span context is treated as absent for all later operations
// (marshal, W3C, Context, IsEmpty).
func New(sc trace.SpanContext, bag baggage.Baggage) Carrier {
	return Carrier{sc: sc, bag: bag}
}

// SpanContext returns the carrier's span context (which may be invalid).
func (c Carrier) SpanContext() trace.SpanContext { return c.sc }

// Baggage returns the carrier's baggage.
func (c Carrier) Baggage() baggage.Baggage { return c.bag }

// IsEmpty reports whether the carrier holds no valid span context and no
// baggage.
func (c Carrier) IsEmpty() bool {
	return !c.sc.IsValid() && c.bag.Len() == 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run 'TestCarrierIsEmpty|TestCarrierAccessors' .`
Expected: PASS.

- [ ] **Step 5: Commit**

Run the validation gate (Global Constraints), then:

```bash
git add carrier.go carrier_test.go
git commit -m "feat: add Carrier type, accessors, IsEmpty"
```

---

### Task 2: Context bridges (FromContext, Context)

**Files:**
- Modify: `carrier.go`
- Test: `carrier_test.go`

**Interfaces:**
- Consumes: `Carrier`, `New` (Task 1).
- Produces: `func FromContext(ctx context.Context) (Carrier, bool)` — `ok` is `false` only when ctx has neither a valid span context nor any baggage; `func (c Carrier) Context(ctx context.Context) context.Context` — injects the span context as **remote**, plus baggage; returns ctx unchanged when the carrier is empty.

- [ ] **Step 1: Write the failing test**

```go
func TestFromContext(t *testing.T) {
	// Empty context → ok=false.
	_, ok := FromContext(context.Background())
	require.False(t, ok)

	// Span only → ok=true.
	ctx := trace.ContextWithSpanContext(context.Background(), testSC(t, false))
	c, ok := FromContext(ctx)
	require.True(t, ok)
	require.True(t, c.SpanContext().Equal(testSC(t, false)))

	// Baggage only (no span) → ok=true.
	ctx = baggage.ContextWithBaggage(context.Background(), testBaggage(t))
	c, ok = FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 1, c.Baggage().Len())
}

func TestCarrierContext(t *testing.T) {
	// Empty carrier → ctx unchanged.
	base := context.Background()
	require.Equal(t, base, Carrier{}.Context(base))

	// Non-empty → span context installed as remote, baggage present.
	c := New(testSC(t, false), testBaggage(t))
	got := c.Context(base)
	sc := trace.SpanContextFromContext(got)
	require.True(t, sc.IsValid())
	require.True(t, sc.IsRemote(), "Context injects as remote")
	require.Equal(t, "alice", baggage.FromContext(got).Member("userId").Value())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestFromContext|TestCarrierContext' .`
Expected: FAIL — `undefined: FromContext`.

- [ ] **Step 3: Write minimal implementation**

Add `"context"` to the import block, then append:

```go
// FromContext extracts the OTel propagation state from ctx. ok is false when
// ctx carries neither a valid span context nor any baggage — the hot-path guard
// (no allocation, no marshaling on the empty path).
func FromContext(ctx context.Context) (Carrier, bool) {
	sc := trace.SpanContextFromContext(ctx)
	bag := baggage.FromContext(ctx)
	if !sc.IsValid() && bag.Len() == 0 {
		return Carrier{}, false
	}
	return Carrier{sc: sc, bag: bag}, true
}

// Context returns a child of ctx carrying this carrier's span context (as a
// REMOTE span context, matching OTel extract semantics) and baggage. An empty
// carrier returns ctx unchanged.
func (c Carrier) Context(ctx context.Context) context.Context {
	if c.sc.IsValid() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, c.sc)
	}
	if c.bag.Len() > 0 {
		ctx = baggage.ContextWithBaggage(ctx, c.bag)
	}
	return ctx
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run 'TestFromContext|TestCarrierContext' .`
Expected: PASS.

- [ ] **Step 5: Commit**

Run the validation gate, then:

```bash
git add carrier.go carrier_test.go
git commit -m "feat: add Carrier context bridges"
```

---

### Task 3: Binary encode (constants, AppendBinary, MarshalBinary)

**Files:**
- Modify: `carrier.go`
- Test: `carrier_test.go`

**Interfaces:**
- Consumes: `Carrier`, `IsEmpty` (Task 1).
- Produces: `func (c Carrier) AppendBinary(b []byte) ([]byte, error)`; `func (c Carrier) MarshalBinary() ([]byte, error)` (= `AppendBinary(nil)`). Constants `formatVersion=1`, tags `1/2/3`, `spanCoreLen=26`, `otxFlagRemote=0x01`, `MaxBytes=65536`. Wire: version byte, then `tag(uvarint)·len(uvarint)·value`; core emitted iff `sc.IsValid()`, tracestate iff valid core + non-empty, baggage iff `bag.Len()>0`. Empty carrier → `nil`.

- [ ] **Step 1: Write the failing test**

```go
func TestMarshalEmpty(t *testing.T) {
	b, err := Carrier{}.MarshalBinary()
	require.NoError(t, err)
	require.Nil(t, b)
}

func TestMarshalSpanOnlyBytes(t *testing.T) {
	c := New(testSC(t, false), baggage.Baggage{})
	b, err := c.MarshalBinary()
	require.NoError(t, err)
	// version(1) + tag(1) + len(26) + 26 core bytes = 29 bytes.
	require.Len(t, b, 29)
	require.Equal(t, byte(formatVersion), b[0])
	require.Equal(t, byte(tagSpanContext), b[1])
	require.Equal(t, byte(spanCoreLen), b[2])
	require.Equal(t, byte(trace.FlagsSampled), b[2+1+24]) // traceFlags byte
	require.Equal(t, byte(0), b[2+1+25])                  // otx-flags: not remote
}

func TestMarshalRemoteBaggageTags(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    testSC(t, true).TraceID(),
		SpanID:     testSC(t, true).SpanID(),
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	c := New(sc, testBaggage(t))
	b, err := c.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, byte(otxFlagRemote), b[2+1+25]) // remote bit set
	require.Contains(t, string(b), "userId=alice")   // baggage TLV present
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestMarshal' .`
Expected: FAIL — `undefined: formatVersion` / `MarshalBinary`.

- [ ] **Step 3: Write minimal implementation**

Add `"encoding"` and `"encoding/binary"` to imports, then:

```go
const formatVersion = 1

// MaxBytes bounds UnmarshalBinary input (§5.3). Set above the largest
// carrier a conformant producer can emit (24,675 bytes), so it never rejects a
// valid carrier; it only stops oversized/hostile input.
const MaxBytes = 65536

const (
	tagSpanContext = 1 // fixed 26-byte core value
	tagTraceState  = 2 // W3C tracestate string
	tagBaggage     = 3 // W3C baggage string
)

const (
	spanCoreLen   = 26   // 16 traceID + 8 spanID + 1 traceFlags + 1 otx-flags
	otxFlagRemote = 0x01 // otx-flags bit0
)

var (
	_ encoding.BinaryMarshaler = Carrier{}
	_ encoding.BinaryAppender  = Carrier{}
)

// MarshalBinary implements encoding.BinaryMarshaler. The empty carrier marshals
// to nil. It never fails in practice (inputs are valid OTel values).
func (c Carrier) MarshalBinary() ([]byte, error) { return c.AppendBinary(nil) }

// AppendBinary implements encoding.BinaryAppender, encoding the carrier into b
// with no per-call allocation when b has spare capacity.
func (c Carrier) AppendBinary(b []byte) ([]byte, error) {
	if c.IsEmpty() {
		return b, nil
	}
	b = append(b, formatVersion)
	if c.sc.IsValid() {
		var core [spanCoreLen]byte
		tid := c.sc.TraceID()
		copy(core[0:16], tid[:])
		sid := c.sc.SpanID()
		copy(core[16:24], sid[:])
		core[24] = byte(c.sc.TraceFlags())
		if c.sc.IsRemote() {
			core[25] |= otxFlagRemote
		}
		b = appendTLV(b, tagSpanContext, core[:])
		if ts := c.sc.TraceState().String(); ts != "" {
			b = appendTLV(b, tagTraceState, []byte(ts))
		}
	}
	if c.bag.Len() > 0 {
		b = appendTLV(b, tagBaggage, []byte(c.bag.String()))
	}
	return b, nil
}

func appendTLV(b []byte, tag uint64, val []byte) []byte {
	b = binary.AppendUvarint(b, tag)
	b = binary.AppendUvarint(b, uint64(len(val)))
	return append(b, val...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run 'TestMarshal' .`
Expected: PASS.

- [ ] **Step 5: Commit**

Run the validation gate, then:

```bash
git add carrier.go carrier_test.go
git commit -m "feat: add Carrier binary encoder"
```

---

### Task 4: Binary decode (UnmarshalBinary, HasOTel, sentinels)

**Files:**
- Modify: `carrier.go`
- Test: `carrier_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1 & 3 (constants, `AppendBinary`).
- Produces: `func (c *Carrier) UnmarshalBinary(data []byte) error`; `func HasOTel(b []byte) bool`; sentinels `ErrMalformed`, `ErrUnsupportedVersion`. Decoder rules per §6: nil/empty/version-only → empty (no error); `len>MaxBytes`/bad version/`Uvarint n<=0`/length-overrun/duplicate-known-tag/trailing-partial/core-len≠26 → fatal; invalid core → drop span; orphan tracestate → drop; tracestate parse fail → drop; baggage uses `baggage.Parse` result iff non-empty; unknown tag → skip. `HasOTel(b)` returns `len(b)>1 && b[0]==formatVersion` (no false negatives).

- [ ] **Step 1: Write the failing test**

```go
func roundTrip(t *testing.T, c Carrier) Carrier {
	t.Helper()
	b, err := c.MarshalBinary()
	require.NoError(t, err)
	var got Carrier
	require.NoError(t, got.UnmarshalBinary(b))
	return got
}

func TestRoundTripIdentity(t *testing.T) {
	ts, err := trace.ParseTraceState("vendor=value,acme=x")
	require.NoError(t, err)
	sc := testSC(t, true).WithTraceState(ts)
	cases := map[string]Carrier{
		"empty":        {},
		"span":         New(testSC(t, false), baggage.Baggage{}),
		"span+remote":  New(testSC(t, true), baggage.Baggage{}),
		"baggage":      New(trace.SpanContext{}, testBaggage(t)),
		"both":         New(testSC(t, false), testBaggage(t)),
		"tracestate":   New(sc, testBaggage(t)),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := roundTrip(t, c)
			require.True(t, got.SpanContext().Equal(c.SpanContext()))
			require.Equal(t, c.Baggage().Len(), got.Baggage().Len())
			require.Equal(t, c.SpanContext().TraceState().String(), got.SpanContext().TraceState().String())
			// property-aware baggage check
			for _, m := range c.Baggage().Members() {
				require.Equal(t, m.Value(), got.Baggage().Member(m.Key()).Value())
				require.Equal(t, m.Properties(), got.Baggage().Member(m.Key()).Properties())
			}
		})
	}
}

func TestUnmarshalEmptyForms(t *testing.T) {
	for _, in := range [][]byte{nil, {}, {formatVersion}} {
		var c Carrier
		require.NoError(t, c.UnmarshalBinary(in))
		require.True(t, c.IsEmpty())
	}
}

func TestUnmarshalMalformed(t *testing.T) {
	core := func() []byte { b, _ := New(testSC(t, false), baggage.Baggage{}).MarshalBinary(); return b }()
	dup := append(append([]byte{}, core...), core[1:]...) // version + two tag-1 entries
	big := make([]byte, MaxBytes+1)
	cases := map[string][]byte{
		"badVersion":    {2, tagBaggage, 1, 'x'},
		"lenOverrun":    {formatVersion, tagBaggage, 9, 'a'},
		"truncatedTLV":  {formatVersion, tagBaggage},
		"coreWrongLen":  {formatVersion, tagSpanContext, 3, 1, 2, 3},
		"duplicateTag":  dup,
		"tooLarge":      big,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var c Carrier
			err := c.UnmarshalBinary(in)
			require.Error(t, err)
			if name == "badVersion" {
				require.ErrorIs(t, err, ErrUnsupportedVersion)
			} else {
				require.ErrorIs(t, err, ErrMalformed)
			}
		})
	}
}

func TestUnmarshalDecoderRules(t *testing.T) {
	// Invalid core (valid traceID, zero spanID) → span dropped.
	partial := trace.NewSpanContext(trace.SpanContextConfig{TraceID: testSC(t, false).TraceID()})
	require.False(t, partial.IsValid())
	c := roundTrip(t, New(partial, testBaggage(t)))
	require.False(t, c.SpanContext().IsValid())
	require.Equal(t, 1, c.Baggage().Len())

	// Unknown tag is skipped, known fields survive.
	good, _ := New(testSC(t, false), baggage.Baggage{}).MarshalBinary()
	withUnknown := appendTLV(append([]byte{}, good...), 99, []byte("future"))
	var d Carrier
	require.NoError(t, d.UnmarshalBinary(withUnknown))
	require.True(t, d.SpanContext().Equal(testSC(t, false)))
}

func TestHasOTel(t *testing.T) {
	full, _ := New(testSC(t, false), baggage.Baggage{}).MarshalBinary()
	require.True(t, HasOTel(full))
	require.False(t, HasOTel(nil))
	require.False(t, HasOTel([]byte{}))
	require.False(t, HasOTel([]byte{formatVersion}))
	require.False(t, HasOTel([]byte{2, 3, 4})) // wrong version byte
}

func TestOverLimitBaggageDegradation(t *testing.T) {
	// >64 members, small values (<8192 bytes) → kept, truncated.
	many := baggage.Baggage{}
	for i := 0; i < 70; i++ {
		m, err := baggage.NewMemberRaw("k"+string(rune('a'+i%26))+string(rune('a'+i/26)), "v")
		require.NoError(t, err)
		many, err = many.SetMember(m)
		require.NoError(t, err)
	}
	got := roundTrip(t, New(testSC(t, false), many))
	require.True(t, got.SpanContext().IsValid(), "span retained")
	require.Greater(t, got.Baggage().Len(), 0, ">64-member baggage kept (truncated)")
	require.LessOrEqual(t, got.Baggage().Len(), 64)

	// >8192 bytes → dropped entirely, span retained.
	huge := baggage.Baggage{}
	for i := 0; i < 200; i++ {
		m, err := baggage.NewMemberRaw("key"+string(rune('A'+i%26))+string(rune('A'+i/26)), "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv")
		require.NoError(t, err)
		huge, err = huge.SetMember(m)
		require.NoError(t, err)
	}
	require.Greater(t, len(huge.String()), 8192)
	got = roundTrip(t, New(testSC(t, false), huge))
	require.True(t, got.SpanContext().IsValid(), "span retained")
	require.Equal(t, 0, got.Baggage().Len(), ">8192-byte baggage dropped")
}

func TestRoundTripLargeTraceState(t *testing.T) {
	// 30 members serialize to > 512 bytes — the regression guard for the
	// removed per-field byte cap (§9).
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "key%02d=%s", i, strings.Repeat("a", 20))
	}
	ts, err := trace.ParseTraceState(sb.String())
	require.NoError(t, err)
	require.Greater(t, len(ts.String()), 512)
	got := roundTrip(t, New(testSC(t, false).WithTraceState(ts), baggage.Baggage{}))
	require.Equal(t, ts.String(), got.SpanContext().TraceState().String())
}

func TestDecoderEdgeCases(t *testing.T) {
	// Orphan tracestate (tag 2 without a valid core) → dropped, carrier empty.
	orphan := appendTLV([]byte{formatVersion}, tagTraceState, []byte("vendor=value"))
	var a Carrier
	require.NoError(t, a.UnmarshalBinary(orphan))
	require.True(t, a.IsEmpty())

	// Unknown-only frame → empty carrier, no error.
	unknownOnly := appendTLV([]byte{formatVersion}, 99, []byte("future"))
	var b Carrier
	require.NoError(t, b.UnmarshalBinary(unknownOnly))
	require.True(t, b.IsEmpty())

	// Hand-crafted core with zero IDs (decode-side invalid) → span dropped.
	zeroCore := appendTLV([]byte{formatVersion}, tagSpanContext, make([]byte, spanCoreLen))
	var c Carrier
	require.NoError(t, c.UnmarshalBinary(zeroCore))
	require.False(t, c.SpanContext().IsValid())
	require.True(t, c.IsEmpty())
}
```

Add `"fmt"` and `"strings"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestRoundTrip|TestUnmarshal|TestHasOTel|TestOverLimit|TestDecoderEdgeCases' .`
Expected: FAIL — `undefined: (*Carrier).UnmarshalBinary` / `HasOTel` / `ErrMalformed`.

- [ ] **Step 3: Write minimal implementation**

Add `"errors"` to imports, then:

```go
// Sentinel decode errors.
var (
	ErrMalformed          = errors.New("otx: malformed carrier")
	ErrUnsupportedVersion = errors.New("otx: unsupported carrier version")
)

var _ encoding.BinaryUnmarshaler = (*Carrier)(nil)

// HasOTel reports, without a full decode, whether b could contain OTel info. It
// returns false only for nil, empty, version-only, or wrong-version input, and
// never returns false for a well-formed non-empty carrier. It may return true
// for malformed bytes (UnmarshalBinary then rejects them); it is a hot-path
// skip filter, not a validator.
func HasOTel(b []byte) bool {
	return len(b) > 1 && b[0] == formatVersion
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler. It resets the receiver,
// then decodes data per the §6 rules. Input is treated as hostile: total size
// and every field length are bounded before allocation, and it never panics.
func (c *Carrier) UnmarshalBinary(data []byte) error {
	*c = Carrier{}
	if len(data) == 0 {
		return nil
	}
	if len(data) > MaxBytes {
		return ErrMalformed
	}
	if data[0] != formatVersion {
		return ErrUnsupportedVersion
	}
	rest := data[1:]

	var (
		haveCore, haveTS, haveBag bool
		tid                       trace.TraceID
		sid                       trace.SpanID
		flags                     trace.TraceFlags
		remote                    bool
		tsStr, bagStr             string
	)

	for len(rest) > 0 {
		tag, n := binary.Uvarint(rest)
		if n <= 0 {
			return ErrMalformed
		}
		rest = rest[n:]
		l, n := binary.Uvarint(rest)
		if n <= 0 {
			return ErrMalformed
		}
		rest = rest[n:]
		if l > uint64(len(rest)) {
			return ErrMalformed
		}
		val := rest[:l]
		rest = rest[l:]

		switch tag {
		case tagSpanContext:
			if haveCore {
				return ErrMalformed
			}
			haveCore = true
			if len(val) != spanCoreLen {
				return ErrMalformed
			}
			copy(tid[:], val[0:16])
			copy(sid[:], val[16:24])
			flags = trace.TraceFlags(val[24])
			remote = val[25]&otxFlagRemote != 0
		case tagTraceState:
			if haveTS {
				return ErrMalformed
			}
			haveTS = true
			tsStr = string(val)
		case tagBaggage:
			if haveBag {
				return ErrMalformed
			}
			haveBag = true
			bagStr = string(val)
		default:
			// unknown tag: already advanced past val — skip.
		}
	}

	if haveCore {
		cfg := trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: flags, Remote: remote}
		if haveTS {
			if ts, err := trace.ParseTraceState(tsStr); err == nil {
				cfg.TraceState = ts // parse failure → tracestate dropped
			}
		}
		if sc := trace.NewSpanContext(cfg); sc.IsValid() {
			c.sc = sc // invalid core → span dropped
		}
	}
	if haveBag {
		// Mirror stock W3C extract: use the parse result iff non-empty
		// (>8192 bytes → empty/dropped; >64 members → truncated/kept).
		if bag, _ := baggage.Parse(bagStr); bag.Len() > 0 {
			c.bag = bag
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run 'TestRoundTrip|TestUnmarshal|TestHasOTel|TestOverLimit|TestDecoderEdgeCases' .`
Expected: PASS.

- [ ] **Step 5: Commit**

Run the validation gate, then:

```bash
git add carrier.go carrier_test.go
git commit -m "feat: add Carrier binary decoder and HasOTel"
```

---

### Task 5: W3C text interop (W3C, ParseW3C)

**Files:**
- Modify: `carrier.go`
- Test: `carrier_test.go`

**Interfaces:**
- Consumes: `Carrier`, `Context`, `FromContext` (Tasks 1–2), `ErrMalformed` (Task 4).
- Produces: `func (c Carrier) W3C() (traceparent, tracestate, baggage string)`; `func ParseW3C(traceparent, tracestate, baggage string) (Carrier, error)`. Both delegate to the stock `propagation.TraceContext`/`Baggage`, so output matches OTel exactly (flags masked, invalid span → empty traceparent). `ParseW3C` returns `ErrMalformed` only when a non-empty `traceparent` fails to extract.

- [ ] **Step 1: Write the failing test**

```go
func TestW3CMatchesStockPropagator(t *testing.T) {
	c := New(testSC(t, false), testBaggage(t))
	tp, ts, bg := c.W3C()

	// Compare against the stock propagators directly.
	mc := propagation.MapCarrier{}
	ctx := c.Context(context.Background())
	propagation.TraceContext{}.Inject(ctx, mc)
	propagation.Baggage{}.Inject(ctx, mc)
	require.Equal(t, mc["traceparent"], tp)
	require.Equal(t, mc["tracestate"], ts)
	require.Equal(t, mc["baggage"], bg)
}

func TestW3CFlagMaskingAndInvalidSpan(t *testing.T) {
	// Private flag bits are masked away in W3C text.
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: testSC(t, false).TraceID(), SpanID: testSC(t, false).SpanID(),
		TraceFlags: trace.TraceFlags(0xff),
	})
	tp, _, _ := New(sc, baggage.Baggage{}).W3C()
	require.True(t, strings.HasSuffix(tp, "-01"), "flags masked to sampled: %q", tp)

	// Invalid span → empty traceparent.
	tp, _, _ = New(trace.SpanContext{}, testBaggage(t)).W3C()
	require.Empty(t, tp)
}

func TestParseW3CRoundTrip(t *testing.T) {
	ts, err := trace.ParseTraceState("vendor=value")
	require.NoError(t, err)
	orig := New(testSC(t, false).WithTraceState(ts), testBaggage(t))
	got, err := ParseW3C(orig.W3C())
	require.NoError(t, err)
	require.Equal(t, orig.SpanContext().TraceID(), got.SpanContext().TraceID())
	require.Equal(t, orig.SpanContext().SpanID(), got.SpanContext().SpanID())
	require.Equal(t, "value", got.SpanContext().TraceState().Get("vendor"))
	require.Equal(t, "alice", got.Baggage().Member("userId").Value())

	// Malformed traceparent → ErrMalformed.
	_, err = ParseW3C("not-a-traceparent", "", "")
	require.ErrorIs(t, err, ErrMalformed)
}
```

Add `"strings"` and `"go.opentelemetry.io/otel/propagation"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestW3C|TestParseW3C' .`
Expected: FAIL — `undefined: (Carrier).W3C` / `ParseW3C`.

- [ ] **Step 3: Write minimal implementation**

Add `"fmt"` and `"go.opentelemetry.io/otel/propagation"` to the `carrier.go` imports, then:

```go
// W3C returns the W3C text forms (traceparent / tracestate / baggage),
// delegating to OTel's stock propagators so output matches them exactly. This
// is intentionally lossy relative to the binary codec: an invalid span yields
// an empty traceparent and trace-flags are masked to the W3C-defined bits. Any
// of the three results may be empty.
func (c Carrier) W3C() (traceparent, tracestate, baggage string) {
	ctx := c.Context(context.Background())
	mc := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, mc)
	propagation.Baggage{}.Inject(ctx, mc)
	return mc["traceparent"], mc["tracestate"], mc["baggage"]
}

// ParseW3C builds a Carrier from W3C text forms via OTel's stock extractors.
// Empty strings yield absent components. It returns ErrMalformed only when a
// non-empty traceparent fails to extract a valid span context.
func ParseW3C(traceparent, tracestate, baggage string) (Carrier, error) {
	mc := propagation.MapCarrier{}
	if traceparent != "" {
		mc["traceparent"] = traceparent
	}
	if tracestate != "" {
		mc["tracestate"] = tracestate
	}
	if baggage != "" {
		mc["baggage"] = baggage
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), mc)
	ctx = propagation.Baggage{}.Extract(ctx, mc)
	c, _ := FromContext(ctx)
	if traceparent != "" && !c.sc.IsValid() {
		return Carrier{}, fmt.Errorf("%w: invalid traceparent", ErrMalformed)
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run 'TestW3C|TestParseW3C' .`
Expected: PASS.

- [ ] **Step 5: Commit**

Run the validation gate, then:

```bash
git add carrier.go carrier_test.go
git commit -m "feat: add Carrier W3C text interop"
```

---

### Task 6: Fuzz + allocation benchmarks

**Files:**
- Create: `carrier_fuzz_test.go`

**Interfaces:**
- Consumes: `Carrier`, `UnmarshalBinary`, `HasOTel`, `MarshalBinary`, `AppendBinary`, `FromContext` (Tasks 1–4), the `testSC`/`testBaggage` helpers (Task 1).
- Produces: `FuzzUnmarshalBinary`, `FuzzHasOTel`, and alloc benchmarks guarding I2/I3.

- [ ] **Step 1: Write the failing test**

```go
package carrier

import (
	"context"
	"testing"
)

func FuzzUnmarshalBinary(f *testing.F) {
	for _, seed := range [][]byte{nil, {formatVersion}, {formatVersion, tagSpanContext, spanCoreLen}} {
		f.Add(seed)
	}
	if b, err := New(testSCB(), testBaggageB()).MarshalBinary(); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var c Carrier
		_ = c.UnmarshalBinary(data) // must never panic; error is fine
		_ = HasOTel(data)           // must never panic
	})
}

func FuzzHasOTel(f *testing.F) {
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, data []byte) { _ = HasOTel(data) })
}

func BenchmarkFromContextEmpty(b *testing.B) {
	ctx := context.Background()
	if n := testing.AllocsPerRun(100, func() { _, _ = FromContext(ctx) }); n != 0 {
		b.Fatalf("FromContext(empty) allocated %v times, want 0", n)
	}
}

func BenchmarkHasOTelAllocs(b *testing.B) {
	full, _ := New(testSCB(), testBaggageB()).MarshalBinary()
	if n := testing.AllocsPerRun(100, func() { _ = HasOTel(full) }); n != 0 {
		b.Fatalf("HasOTel allocated %v times, want 0", n)
	}
}

func BenchmarkAppendBinaryReuse(b *testing.B) {
	c := New(testSCB(), testBaggageB())
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf = buf[:0]
		buf, _ = c.AppendBinary(buf)
	}
}
```

Add bench helpers (no `*testing.T`) to `carrier_fuzz_test.go`:

```go
func testSCB() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
}

func testBaggageB() baggage.Baggage {
	m, _ := baggage.NewMember("userId", "alice")
	bag, _ := baggage.New(m)
	return bag
}
```

Add `"go.opentelemetry.io/otel/baggage"` and `"go.opentelemetry.io/otel/trace"` to the fuzz-file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'Fuzz|Benchmark' .`
Expected: FAIL only if a name is mistyped; otherwise it may already pass — confirm the file compiles and the alloc-bench `Fatalf` guards are exercised with `go test -run Benchmark -bench . .`.

- [ ] **Step 3: Confirm fuzz finds no panics**

Run: `go test -run x -fuzz FuzzUnmarshalBinary -fuzztime 20s .`
Expected: no crash; `go test` reports the fuzz completed.

Run: `go test -run x -fuzz FuzzHasOTel -fuzztime 10s .`
Expected: no crash.

- [ ] **Step 4: Run the alloc benchmarks as tests**

Run: `go test -race -run 'BenchmarkFromContextEmpty|BenchmarkHasOTelAllocs' -bench 'FromContextEmpty|HasOTelAllocs' .`
Expected: PASS (the `AllocsPerRun != 0` guards do not fire).

- [ ] **Step 5: Commit**

Run the validation gate, then:

```bash
git add carrier_fuzz_test.go
git commit -m "test: add Carrier fuzz and allocation benchmarks"
```

---

## Final validation

- [ ] Run the full gate once more: `go fix ./...` → `make lint` → `make test`. All green.
- [ ] Update the design doc status line in `docs/design/2026-06-23-ipc-otel-carrier-design.md` from "implementation-ready" to "implemented" and commit (`docs: mark IPC carrier design implemented`).
