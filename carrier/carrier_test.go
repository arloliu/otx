package carrier

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

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

// testSC returns a valid, recognizable span context (the W3C example IDs).
func testSC(t *testing.T, remote bool) trace.SpanContext {
	t.Helper()
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{
			0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6,
			0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36,
		},
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
		"empty":       {},
		"span":        New(testSC(t, false), baggage.Baggage{}),
		"span+remote": New(testSC(t, true), baggage.Baggage{}),
		"baggage":     New(trace.SpanContext{}, testBaggage(t)),
		"both":        New(testSC(t, false), testBaggage(t)),
		"tracestate":  New(sc, testBaggage(t)),
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
	// Duplicate tag-2 (tracestate) and tag-3 (baggage): §6 fails on a duplicate
	// of ANY known tag (1, 2, or 3), not just the span core.
	dupTS := appendTLV([]byte{formatVersion}, tagTraceState, []byte("vendor=a"))
	dupTS = appendTLV(dupTS, tagTraceState, []byte("vendor=b"))
	dupBag := appendTLV([]byte{formatVersion}, tagBaggage, []byte("k=v"))
	dupBag = appendTLV(dupBag, tagBaggage, []byte("k=w"))
	big := make([]byte, MaxBytes+1)
	cases := map[string][]byte{
		"badVersion":      {2, tagBaggage, 1, 'x'},
		"lenOverrun":      {formatVersion, tagBaggage, 9, 'a'},
		"truncatedTLV":    {formatVersion, tagBaggage},
		"coreWrongLen":    {formatVersion, tagSpanContext, 3, 1, 2, 3},
		"duplicateTag":    dup,
		"duplicateTagTS":  dupTS,
		"duplicateTagBag": dupBag,
		"tooLarge":        big,
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
	for i := range 70 {
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
	for i := range 200 {
		val := "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv"
		m, err := baggage.NewMemberRaw(
			"key"+string(rune('A'+i%26))+string(rune('A'+i/26)), val,
		)
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
	for i := range 30 {
		if i > 0 {
			sb.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&sb, "key%02d=%s", i, strings.Repeat("a", 20))
	}
	ts, err := trace.ParseTraceState(sb.String())
	require.NoError(t, err)
	require.Greater(t, len(ts.String()), 512)
	got := roundTrip(t, New(testSC(t, false).WithTraceState(ts), baggage.Baggage{}))
	require.Equal(t, ts.String(), got.SpanContext().TraceState().String())
}

func TestRoundTripNearLimitBaggage(t *testing.T) {
	// A conformant baggage (≤64 members, <8192 bytes) whose serialization sits
	// near the 8192-byte limit round-trips losslessly — the §9 regression guard
	// for the removed per-field byte cap, on the valid (kept) side of §6's
	// degradation rules.
	members := make([]baggage.Member, 0, 59)
	for i := range 59 {
		m, err := baggage.NewMemberRaw(fmt.Sprintf("key%02d", i), strings.Repeat("a", 130))
		require.NoError(t, err)
		members = append(members, m)
	}
	bag, err := baggage.New(members...)
	require.NoError(t, err)
	require.Greater(t, len(bag.String()), 8000, "baggage must sit near the 8192-byte limit")
	require.LessOrEqual(t, len(bag.String()), 8192, "baggage must stay within the limit (valid)")

	got := roundTrip(t, New(testSC(t, false), bag))
	require.True(t, got.SpanContext().IsValid(), "span retained")
	require.Equal(t, bag.Len(), got.Baggage().Len(), "all members kept")
	for _, m := range bag.Members() {
		require.Equal(t, m.Value(), got.Baggage().Member(m.Key()).Value(),
			"member %q value preserved", m.Key())
	}
}

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
	// W3C masks trace-flags to FlagsSampled|FlagsRandom (0x03); 0xff -> 0x03.
	require.True(t, strings.HasSuffix(tp, "-03"), "flags masked to W3C bits: %q", tp)

	// Not-sampled flags (0x00) behave per OTel: traceparent ends with "-00".
	notSampled := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: testSC(t, false).TraceID(), SpanID: testSC(t, false).SpanID(),
		TraceFlags: 0,
	})
	tp, _, _ = New(notSampled, baggage.Baggage{}).W3C()
	require.True(t, strings.HasSuffix(tp, "-00"), "not-sampled flags emit -00: %q", tp)

	// Invalid span → empty traceparent.
	tp, _, _ = New(trace.SpanContext{}, testBaggage(t)).W3C()
	require.Empty(t, tp)
}

func TestParseW3CTraceStateWithoutTraceparent(t *testing.T) {
	// tracestate without a traceparent yields an absent span and no error
	// (OTel's TraceContext.Extract drops orphan tracestate).
	c, err := ParseW3C("", "vendor=foo", "")
	require.NoError(t, err)
	require.False(t, c.SpanContext().IsValid())
	require.True(t, c.IsEmpty())
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

// TestUnmarshalReceiverPreservedOnError is a regression test for the P0 fix:
// UnmarshalBinary must not mutate the receiver when it returns an error.
func TestUnmarshalReceiverPreservedOnError(t *testing.T) {
	seed := New(testSC(t, false), testBaggage(t))

	// Build a valid core for use in hand-crafted frames.
	validCore := make([]byte, spanCoreLen)
	tid := seed.SpanContext().TraceID()
	copy(validCore[0:16], tid[:])
	sid := seed.SpanContext().SpanID()
	copy(validCore[16:24], sid[:])
	validCore[24] = byte(seed.SpanContext().TraceFlags())

	// Craft a duplicate-tag frame (two tag-1 entries).
	frame := appendTLV([]byte{formatVersion}, tagSpanContext, validCore)
	frame = appendTLV(frame, tagSpanContext, validCore)

	// Craft a wrong-core-len frame (tag-1 with 3 bytes instead of 26).
	wrongCore := appendTLV([]byte{formatVersion}, tagSpanContext, []byte{1, 2, 3})

	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"badVersion", []byte{2, tagBaggage, 1, 'x'}, ErrUnsupportedVersion},
		{"tooLarge", make([]byte, MaxBytes+1), ErrMalformed},
		{"lenOverrun", []byte{formatVersion, tagBaggage, 9, 'a'}, ErrMalformed},
		{"truncatedTLV", []byte{formatVersion, tagBaggage}, ErrMalformed},
		{"duplicateTag", frame, ErrMalformed},
		{"wrongCoreLen", wrongCore, ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := seed
			err := (&c).UnmarshalBinary(tc.in)
			require.ErrorIs(t, err, tc.want)
			require.True(t, c.SpanContext().Equal(seed.SpanContext()), "receiver SpanContext mutated on error")
			require.Equal(t, seed.Baggage().Len(), c.Baggage().Len(), "receiver Baggage mutated on error")
		})
	}
}

func TestUnmarshalSuccessClearsReusedReceiver(t *testing.T) {
	// A successful decode into a reused, previously-populated receiver must
	// replace ALL fields via the *c = out whole-struct assignment, not merge.
	c := New(testSC(t, false), testBaggage(t)) // seed: span + baggage

	// Baggage-only frame → the stale span must be cleared.
	bagOnly, err := New(trace.SpanContext{}, testBaggage(t)).MarshalBinary()
	require.NoError(t, err)
	require.NoError(t, (&c).UnmarshalBinary(bagOnly))
	require.False(t, c.SpanContext().IsValid(), "stale span cleared on successful decode")
	require.Equal(t, 1, c.Baggage().Len())

	// Version-only (empty success) frame → the receiver is fully cleared.
	require.NoError(t, (&c).UnmarshalBinary([]byte{formatVersion}))
	require.True(t, c.IsEmpty(), "reused receiver fully cleared on empty decode")
}

// TestTraceFlagsBinaryRoundTrip verifies that all TraceFlags bits survive
// binary encoding/decoding, unlike W3C which masks to only the defined bits.
func TestTraceFlagsBinaryRoundTrip(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    testSC(t, false).TraceID(),
		SpanID:     testSC(t, false).SpanID(),
		TraceFlags: trace.TraceFlags(0xff),
	})
	got := roundTrip(t, New(sc, baggage.Baggage{}))
	require.Equal(t, trace.TraceFlags(0xff), got.SpanContext().TraceFlags(),
		"binary codec must preserve all TraceFlags bits, unlike W3C masking")
}

// TestUnmarshalInvalidCoreDropped verifies that a core with a nonzero TraceID
// but zero SpanID (structurally invalid per OTel) is silently dropped.
func TestUnmarshalInvalidCoreDropped(t *testing.T) {
	// valid 16-byte TraceID, zero 8-byte SpanID, flags=0x01, otxFlags=0
	coreBytes := make([]byte, spanCoreLen)
	tid := testSC(t, false).TraceID()
	copy(coreBytes[0:16], tid[:]) // nonzero TraceID
	// [16:24] zero SpanID
	coreBytes[24] = byte(trace.FlagsSampled)
	// coreBytes[25] = 0

	frame := appendTLV([]byte{formatVersion}, tagSpanContext, coreBytes)
	var c Carrier
	require.NoError(t, c.UnmarshalBinary(frame))
	require.False(t, c.SpanContext().IsValid(), "invalid core (zero SpanID) must be dropped")
	require.True(t, c.IsEmpty())
}

// TestUnmarshalUnknownTagBeforeKnown verifies that an unknown tag appearing
// before a known tag does not prevent the known tag from being decoded.
func TestUnmarshalUnknownTagBeforeKnown(t *testing.T) {
	// unknown tag 99 appears BEFORE tag-1 core
	frame := appendTLV([]byte{formatVersion}, 99, []byte("future"))
	validCore := make([]byte, spanCoreLen)
	tid := testSC(t, false).TraceID()
	copy(validCore[0:16], tid[:])
	sid := testSC(t, false).SpanID()
	copy(validCore[16:24], sid[:])
	validCore[24] = byte(trace.FlagsSampled)
	frame = appendTLV(frame, tagSpanContext, validCore)

	var c Carrier
	require.NoError(t, c.UnmarshalBinary(frame))
	require.True(t, c.SpanContext().Equal(testSC(t, false)), "known tag after unknown must decode intact")
}

// TestUnmarshalHostileUvarint checks both incomplete (n==0) and overflow (n<0)
// uvarint encodings, plus non-canonical (tolerated) ones — the three uvarint
// cases §6 and §9 distinguish.
func TestUnmarshalHostileUvarint(t *testing.T) {
	// (a) incomplete uvarint (binary.Uvarint n == 0): 10 continuation bytes that
	// never terminate. The buffer ends before the value does.
	incomplete := append([]byte{formatVersion}, bytes.Repeat([]byte{0x80}, 10)...)
	var a Carrier
	require.ErrorIs(t, a.UnmarshalBinary(incomplete), ErrMalformed,
		"incomplete uvarint (n==0) must return ErrMalformed")

	// (b) overflow uvarint (binary.Uvarint n < 0): a 10-byte uvarint whose value
	// exceeds 64 bits. Nine 0x80 continuation bytes carry 63 bits; the tenth byte
	// 0x02 (>1, terminating) pushes past 64 bits, so binary.Uvarint returns n<0.
	overflow := append([]byte{formatVersion}, append(bytes.Repeat([]byte{0x80}, 9), 0x02)...)
	var b Carrier
	require.ErrorIs(t, b.UnmarshalBinary(overflow), ErrMalformed,
		"overflow uvarint (n<0) must return ErrMalformed")

	// (c) non-canonical uvarint tag: 0x81,0x00 encodes value 1 (non-minimal).
	// Tolerated: should decode successfully.
	validCore := make([]byte, spanCoreLen)
	tid := testSC(t, false).TraceID()
	copy(validCore[0:16], tid[:])
	sid := testSC(t, false).SpanID()
	copy(validCore[16:24], sid[:])
	validCore[24] = byte(trace.FlagsSampled)
	// Manually build: version + non-canonical tag(1) as 0x81,0x00 + len(26) + core
	nonCanonical := []byte{formatVersion, 0x81, 0x00, spanCoreLen}
	nonCanonical = append(nonCanonical, validCore...)
	var d Carrier
	require.NoError(t, d.UnmarshalBinary(nonCanonical),
		"non-canonical uvarint tag must be tolerated")
	require.True(t, d.SpanContext().Equal(testSC(t, false)))
}

// TestUnmarshalOverLimitTracestateDropped verifies that a tracestate with more
// than 32 members is silently dropped while the span context is retained.
func TestUnmarshalOverLimitTracestateDropped(t *testing.T) {
	// Build a tracestate string with 33 key=value entries (>32 → ParseTraceState fails).
	var sb strings.Builder
	for i := range 33 {
		if i > 0 {
			sb.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&sb, "key%02d=val", i)
	}
	bigTS := sb.String()
	// Verify it actually fails to parse.
	_, err := trace.ParseTraceState(bigTS)
	require.Error(t, err, "sanity: >32 members should fail ParseTraceState")

	// Build the valid span context core bytes.
	validCore := make([]byte, spanCoreLen)
	tid := testSC(t, false).TraceID()
	copy(validCore[0:16], tid[:])
	sid := testSC(t, false).SpanID()
	copy(validCore[16:24], sid[:])
	validCore[24] = byte(trace.FlagsSampled)

	// Build frame: version + tag-1 core + tag-2 over-limit tracestate.
	frame := appendTLV([]byte{formatVersion}, tagSpanContext, validCore)
	frame = appendTLV(frame, tagTraceState, []byte(bigTS))

	var c Carrier
	require.NoError(t, c.UnmarshalBinary(frame))
	require.True(t, c.SpanContext().Equal(testSC(t, false)), "span context must be retained")
	require.Equal(t, "", c.SpanContext().TraceState().String(), "over-limit tracestate must be dropped")
}
