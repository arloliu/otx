package otx

// SpanNamer defines how operation names are transformed into span names.
type SpanNamer interface {
	Name(operation string) string
}

// DefaultNamer returns operation names unchanged.
// This complies with OpenTelemetry semantic conventions which recommend
// using the raw operation name without service prefixes.
type DefaultNamer struct{}

// Name returns the operation name as is.
func (DefaultNamer) Name(operation string) string {
	return operation
}

// NameHTTP returns a compliant span name for an HTTP request: "METHOD /route".
// Example: "GET /users/{id}"
func NameHTTP(method, route string) string {
	return method + " " + route
}

// NameRPC returns a compliant span name for an RPC call: "Service/Method".
// Example: "Greeter/SayHello"
func NameRPC(service, method string) string {
	return service + "/" + method
}

// NameMessaging returns a compliant span name for a messaging operation: "verb destination".
// Example: "publish orders"
func NameMessaging(verb, destination string) string {
	return verb + " " + destination
}

// NameDB returns a compliant span name for a database operation: "verb table".
// Example: "SELECT users"
func NameDB(verb, table string) string {
	return verb + " " + table
}
