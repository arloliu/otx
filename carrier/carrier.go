// Package carrier provides a binary-marshalable snapshot of OpenTelemetry
// propagation state (span context + baggage) for in-process IPC boundaries.
package carrier

import (
	"context"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

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

// ErrMalformed reports binary input that is structurally invalid (bad framing,
// length overrun, duplicate tag, wrong core length, or oversized input).
var ErrMalformed = errors.New("otx: malformed carrier")

// ErrUnsupportedVersion reports an unrecognized format version byte.
var ErrUnsupportedVersion = errors.New("otx: unsupported carrier version")

// Carrier is a portable, marshalable snapshot of OTel propagation state (span
// context + baggage). It is an immutable value; the zero value is the empty
// carrier. Copying a Carrier is cheap and safe for concurrent reads. An invalid
// span context (either ID zero) is treated as absent by every operation.
type Carrier struct {
	sc  trace.SpanContext
	bag baggage.Baggage
}

var (
	_ encoding.BinaryMarshaler   = Carrier{}
	_ encoding.BinaryAppender    = Carrier{}
	_ encoding.BinaryUnmarshaler = (*Carrier)(nil)
)

// New builds a Carrier from explicit OTel values. If sc.IsValid() is
// false the span context is treated as absent for all later operations
// (marshal, W3C, Context, IsEmpty).
func New(sc trace.SpanContext, bag baggage.Baggage) Carrier {
	return Carrier{sc: sc, bag: bag}
}

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

// HasOTel reports, without a full decode, whether b could contain OTel info. It
// returns false only for nil, empty, version-only, or wrong-version input, and
// never returns false for a well-formed non-empty carrier. It may return true
// for malformed bytes (UnmarshalBinary then rejects them); it is a hot-path
// skip filter, not a validator.
func HasOTel(b []byte) bool {
	return len(b) > 1 && b[0] == formatVersion
}

// ParseW3C builds a Carrier from W3C text forms via OTel's stock extractors.
// Empty strings yield absent components. It returns ErrMalformed only when a
// non-empty traceparent fails to extract a valid span context.
func ParseW3C(traceparent, tracestate, bg string) (Carrier, error) {
	mc := propagation.MapCarrier{}
	if traceparent != "" {
		mc["traceparent"] = traceparent
	}
	if tracestate != "" {
		mc["tracestate"] = tracestate
	}
	if bg != "" {
		mc["baggage"] = bg
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), mc)
	ctx = propagation.Baggage{}.Extract(ctx, mc)
	c, _ := FromContext(ctx)
	if traceparent != "" && !c.sc.IsValid() {
		return Carrier{}, fmt.Errorf("%w: invalid traceparent", ErrMalformed)
	}

	return c, nil
}

func appendTLV(b []byte, tag uint64, val []byte) []byte {
	b = binary.AppendUvarint(b, tag)
	b = binary.AppendUvarint(b, uint64(len(val)))
	return append(b, val...)
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

// W3C returns the W3C text forms (traceparent / tracestate / baggage),
// delegating to OTel's stock propagators so output matches them exactly. This
// is intentionally lossy relative to the binary codec: an invalid span yields
// an empty traceparent and trace-flags are masked to the W3C-defined bits. Any
// of the three results may be empty.
func (c Carrier) W3C() (tp, ts, bg string) {
	ctx := c.Context(context.Background())
	mc := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, mc)
	propagation.Baggage{}.Inject(ctx, mc)

	return mc["traceparent"], mc["tracestate"], mc["baggage"]
}

// MarshalBinary implements encoding.BinaryMarshaler. The empty carrier
// marshals to nil. It never fails in practice (inputs are valid OTel values).
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

// UnmarshalBinary implements encoding.BinaryUnmarshaler. It decodes data per
// the §6 rules and only writes to the receiver on success. Input is treated as
// hostile: total size and every field length are bounded before allocation, and
// it never panics. The receiver is not modified if an error is returned.
func (c *Carrier) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		*c = Carrier{}
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

	var out Carrier
	if haveCore {
		cfg := trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: flags, Remote: remote}
		if haveTS {
			if ts, err := trace.ParseTraceState(tsStr); err == nil {
				cfg.TraceState = ts // parse failure → tracestate dropped
			}
		}
		if sc := trace.NewSpanContext(cfg); sc.IsValid() {
			out.sc = sc // invalid core → span dropped
		}
	}
	if haveBag {
		// Mirror stock W3C extract: use the parse result iff non-empty
		// (>8192 bytes → empty/dropped; >64 members → truncated/kept).
		if bag, _ := baggage.Parse(bagStr); bag.Len() > 0 {
			out.bag = bag
		}
	}
	*c = out

	return nil
}
