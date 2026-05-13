package store

import (
	"encoding/json"
	"fmt"
)

// marshalAttrs converts a Go map[string]string to a JSON byte slice
// suitable for pgx JSONB columns. Returns "{}" on nil input.
func marshalAttrs(m map[string]string) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal attrs: %w", err)
	}
	return b, nil
}

// unmarshalAttrs decodes a JSONB column back into map[string]string.
// Nil / empty bytes round-trip to an empty map (never nil) so callers
// can safely range without checks.
func unmarshalAttrs(b []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(b) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal attrs: %w", err)
	}
	return out, nil
}
