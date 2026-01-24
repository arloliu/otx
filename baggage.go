package otx

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/baggage"
)

// SetBaggage adds a key-value pair to baggage in the context.
//
// Keys and values must conform to the W3C Baggage specification:
//   - Keys: Must be valid HTTP header tokens (alphanumeric, hyphen, underscore, tilde).
//     Invalid characters: spaces, control chars, and most punctuation.
//   - Values: Must be URL-encoded or percent-encoded if containing special characters.
//     Invalid characters: control chars (0x00-0x1F, 0x7F).
//
// Returns an error if key or value violates these constraints.
func SetBaggage(ctx context.Context, key, value string) (context.Context, error) {
	bag := baggage.FromContext(ctx)
	member, err := baggage.NewMember(key, value)
	if err != nil {
		return ctx, fmt.Errorf("create baggage member: %w", err)
	}
	bag, err = bag.SetMember(member)
	if err != nil {
		return ctx, fmt.Errorf("set baggage member: %w", err)
	}

	return baggage.ContextWithBaggage(ctx, bag), nil
}

// MustSetBaggage adds a key-value pair to baggage, panicking on error.
// Use when key/value are known to be valid (e.g., hardcoded keys).
//
// See [SetBaggage] for key/value format requirements.
// Panics if key or value violates W3C Baggage specification.
func MustSetBaggage(ctx context.Context, key, value string) context.Context {
	newCtx, err := SetBaggage(ctx, key, value)
	if err != nil {
		panic(fmt.Sprintf("otx: invalid baggage key=%q value=%q: %v", key, value, err))
	}

	return newCtx
}

// GetBaggage retrieves a value from baggage in the context.
func GetBaggage(ctx context.Context, key string) string {
	bag := baggage.FromContext(ctx)
	return bag.Member(key).Value()
}

// DeleteBaggage removes a key from baggage in the context.
func DeleteBaggage(ctx context.Context, key string) context.Context {
	bag := baggage.FromContext(ctx)
	bag = bag.DeleteMember(key)
	return baggage.ContextWithBaggage(ctx, bag)
}

// AllBaggage returns all baggage members as a map.
func AllBaggage(ctx context.Context) map[string]string {
	bag := baggage.FromContext(ctx)
	result := make(map[string]string)
	for _, m := range bag.Members() {
		result[m.Key()] = m.Value()
	}

	return result
}
