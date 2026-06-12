package endpoint_test

import (
	"testing"

	"github.com/arloliu/otx/internal/endpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    endpoint.Kind
		wantErr bool
		errFrag string // substring that must appear in the error message
	}{
		// --- valid bare host:port forms ---
		{name: "bare localhost:port", raw: "localhost:4318", want: endpoint.KindBare},
		{name: "bare hostname no port", raw: "collector", want: endpoint.KindBare},
		{name: "bare IPv6", raw: "[::1]:4318", want: endpoint.KindBare},
		{name: "bare IP:port", raw: "192.168.1.1:4318", want: endpoint.KindBare},

		// --- valid http/https URL forms ---
		{name: "http URL", raw: "http://h:4318", want: endpoint.KindHTTP},
		{name: "https URL", raw: "https://h:4318", want: endpoint.KindHTTPS},
		{name: "http URL with path", raw: "http://h:4318/custom/path", want: endpoint.KindHTTP},
		{name: "https URL with path", raw: "https://h:4318/v1/traces", want: endpoint.KindHTTPS},
		{name: "http URL with query", raw: "http://h:4318/path?x=1", want: endpoint.KindHTTP},
		{name: "HTTP uppercase scheme", raw: "HTTP://h:4318", want: endpoint.KindHTTP},
		{name: "HTTPS uppercase scheme", raw: "HTTPS://h:4318", want: endpoint.KindHTTPS},
		{name: "HttP mixed-case scheme", raw: "HttP://h:4318", want: endpoint.KindHTTP},
		{name: "HttpS mixed-case scheme", raw: "HttpS://h:4318", want: endpoint.KindHTTPS},

		// --- empty: always valid, bare ---
		{name: "empty", raw: "", want: endpoint.KindBare},

		// --- invalid schemes ---
		{
			name:    "grpc scheme",
			raw:     "grpc://h:4317",
			wantErr: true,
			errFrag: `invalid endpoint scheme "grpc"`,
		},
		{
			name:    "tcp scheme",
			raw:     "tcp://h:1",
			wantErr: true,
			errFrag: `invalid endpoint scheme "tcp"`,
		},
		{
			name:    "unix scheme",
			raw:     "unix:///x.sock",
			wantErr: true,
			errFrag: `invalid endpoint scheme "unix"`,
		},
		{
			name:    "malformed ://bad",
			raw:     "://bad",
			wantErr: true,
			errFrag: `invalid endpoint scheme ""`,
		},
		{
			// Detection is case-insensitive; the error preserves the original
			// scheme casing.
			name:    "mixed-case GRPC scheme",
			raw:     "GRPC://h:4317",
			wantErr: true,
			errFrag: `invalid endpoint scheme "GRPC"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := endpoint.Classify(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errFrag)
				// actionable message always present
				assert.Contains(t, err.Error(), "use host:port or an http(s):// URL")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestIsHTTP(t *testing.T) {
	assert.True(t, endpoint.IsHTTP("http://h:4318"))
	assert.True(t, endpoint.IsHTTP("https://h:4318"))
	assert.False(t, endpoint.IsHTTP("h:4318"))
	assert.False(t, endpoint.IsHTTP("collector"))
	assert.False(t, endpoint.IsHTTP(""))
	// invalid scheme returns false (no panic)
	assert.False(t, endpoint.IsHTTP("grpc://h:4317"))
}
