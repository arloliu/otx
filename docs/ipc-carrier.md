# IPC OTel Carrier

The `otx/carrier` package carries OpenTelemetry propagation state — span context
(TraceID, SpanID, TraceFlags, TraceState, remote flag) **plus** baggage — across
**in-process IPC boundaries** as a compact, self-contained `[]byte`.

W3C text propagation (`InjectGRPC` / `ExtractGRPC`) is the right model for network
RPC between services. `Carrier` is additive and targets a different scenario: when
you want to carry the OTel state as an **opaque blob inside your own message** — a
`bytes` protobuf field, a pipe frame, a shared-memory segment, an on-disk queue —
rather than only as transport headers. A typical case is a host process handing
its propagation state to plugin processes over a UDS-based transport.

## Overview

`Carrier` is a small immutable value that holds OTel's own already-parsed types,
so in-process reads cost nothing to decode. It serializes only at the boundary:

- **Construction & context bridges** — `New`, `FromContext`, `Context`, `ParseW3C`.
- **Accessors** — `SpanContext`, `Baggage`, `IsEmpty`.
- **Binary codec** — `MarshalBinary`, `AppendBinary`, `UnmarshalBinary`, plus the
  `HasOTel` hot-path skip filter. It implements `encoding.BinaryMarshaler`,
  `encoding.BinaryAppender`, and `encoding.BinaryUnmarshaler`.
- **W3C text interop** — `W3C` and `ParseW3C`, delegating to the stock OTel
  propagators for debugging or bridging to the text path.

Key properties:

- **Compact, versioned, forward-compatible wire format.** Producer and consumer
  may be built against different otx versions; a newer producer that adds a field
  never breaks an older consumer's decode of the fields it understands.
- **Safe on untrusted input.** `UnmarshalBinary` and `HasOTel` never panic for any
  input. Total size is bounded by `MaxBytes` before any allocation, and the
  receiver is left unchanged when decoding fails.
- **Cheap to skip when empty.** A context with no OTel state is detected without
  allocation on the producing side and without parsing on the consuming side. The
  empty carrier marshals to `nil`.

## Quick Start

### Producer — embed the OTel state in your message

```go
import "github.com/arloliu/otx/carrier"

func send(ctx context.Context, msg *MyMessage) error {
    // ok is false when ctx carries no valid span and no baggage —
    // the hot-path guard: nothing to serialize, nothing to send.
    if c, ok := carrier.FromContext(ctx); ok {
        // MarshalBinary's error is always nil in practice (inputs are valid
        // OTel values); the empty carrier would marshal to nil.
        blob, _ := c.MarshalBinary()
        msg.Trace = blob // your own bytes field
    }
    return transport.Send(msg)
}
```

### Consumer — restore the trace and baggage

```go
func receive(ctx context.Context, msg *MyMessage) error {
    // HasOTel is a cheap pre-filter: no OTel bytes → skip the decode entirely.
    if carrier.HasOTel(msg.Trace) {
        var c carrier.Carrier
        if err := c.UnmarshalBinary(msg.Trace); err != nil {
            // Malformed or unsupported version: proceed without trace context
            // rather than failing the message. The receiver is unchanged.
            log.Warn("dropping trace carrier", "err", err)
        } else {
            // Continue the trace (span context installed as remote) and baggage.
            ctx = c.Context(ctx)
        }
    }

    ctx, span := otx.StartConsumer(ctx, "handle.message")
    defer span.End()
    return handle(ctx, msg)
}
```

`HasOTel` is a skip filter, not a validator: it may return `true` for malformed
bytes, which `UnmarshalBinary` then rejects. It returns `false` only for `nil`,
empty, version-only, or wrong-version input.

## API Reference

### Construction and context bridges

| Function | Description |
|----------|-------------|
| `New(sc trace.SpanContext, bag baggage.Baggage) Carrier` | Build a carrier from explicit OTel values. An invalid span context (either ID zero) is treated as absent by every later operation. |
| `FromContext(ctx context.Context) (Carrier, bool)` | Extract propagation state from `ctx`. `ok` is `false` when `ctx` carries neither a valid span context nor any baggage — the zero-allocation hot-path guard. |
| `(c Carrier) Context(ctx context.Context) context.Context` | Return a child of `ctx` carrying the carrier's span context (installed as a **remote** span context, matching OTel extract semantics) and baggage. An empty carrier returns `ctx` unchanged. |
| `ParseW3C(traceparent, tracestate, bg string) (Carrier, error)` | Build a carrier from W3C text forms via the stock OTel extractors. Empty strings yield absent components. Returns `ErrMalformed` only when a non-empty `traceparent` fails to extract a valid span context. |

### Accessors

| Method | Description |
|--------|-------------|
| `(c Carrier) SpanContext() trace.SpanContext` | The carrier's span context (may be invalid/absent). |
| `(c Carrier) Baggage() baggage.Baggage` | The carrier's baggage. |
| `(c Carrier) IsEmpty() bool` | Reports whether the carrier holds no valid span context and no baggage. |

### Binary codec

| Member | Description |
|--------|-------------|
| `(c Carrier) MarshalBinary() ([]byte, error)` | Encode to the compact binary form. The empty carrier marshals to `nil`. Never fails in practice. |
| `(c Carrier) AppendBinary(b []byte) ([]byte, error)` | Append the encoding to `b` with no per-call allocation when `b` has spare capacity — reuse a buffer on the hot path. |
| `(c *Carrier) UnmarshalBinary(data []byte) error` | Decode hostile input. `nil`, empty, and version-only input yield the empty carrier with no error. On any error the receiver is left **unchanged**. |
| `HasOTel(b []byte) bool` | Cheap, allocation-free pre-filter: reports whether `b` could contain OTel info (length > 1 and a recognized version byte). A skip filter, not a validator. |
| `MaxBytes` | Upper bound on `UnmarshalBinary` input, checked first in O(1). Set above the largest carrier a conformant producer can emit, so it never rejects a valid carrier — it only stops oversized/hostile input. |

### W3C text interop

| Member | Description |
|--------|-------------|
| `(c Carrier) W3C() (tp, ts, bg string)` | Return the W3C `traceparent` / `tracestate` / `baggage` strings via the stock propagators. **Intentionally lossy** relative to the binary codec: an invalid span yields an empty `traceparent`, and trace-flags are masked to the W3C-defined bits. Any result may be empty. |

### Sentinel errors

| Error | Meaning |
|-------|---------|
| `ErrMalformed` | Binary input is structurally invalid — bad framing, length overrun, duplicate tag, wrong core length, or oversized input. |
| `ErrUnsupportedVersion` | The format version byte is unrecognized (a breaking change from a newer producer), distinct from a forward-compatible unknown tag. |

## Semantics and Caveats

- **Binary is the source of truth; W3C is lossy.** A full binary round-trip
  (`UnmarshalBinary(MarshalBinary(c))`) preserves the whole TraceFlags byte, the
  remote bit, the full TraceState, and every baggage member including properties.
  `W3C()` is for debugging or bridging to the text path and discards what the W3C
  model cannot represent (e.g. private flag bits, an invalid span).

- **Remote span semantics.** `Context` installs the span context as a *remote*
  parent — the same thing a stock W3C extractor does — so downstream spans are
  parented correctly across the process boundary.

- **Over-limit baggage degrades like the W3C text path.** Baggage that exceeds the
  W3C limits (constructible only by bypassing `baggage.Parse`) decodes to OTel's
  own partial-parse result — empty (dropped) for more than 8192 bytes, the
  truncated first-64-member subset for more than 64 members — exactly what the
  stock `propagation.Baggage` extractor would install. The span context and
  tracestate always survive. The binary path is never weaker than the text path.

- **Decode failures are non-fatal to your message flow.** When `UnmarshalBinary`
  returns an error, treat it as "no trace context" and continue — the bytes were
  produced by another process and are untrusted. The receiver value you decoded
  into is unchanged, so a reused `Carrier` keeps its prior state on error.

- **Concurrency.** A `Carrier` is an immutable value; copying it is cheap and safe
  for concurrent reads. Only `UnmarshalBinary` mutates its (pointer) receiver.

## See Also

- [HTTP/gRPC Integration](http-grpc-integration.md) — W3C text propagation across
  network RPC.
- [NATS Integration](nats-integration.md) — trace context injection/extraction for
  JetStream messaging.
