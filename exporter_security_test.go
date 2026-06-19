package otx

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetInsecureWarn clears the per-endpoint dedup set so each case starts fresh.
func resetInsecureWarn(t *testing.T) {
	t.Helper()
	insecureWarnMu.Lock()
	insecureWarned = map[string]struct{}{}
	insecureWarnMu.Unlock()
	t.Cleanup(func() {
		insecureWarnMu.Lock()
		insecureWarned = map[string]struct{}{}
		insecureWarnMu.Unlock()
	})
}

func TestWarnInsecureRemote(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		insecure bool
		wantWarn bool
	}{
		{
			name:     "remote insecure host:port warns",
			endpoint: "collector.example.com:4317", insecure: true, wantWarn: true,
		},
		{
			name:     "remote insecure http URL warns",
			endpoint: "http://collector.example.com:4318", insecure: true, wantWarn: true,
		},
		{name: "remote insecure ip warns", endpoint: "10.0.0.5:4317", insecure: true, wantWarn: true},
		{name: "localhost host:port no warn", endpoint: "localhost:4317", insecure: true, wantWarn: false},
		{name: "loopback ip no warn", endpoint: "127.0.0.1:4317", insecure: true, wantWarn: false},
		{name: "ipv6 loopback no warn", endpoint: "[::1]:4317", insecure: true, wantWarn: false},
		{name: "empty endpoint no warn", endpoint: "", insecure: true, wantWarn: false},
		{
			name:     "remote https URL no warn",
			endpoint: "https://collector.example.com:4318", insecure: true, wantWarn: false,
		},
		{
			name:     "remote but secure no warn",
			endpoint: "collector.example.com:4317", insecure: false, wantWarn: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resetInsecureWarn(t)
			sink := captureOTelErrors(t)

			warnInsecureRemote(exporterParams{Endpoint: tt.endpoint, Insecure: tt.insecure})

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

func TestWarnInsecureRemote_DedupsPerEndpoint(t *testing.T) {
	resetInsecureWarn(t)
	sink := captureOTelErrors(t)

	params := exporterParams{Endpoint: "collector.example.com:4317", Insecure: true}
	warnInsecureRemote(params)
	warnInsecureRemote(params)
	warnInsecureRemote(params)

	assert.Len(t, sink.messages(), 1, "same endpoint must warn at most once")
}

func TestWarnInsecureRemote_ConcurrentSafe(t *testing.T) {
	resetInsecureWarn(t)
	sink := captureOTelErrors(t)

	params := exporterParams{Endpoint: "collector.example.com:4317", Insecure: true}

	const goroutines = 32
	start := make(chan struct{})
	done := make(chan struct{}, goroutines)
	for range goroutines {
		go func() {
			<-start
			warnInsecureRemote(params)
			done <- struct{}{}
		}()
	}
	close(start)
	for range goroutines {
		<-done
	}

	// Even under concurrent first-calls the endpoint warns exactly once and the
	// shared dedup map is race-free (run with -race).
	assert.Len(t, sink.messages(), 1)
}

func TestBuildTraceExporter_WarnsForRemoteInsecure(t *testing.T) {
	resetInsecureWarn(t)
	sink := captureOTelErrors(t)

	cfg := &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "svc",
		Traces:      &TracesConfig{Exporter: "none"},
		OTLP:        &OTLPConfig{Endpoint: "collector.example.com:4317", Protocol: "grpc"},
	}

	_, err := buildTraceExporter(context.Background(), cfg)
	require.NoError(t, err)

	msgs := sink.messages()
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "plaintext")
}

func TestBuildExporter_UnknownType(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:     BoolPtr(true),
		ServiceName: "svc",
		Traces:      &TracesConfig{Exporter: "bogus"},
		Logs:        &LogsConfig{Exporter: "bogus"},
		Metrics:     &MetricsConfig{Exporter: "bogus"},
	}

	_, err := buildTraceExporter(context.Background(), cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownExporter)
	assert.Contains(t, err.Error(), "bogus")

	_, err = buildLogExporter(context.Background(), cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownExporter)

	_, err = buildMetricExporter(context.Background(), cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownExporter)
}

func TestBuildExporter_KnownTypesStillBuild(t *testing.T) {
	// nop/none and console must NOT hit the unknown-type branch.
	for _, typ := range []string{"none", "nop", "noop", "console", "stdout"} {
		cfg := &TelemetryConfig{
			Enabled:     BoolPtr(true),
			ServiceName: "svc",
			Traces:      &TracesConfig{Exporter: typ},
		}
		_, err := buildTraceExporter(context.Background(), cfg)
		assert.False(t, errors.Is(err, ErrUnknownExporter), "type %q must be recognized", typ)
	}
}
