// Package scenario defines interfaces and embedded scenarios for the OTLP simulator.
package scenario

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Scenario defines a complete trace/log simulation scenario.
type Scenario struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Services    []Service    `yaml:"services"`
	RootSpan    SpanTemplate `yaml:"rootSpan"`
}

// Service represents a microservice in the scenario.
type Service struct {
	Name       string            `yaml:"name"`
	Attributes map[string]string `yaml:"attributes,omitempty"`
}

// SpanTemplate defines a span and its children.
type SpanTemplate struct {
	Name       string            `yaml:"name"`
	Service    string            `yaml:"service"`
	Kind       SpanKind          `yaml:"kind"`
	Duration   Duration          `yaml:"duration"`
	Attributes map[string]string `yaml:"attributes,omitempty"`
	Children   []SpanTemplate    `yaml:"children,omitempty"`
	Logs       []LogTemplate     `yaml:"logs,omitempty"`

	// Error simulation
	ErrorRate   float64 `yaml:"errorRate,omitempty"`   // 0.0-1.0
	ErrorStatus string  `yaml:"errorStatus,omitempty"` // Error message when triggered
}

// LogTemplate defines a log entry within a span.
type LogTemplate struct {
	Level      string            `yaml:"level"` // INFO, WARN, ERROR, DEBUG
	Message    string            `yaml:"message"`
	Attributes map[string]string `yaml:"attributes,omitempty"`
	Delay      Duration          `yaml:"delay,omitempty"` // Delay after span start
}

// SpanKind represents the type of span.
type SpanKind string

const (
	SpanKindServer   SpanKind = "SERVER"
	SpanKindClient   SpanKind = "CLIENT"
	SpanKindProducer SpanKind = "PRODUCER"
	SpanKindConsumer SpanKind = "CONSUMER"
	SpanKindInternal SpanKind = "INTERNAL"
)

// Duration is a wrapper for time.Duration that supports YAML parsing.
type Duration time.Duration

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)

	return nil
}

// AsDuration converts Duration to time.Duration.
func (d Duration) AsDuration() time.Duration {
	return time.Duration(d)
}

// Registry holds all available scenarios.
var Registry = map[string]*Scenario{}

func init() {
	// Register embedded scenarios
	Register(PaymentScenario())
	Register(EdgeIoTScenario())
	Register(EcommerceScenario())
	Register(HealthCheckScenario())
}

// Register adds a scenario to the registry.
func Register(s *Scenario) {
	Registry[s.Name] = s
}

// Get retrieves a scenario by name.
func Get(name string) (*Scenario, bool) {
	s, ok := Registry[name]
	return s, ok
}

// List returns all available scenario names.
func List() []string {
	names := make([]string, 0, len(Registry))
	for name := range Registry {
		names = append(names, name)
	}

	return names
}

// HTTPServerAttrs returns semantic convention attributes for an HTTP server span.
func HTTPServerAttrs(method, route, target string, statusCode int) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(method),
		semconv.HTTPRouteKey.String(route),
		semconv.URLPathKey.String(target),
		semconv.HTTPResponseStatusCodeKey.Int(statusCode),
	}
}

// HTTPClientAttrs returns semantic convention attributes for an HTTP client span.
func HTTPClientAttrs(method, url string, statusCode int) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(method),
		semconv.URLFullKey.String(url),
		semconv.HTTPResponseStatusCodeKey.Int(statusCode),
	}
}

// RPCAttrs returns semantic convention attributes for an RPC span.
func RPCAttrs(system, service, method string) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.RPCSystemKey.String(system),
		semconv.RPCServiceKey.String(service),
		semconv.RPCMethodKey.String(method),
	}
}

// DBAttrs returns semantic convention attributes for a database span.
func DBAttrs(system, name, statement string) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.DBSystemKey.String(system),
		semconv.DBNamespaceKey.String(name),
		semconv.DBQueryTextKey.String(statement),
	}
}

// MessagingAttrs returns semantic convention attributes for a messaging span.
func MessagingAttrs(system, destination, operation string) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.MessagingSystemKey.String(system),
		semconv.MessagingDestinationNameKey.String(destination),
		semconv.MessagingOperationNameKey.String(operation),
	}
}
