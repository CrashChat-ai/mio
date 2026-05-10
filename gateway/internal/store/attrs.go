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
