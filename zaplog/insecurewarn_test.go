package zaplog

import (
	"context"
	"sync"
	"testing"

	"github.com/arloliu/otx"
	"github.com/arloliu/otx/internal/insecurewarn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zapcore"
)

// otelErrSink captures every error otel.Handle receives for the duration of a
// test, restoring the previous handler via t.Cleanup so cases stay isolated.
type otelErrSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *otelErrSink) Handle(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *otelErrSink) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.errs))
	for i, e := range s.errs {
		out[i] = e.Error()
	}

	return out
}

// captureInsecureWarn installs an otel error sink and resets the shared dedup
// set so each case observes a fresh one-time warning decision.
func captureInsecureWarn(t *testing.T) *otelErrSink {
	t.Helper()
	insecurewarn.Reset()
	t.Cleanup(insecurewarn.Reset)

	sink := &otelErrSink{}
	prev := otel.GetErrorHandler()
	otel.SetErrorHandler(sink)
	t.Cleanup(func() { otel.SetErrorHandler(prev) })

	return sink
}

// warnCfg builds an enabled grpc config pointing logs at endpoint with the given
// insecure mode. grpc is used so NewCore constructs without dialing a real host.
func warnCfg(endpoint string, insecure bool) *otx.TelemetryConfig {
	return &otx.TelemetryConfig{
		Enabled:     boolPtr(true),
		ServiceName: "svc",
		OTLP: &otx.OTLPConfig{
			Endpoint: endpoint,
			Protocol: "grpc",
			Insecure: boolPtr(insecure),
		},
		Logs: &otx.LogsConfig{Enabled: boolPtr(true)},
	}
}

// TestNewCore_WarnsOnInsecureRemote pins the ROB-1 hardening: a zaplog OTLP core
// pointed at a non-loopback host with insecure (plaintext) transport emits the
// same one-time otel.Handle diagnostic the SDK trace/metric/log exporters do,
// and stays silent for loopback hosts and https:// endpoints.
func TestNewCore_WarnsOnInsecureRemote(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		insecure bool
		wantWarn bool
	}{
		{
			name: "remote insecure bare host warns", endpoint: "collector.example.com:4317",
			insecure: true, wantWarn: true,
		},
		{
			name: "remote insecure http URL warns", endpoint: "http://collector.example.com:4317",
			insecure: true, wantWarn: true,
		},
		{name: "remote insecure ip warns", endpoint: "10.0.0.5:4317", insecure: true, wantWarn: true},
		{name: "localhost no warn", endpoint: "localhost:4317", insecure: true, wantWarn: false},
		{name: "loopback ip no warn", endpoint: "127.0.0.1:4317", insecure: true, wantWarn: false},
		{name: "ipv6 loopback no warn", endpoint: "[::1]:4317", insecure: true, wantWarn: false},
		{
			name: "remote https URL no warn", endpoint: "https://collector.example.com:4317",
			insecure: true, wantWarn: false,
		},
		{name: "remote secure no warn", endpoint: "collector.example.com:4317", insecure: false, wantWarn: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sink := captureInsecureWarn(t)

			core, w, err := NewCore(context.Background(), warnCfg(tt.endpoint, tt.insecure), zapcore.InfoLevel)
			require.NoError(t, err, "construction must not dial; remote endpoints still build")
			t.Cleanup(func() { _ = w.Close() })
			require.NotNil(t, core)

			msgs := sink.messages()
			if tt.wantWarn {
				require.Len(t, msgs, 1)
				assert.Contains(t, msgs[0], "plaintext")
				assert.Contains(t, msgs[0], tt.endpoint)
			} else {
				assert.Empty(t, msgs)
			}
		})
	}
}

// TestNewCore_InsecureRemoteWarnsOnce confirms the diagnostic dedups: rebuilding
// a core for the same insecure remote endpoint warns at most once per process.
func TestNewCore_InsecureRemoteWarnsOnce(t *testing.T) {
	sink := captureInsecureWarn(t)

	for range 3 {
		core, w, err := NewCore(context.Background(), warnCfg("collector.example.com:4317", true), zapcore.InfoLevel)
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Close() })
		require.NotNil(t, core)
	}

	assert.Len(t, sink.messages(), 1, "same endpoint must warn at most once")
}
