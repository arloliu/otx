package scenario

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuration_AsDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        Duration
		expected time.Duration
	}{
		{"zero", Duration(0), 0},
		{"100ms", Duration(100 * time.Millisecond), 100 * time.Millisecond},
		{"5s", Duration(5 * time.Second), 5 * time.Second},
		{"1m", Duration(time.Minute), time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.d.AsDuration())
		})
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		d        Duration
		expected string
	}{
		{"zero", Duration(0), "0s"},
		{"100ms", Duration(100 * time.Millisecond), "100ms"},
		{"5s", Duration(5 * time.Second), "5s"},
		{"1m30s", Duration(90 * time.Second), "1m30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.d.MarshalYAML()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Duration
		wantErr  bool
	}{
		{"100ms", "100ms", Duration(100 * time.Millisecond), false},
		{"5s", "5s", Duration(5 * time.Second), false},
		{"1m", "1m", Duration(time.Minute), false},
		{"invalid", "not-a-duration", Duration(0), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalYAML(func(v any) error {
				s, ok := v.(*string)
				if !ok {
					return fmt.Errorf("expected *string, got %T", v)
				}
				*s = tt.input

				return nil
			})

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, d)
			}
		})
	}
}

func TestRegistry_EmbeddedScenarios(t *testing.T) {
	// Verify all expected scenarios are registered
	expectedScenarios := []string{"payment", "edge-iot", "ecommerce", "health-check"}

	for _, name := range expectedScenarios {
		t.Run(name, func(t *testing.T) {
			s, ok := Get(name)
			require.True(t, ok, "scenario %q should be registered", name)
			require.NotNil(t, s)
			assert.Equal(t, name, s.Name)
			assert.NotEmpty(t, s.Description)
			assert.NotEmpty(t, s.Services)
			assert.NotEmpty(t, s.RootSpan.Name)
		})
	}
}

func TestGet_NotFound(t *testing.T) {
	s, ok := Get("non-existent-scenario")
	assert.False(t, ok)
	assert.Nil(t, s)
}

func TestList(t *testing.T) {
	names := List()

	// Should have at least the 4 embedded scenarios
	assert.GreaterOrEqual(t, len(names), 4)

	// Check all embedded scenarios are present
	assert.True(t, slices.Contains(names, "payment"))
	assert.True(t, slices.Contains(names, "edge-iot"))
	assert.True(t, slices.Contains(names, "ecommerce"))
	assert.True(t, slices.Contains(names, "health-check"))
}

func TestRegister(t *testing.T) {
	// Register a custom scenario
	customScenario := &Scenario{
		Name:        "test-custom",
		Description: "Test custom scenario",
		Services:    []Service{{Name: "test-service"}},
		RootSpan: SpanTemplate{
			Name:     "test-span",
			Service:  "test-service",
			Kind:     SpanKindServer,
			Duration: Duration(10 * time.Millisecond),
		},
	}

	Register(customScenario)

	// Verify it was registered
	s, ok := Get("test-custom")
	require.True(t, ok)
	assert.Equal(t, customScenario, s)

	// Cleanup
	delete(Registry, "test-custom")
}

func TestHTTPServerAttrs(t *testing.T) {
	attrs := HTTPServerAttrs("POST", "/api/users", "/api/users", 201)

	require.Len(t, attrs, 4)

	// Verify attribute keys are set
	attrMap := make(map[string]any)
	for _, kv := range attrs {
		attrMap[string(kv.Key)] = kv.Value.AsInterface()
	}

	assert.Equal(t, "POST", attrMap["http.request.method"])
	assert.Equal(t, "/api/users", attrMap["http.route"])
	assert.Equal(t, "/api/users", attrMap["url.path"])
	assert.Equal(t, int64(201), attrMap["http.response.status_code"])
}

func TestHTTPClientAttrs(t *testing.T) {
	attrs := HTTPClientAttrs("GET", "https://api.example.com/users", 200)

	require.Len(t, attrs, 3)

	attrMap := make(map[string]any)
	for _, kv := range attrs {
		attrMap[string(kv.Key)] = kv.Value.AsInterface()
	}

	assert.Equal(t, "GET", attrMap["http.request.method"])
	assert.Equal(t, "https://api.example.com/users", attrMap["url.full"])
	assert.Equal(t, int64(200), attrMap["http.response.status_code"])
}

func TestRPCAttrs(t *testing.T) {
	attrs := RPCAttrs("grpc", "UserService", "GetUser")

	require.Len(t, attrs, 3)

	attrMap := make(map[string]any)
	for _, kv := range attrs {
		attrMap[string(kv.Key)] = kv.Value.AsInterface()
	}

	assert.Equal(t, "grpc", attrMap["rpc.system"])
	assert.Equal(t, "UserService", attrMap["rpc.service"])
	assert.Equal(t, "GetUser", attrMap["rpc.method"])
}

func TestDBAttrs(t *testing.T) {
	attrs := DBAttrs("postgresql", "users", "SELECT * FROM users")

	require.Len(t, attrs, 3)

	attrMap := make(map[string]any)
	for _, kv := range attrs {
		attrMap[string(kv.Key)] = kv.Value.AsInterface()
	}

	assert.Equal(t, "postgresql", attrMap["db.system"])
	assert.Equal(t, "users", attrMap["db.namespace"])
	assert.Equal(t, "SELECT * FROM users", attrMap["db.query.text"])
}

func TestMessagingAttrs(t *testing.T) {
	attrs := MessagingAttrs("kafka", "orders", "publish")

	require.Len(t, attrs, 3)

	attrMap := make(map[string]any)
	for _, kv := range attrs {
		attrMap[string(kv.Key)] = kv.Value.AsInterface()
	}

	assert.Equal(t, "kafka", attrMap["messaging.system"])
	assert.Equal(t, "orders", attrMap["messaging.destination.name"])
	assert.Equal(t, "publish", attrMap["messaging.operation.name"])
}

func TestPaymentScenario_Structure(t *testing.T) {
	s := PaymentScenario()

	assert.Equal(t, "payment", s.Name)
	assert.Len(t, s.Services, 6)

	// Verify root span
	assert.Equal(t, "POST /api/v1/checkout", s.RootSpan.Name)
	assert.Equal(t, "payment-gateway", s.RootSpan.Service)
	assert.Equal(t, SpanKindServer, s.RootSpan.Kind)

	// Verify has children
	assert.NotEmpty(t, s.RootSpan.Children)
}

func TestEdgeIoTScenario_Structure(t *testing.T) {
	s := EdgeIoTScenario()

	assert.Equal(t, "edge-iot", s.Name)
	assert.Len(t, s.Services, 4)

	// Verify root span is MQTT consumer
	assert.Equal(t, SpanKindConsumer, s.RootSpan.Kind)
	assert.Contains(t, s.RootSpan.Attributes["messaging.system"], "mqtt")
}

func TestEcommerceScenario_Structure(t *testing.T) {
	s := EcommerceScenario()

	assert.Equal(t, "ecommerce", s.Name)
	assert.Len(t, s.Services, 4)
	assert.Equal(t, "POST /orders", s.RootSpan.Name)
}

func TestHealthCheckScenario_Structure(t *testing.T) {
	s := HealthCheckScenario()

	assert.Equal(t, "health-check", s.Name)
	assert.Len(t, s.Services, 1)

	// Should be a simple scenario with no children
	assert.Empty(t, s.RootSpan.Children)
	assert.Equal(t, "GET /health", s.RootSpan.Name)
}
