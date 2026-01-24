package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNamerHelpers(t *testing.T) {
	// DefaultNamer conforms to OTel spec (pass-through)
	assert.Equal(t, "operation", DefaultNamer{}.Name("operation"))

	// HTTP helper
	assert.Equal(t, "GET /users/{id}", NameHTTP("GET", "/users/{id}"))

	// RPC helper
	assert.Equal(t, "Greeter/SayHello", NameRPC("Greeter", "SayHello"))

	// Messaging helper
	assert.Equal(t, "publish orders", NameMessaging("publish", "orders"))

	// DB helper
	assert.Equal(t, "SELECT users", NameDB("SELECT", "users"))
}
