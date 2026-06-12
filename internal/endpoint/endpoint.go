// Package endpoint classifies OTLP endpoint strings and provides the single
// shared helper used by config validation, exporter.go, and zaplog/core.go.
//
// Valid forms:
//   - bare host:port   (e.g. "localhost:4318", "collector", "[::1]:4318")
//   - http:// URL      (scheme overrides Insecure)
//   - https:// URL     (scheme overrides Insecure)
//
// Any other scheme (grpc://, tcp://, unix://, …) is rejected with an actionable error.
package endpoint

import (
	"fmt"
	"strings"
)

// Kind classifies an endpoint string.
type Kind int

const (
	// KindBare is a bare host:port (no scheme).
	KindBare Kind = iota
	// KindHTTP is an http:// URL.
	KindHTTP
	// KindHTTPS is an https:// URL.
	KindHTTPS
)

// Classify returns the Kind of the endpoint string and an error if the scheme
// is recognised but invalid (e.g. "grpc://", "tcp://", "unix://").
//
// Empty strings return KindBare, nil — callers treat them as "use default".
//
// The rule is simple and avoids url.Parse's colon trap (url.Parse("localhost:4317")
// yields Scheme="localhost"): a string is an http(s) URL if and only if it
// starts with "http://" or "https://". Anything else that contains "://" is a
// rejected scheme. Everything else is bare.
func Classify(raw string) (Kind, error) {
	if raw == "" {
		return KindBare, nil
	}

	lower := strings.ToLower(raw)

	switch {
	case strings.HasPrefix(lower, "http://"):
		return KindHTTP, nil
	case strings.HasPrefix(lower, "https://"):
		return KindHTTPS, nil
	}

	// Detect any other "://" pattern and reject it.
	if idx := strings.Index(raw, "://"); idx >= 0 {
		scheme := raw[:idx]
		return KindBare, fmt.Errorf(
			"invalid endpoint scheme %q: transport is selected by protocol, not the endpoint scheme; "+
				"use host:port or an http(s):// URL",
			scheme,
		)
	}

	// No "://" means bare host:port (covers "localhost:4318", "collector",
	// "[::1]:4318", and any host-only form).
	return KindBare, nil
}

// IsHTTP reports whether the endpoint string carries an http or https scheme.
// It is safe to call when the endpoint has already been validated by Classify.
// Returns false for bare endpoints and empty strings.
func IsHTTP(raw string) bool {
	k, err := Classify(raw)
	return err == nil && (k == KindHTTP || k == KindHTTPS)
}
