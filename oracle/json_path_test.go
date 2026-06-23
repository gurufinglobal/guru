package oracle

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractJSONPath(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"data":{"prices":[{"value":"10.5"}]}}`))
	decoder.UseNumber()
	var payload any
	require.NoError(t, decoder.Decode(&payload))

	value, err := ExtractJSONPath(payload, "data.prices[0].value")
	require.NoError(t, err)
	require.Equal(t, "10.5", value)
}

func TestExtractJSONPathMissingField(t *testing.T) {
	value, err := ExtractJSONPath(map[string]any{"data": map[string]any{}}, "data.price")
	require.ErrorContains(t, err, "missing field")
	require.Nil(t, value)
}
