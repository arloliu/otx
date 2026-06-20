package http

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type stubRoundTripper struct{}

func (stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("stub")
}

// L10 — when the base is not an *http.Transport, buildTransport must return it
// verbatim (transport-pool/timeout options cannot apply to an opaque
// RoundTripper and are silently ignored).
func TestBuildTransport_OpaqueRoundTripperReturnedVerbatim(t *testing.T) {
	stub := stubRoundTripper{}
	cfg := &clientConfig{
		baseTransport:       stub,
		dialTimeout:         time.Second, // a transport-level option that must be ignored
		maxIdleConns:        99,
		tlsHandshakeTimeout: time.Second,
	}

	got := buildTransport(cfg)
	gotStub, ok := got.(stubRoundTripper)
	assert.True(t, ok, "opaque RoundTripper must be returned unchanged, got %T", got)
	assert.Equal(t, stub, gotStub)
}
