package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetQueryCmdIncludesConstitutionCommands(t *testing.T) {
	cmd := GetQueryCmd()

	for _, name := range []string{"params", "base-address", "moderator-address", "separation-ratio", "min-gas-price"} {
		_, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
	}
}
