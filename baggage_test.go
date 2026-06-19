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

func TestAllBaggage(t *testing.T) {
	t.Run("empty context", func(t *testing.T) {
		bag := AllBaggage(context.Background())
		assert.NotNil(t, bag)
		assert.Empty(t, bag)
	})

	t.Run("multiple members", func(t *testing.T) {
		ctx := context.Background()
		ctx = MustSetBaggage(ctx, "alpha", "1")
		ctx = MustSetBaggage(ctx, "beta", "2")

		bag := AllBaggage(ctx)
		assert.Equal(t, map[string]string{"alpha": "1", "beta": "2"}, bag)
	})
}

func TestLookupBaggage(t *testing.T) {
	ctx := context.Background()
	ctx = MustSetBaggage(ctx, "present", "value")
	ctx = MustSetBaggage(ctx, "empty", "")

	t.Run("present with value", func(t *testing.T) {
		val, ok := LookupBaggage(ctx, "present")
		assert.True(t, ok)
		assert.Equal(t, "value", val)
	})

	t.Run("present with empty value", func(t *testing.T) {
		val, ok := LookupBaggage(ctx, "empty")
		assert.True(t, ok)
		assert.Empty(t, val)
	})

	t.Run("absent key", func(t *testing.T) {
		val, ok := LookupBaggage(ctx, "missing")
		assert.False(t, ok)
		assert.Empty(t, val)
	})

	t.Run("distinguishes absent from empty value", func(t *testing.T) {
		// GetBaggage returns "" for both, LookupBaggage's ok disambiguates.
		_, okEmpty := LookupBaggage(ctx, "empty")
		_, okAbsent := LookupBaggage(ctx, "missing")
		assert.True(t, okEmpty)
		assert.False(t, okAbsent)
	})
}
