// Package insecurewarn emits a one-time diagnostic when telemetry would be
// exported as plaintext (insecure, no https:// scheme) to a non-loopback host.
//
// It is the shared implementation behind the v1.1.0 trace/metric/log exporter
// hardening (exporter.go) and the zaplog OTLP core. The classification and the
// diagnostic message live here so both call sites stay byte-identical; the
// per-process dedup that backs Warn is shared, so a given remote endpoint warns
// at most once per process across every signal. The package is internal-only;
// it adds no public API surface to otx.
package insecurewarn

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/arloliu/otx/internal/endpoint"
	"go.opentelemetry.io/otel"
)

// warned dedups the plaintext-to-remote diagnostic so each distinct remote
// endpoint is reported at most once per process (the same endpoint is resolved
// repeatedly across signals and provider re-creation). It backs Warn; the
// exporter path keeps its own equivalent dedup set (see exporter.go).
var (
	warnMu sync.Mutex
	warned = map[string]struct{}{}
)

// ShouldWarn reports whether the endpoint would export plaintext (insecure, no
// https:// scheme) to a non-loopback host. An https:// scheme forces TLS
// regardless of the insecure flag, so URL endpoints never warn; loopback
// endpoints (localhost, 127.0.0.0/8, ::1) are the safe default and never warn.
// It performs no dedup — callers own their one-time gate.
func ShouldWarn(ep string, insecure bool) bool {
	if !insecure {
		return false
	}
	// https:// always forces TLS, so it is never a plaintext leak.
	if k, err := endpoint.Classify(ep); err == nil && k == endpoint.KindHTTPS {
		return false
	}

	return !IsLoopback(ep)
}

// Emit reports the plaintext-to-remote diagnostic through otel.Handle. It does
// not dedup; callers gate it behind ShouldWarn and their own one-time set.
func Emit(ep string) {
	otel.Handle(fmt.Errorf(
		"otx: OTLP exporter is configured insecure (plaintext) for non-loopback host %q; "+
			"telemetry and any credential-bearing headers are sent without TLS. Set Insecure=false "+
			"or use an https:// endpoint for remote collectors",
		ep,
	))
}

// Warn emits the one-time otel.Handle diagnostic when ShouldWarn is true,
// deduping on a shared per-process set so a given endpoint warns at most once
// regardless of which call site resolves it first. It is the zaplog entry point;
// the exporter path keeps a behavior-identical local gate for test isolation.
func Warn(ep string, insecure bool) {
	if !ShouldWarn(ep, insecure) {
		return
	}

	warnMu.Lock()
	_, seen := warned[ep]
	if !seen {
		warned[ep] = struct{}{}
	}
	warnMu.Unlock()
	if seen {
		return
	}

	Emit(ep)
}

// Reset clears the shared dedup set that backs Warn. It exists for tests that
// assert the one-time warning fires for a fresh endpoint; production code never
// calls it.
func Reset() {
	warnMu.Lock()
	warned = map[string]struct{}{}
	warnMu.Unlock()
}

// IsLoopback reports whether the endpoint's host resolves to a loopback address
// (localhost, 127.0.0.0/8, ::1). An empty endpoint is treated as loopback
// because the protocol default is always localhost.
func IsLoopback(raw string) bool {
	if raw == "" {
		return true
	}

	host := raw
	if k, err := endpoint.Classify(raw); err == nil && (k == endpoint.KindHTTP || k == endpoint.KindHTTPS) {
		// Strip the scheme then fall through to host:port handling below.
		if idx := strings.Index(host, "://"); idx >= 0 {
			host = host[idx+len("://"):]
		}
	}

	// Strip a trailing path on URL forms (e.g. host:port/v1/traces).
	if idx := strings.IndexByte(host, '/'); idx >= 0 {
		host = host[:idx]
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")

	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}

	return false
}
