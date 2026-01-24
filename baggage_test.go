package otx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaggageHelpers(t *testing.T) {
	ctx := context.Background()
	var err error

	// Test Baggage
	ctx, err = SetBaggage(ctx, "key", "value")
	require.NoError(t, err)

	val := GetBaggage(ctx, "key")
	assert.Equal(t, "value", val)

	bag := AllBaggage(ctx)
	assert.Equal(t, "value", bag["key"])

	// Test MustSetBaggage
	ctx = MustSetBaggage(ctx, "key2", "value2")
	assert.Equal(t, "value2", GetBaggage(ctx, "key2"))

	ctx = DeleteBaggage(ctx, "key")
	val = GetBaggage(ctx, "key")
	assert.Empty(t, val)
}
