package otx

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// captureOTelErrors swaps the global OTel error handler for the duration of fn
// and returns every error otel.Handle received. It restores the previous handler
// via t.Cleanup so cases do not leak into one another.
func captureOTelErrors(t *testing.T) *errorSink {
	t.Helper()

	sink := &errorSink{}
	prev := otelErrorHandler()
	otel.SetErrorHandler(sink)
	t.Cleanup(func() { otel.SetErrorHandler(prev) })

	return sink
}

// otelErrorHandler returns the currently installed global error handler so it can
// be restored after a test.
func otelErrorHandler() otel.ErrorHandler {
	return otel.GetErrorHandler()
}

type errorSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errorSink) Handle(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *errorSink) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.errs))
	for i, e := range s.errs {
		out[i] = e.Error()
	}

	return out
}

func TestBuildPropagator_UnimplementedContribPropagators(t *testing.T) {
	cases := []struct {
		name        string
		propagators string
		wantPkg     string
	}{
		{name: "b3", propagators: "b3", wantPkg: "contrib/propagators/b3"},
		{name: "b3multi", propagators: "b3multi", wantPkg: "contrib/propagators/b3"},
		{name: "jaeger", propagators: "jaeger", wantPkg: "contrib/propagators/jaeger"},
		{name: "xray", propagators: "xray", wantPkg: "contrib/propagators/aws/xray"},
		{name: "ottrace", propagators: "ottrace", wantPkg: "contrib/propagators/ot"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sink := captureOTelErrors(t)

			prop := BuildPropagator(&PropConfig{Propagators: tt.propagators})

			// Configuring only an unimplemented propagator yields an empty
			// composite (no fields), proving nothing was installed.
			assert.Empty(t, prop.Fields(), "expected empty composite for %s", tt.name)

			msgs := sink.messages()
			require.Len(t, msgs, 1, "expected exactly one diagnostic")
			assert.Contains(t, msgs[0], tt.propagators)
			assert.Contains(t, msgs[0], tt.wantPkg)
			assert.Contains(t, msgs[0], "not installed")
		})
	}
}

func TestBuildPropagator_UnknownPropagatorStillWarns(t *testing.T) {
	sink := captureOTelErrors(t)

	prop := BuildPropagator(&PropConfig{Propagators: "bogus"})
	assert.Empty(t, prop.Fields())

	msgs := sink.messages()
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "unknown propagator")
	assert.Contains(t, msgs[0], "bogus")
}

func TestBuildPropagator_DefaultComposite(t *testing.T) {
	sink := captureOTelErrors(t)

	// Nil config and explicit default both yield tracecontext + baggage and no
	// diagnostics.
	for _, cfg := range []*PropConfig{nil, {Propagators: "tracecontext,baggage"}} {
		prop := BuildPropagator(cfg)

		fields := prop.Fields()
		// W3C tracecontext contributes "traceparent"/"tracestate"; baggage
		// contributes "baggage".
		assert.Contains(t, fields, "traceparent")
		assert.Contains(t, fields, "baggage")
	}

	assert.Empty(t, sink.messages(), "default config must not emit diagnostics")
}

func TestBuildPropagator_TraceContextOnly(t *testing.T) {
	prop := BuildPropagator(&PropConfig{Propagators: "tracecontext"})
	assert.Contains(t, prop.Fields(), "traceparent")
	assert.NotContains(t, prop.Fields(), "baggage")
}

// Compile-time assertion that BuildPropagator returns the documented type.
var _ propagation.TextMapPropagator = BuildPropagator(nil)
