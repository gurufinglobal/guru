package harness

import (
	"encoding/json"
	"fmt"
)

func MustUnmarshalJSON[T any](data []byte, target *T) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal json: %w", err)
	}
	return nil
}

func MarshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}
	return b, nil
}
