// Package http provides OpenTelemetry instrumentation for HTTP clients and servers.
//
// # HTTP Server
//
// Use middleware to instrument HTTP handlers:
//
//	// Standard net/http
//	http.Handle("/api", otxhttp.Middleware("api-server")(myHandler))
//
//	// Explicit verification of providers
//	http.Handle("/api", otxhttp.MiddlewareWithProviders(tp, mp, prop)(myHandler))
//
// # HTTP Client
//
// Create an instrumented HTTP client:
//
//	client := otxhttp.NewClient(
//	    otxhttp.WithTimeout(30 * time.Second),
//	)
//
//	// Use the client
//	resp, err := client.Get("https://example.com")
package http
