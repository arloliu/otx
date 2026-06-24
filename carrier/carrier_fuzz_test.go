package carrier

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// FuzzUnmarshalBinary guards invariant I3: UnmarshalBinary and HasOTel never
// panic on arbitrary input.
func FuzzUnmarshalBinary(f *testing.F) {
	for _, seed := range [][]byte{nil, {formatVersion}, {formatVersion, tagSpanContext, spanCoreLen}} {
		f.Add(seed)
	}
	if b, err := New(testSCB(), testBaggageB()).MarshalBinary(); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		var c Carrier
		_ = c.UnmarshalBinary(data) // must never panic; error is fine
		_ = HasOTel(data)           // must never panic
	})
}

// FuzzHasOTel guards invariant I2: HasOTel never panics on arbitrary input.
func FuzzHasOTel(f *testing.F) {
	f.Add([]byte(nil))
	f.Fuzz(func(_ *testing.T, data []byte) { _ = HasOTel(data) })
}

// BenchmarkFromContextEmpty guards invariant I2: FromContext on an empty
// context must not allocate (the hot-path fast exit must remain alloc-free).
func BenchmarkFromContextEmpty(b *testing.B) {
	ctx := context.Background()
	if n := testing.AllocsPerRun(100, func() { _, _ = FromContext(ctx) }); n != 0 {
		b.Fatalf("FromContext(empty) allocated %v times, want 0", n)
	}
}

// BenchmarkHasOTelAllocs guards invariant I2: HasOTel must not allocate.
func BenchmarkHasOTelAllocs(b *testing.B) {
	full, _ := New(testSCB(), testBaggageB()).MarshalBinary()
	if n := testing.AllocsPerRun(100, func() { _ = HasOTel(full) }); n != 0 {
		b.Fatalf("HasOTel allocated %v times, want 0", n)
	}
}

// BenchmarkHasOTelAllocsAll guards that HasOTel never allocates on any input shape.
func BenchmarkHasOTelAllocsAll(b *testing.B) {
	inputs := [][]byte{
		nil,
		{},
		{formatVersion},
		{0x99, 0x01, 0x02}, // malformed / wrong version
	}
	for _, in := range inputs {
		if n := testing.AllocsPerRun(100, func() { _ = HasOTel(in) }); n != 0 {
			b.Fatalf("HasOTel(%v) allocated %v times, want 0", in, n)
		}
	}
}

// BenchmarkMarshalBinary reports allocs for a full carrier marshal.
func BenchmarkMarshalBinary(b *testing.B) {
	c := New(testSCB(), testBaggageB())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = c.MarshalBinary()
	}
}

// BenchmarkUnmarshalBinary reports allocs for a full carrier unmarshal.
func BenchmarkUnmarshalBinary(b *testing.B) {
	data, _ := New(testSCB(), testBaggageB()).MarshalBinary()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var c Carrier
		_ = c.UnmarshalBinary(data)
	}
}

// BenchmarkAppendBinaryReuse measures AppendBinary with a reused buffer;
// reports allocs without asserting zero (a single alloc on first use is fine).
func BenchmarkAppendBinaryReuse(b *testing.B) {
	c := New(testSCB(), testBaggageB())
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf = buf[:0]
		buf, _ = c.AppendBinary(buf)
	}
}

// testSCB returns a valid, recognizable span context without requiring *testing.T,
// for use in benchmark and fuzz setup.
func testSCB() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{
			0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6,
			0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36,
		},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
}

// testBaggageB returns baggage with one member, without requiring *testing.T,
// for use in benchmark and fuzz setup.
func testBaggageB() baggage.Baggage {
	m, _ := baggage.NewMember("userId", "alice")
	bag, _ := baggage.New(m)
	return bag
}
