package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestConstitutionCommandRootsAreRegistered(t *testing.T) {
	for _, tc := range []struct {
		name string
		root *cobra.Command
	}{
		{name: "query", root: newQueryCommand()},
		{name: "tx", root: newTxCommand()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, findImmediateSubcommand(tc.root, "constitution"))
		})
	}
}

func TestVoteExtensionPolicyIsNotExposedAsBootstrapCLI(t *testing.T) {
	rootCommand, err := NewRootCmd()
	require.NoError(t, err)
	initCommand := findImmediateSubcommand(rootCommand, "init")
	require.NotNil(t, initCommand)
	require.Nil(t, initCommand.Flags().Lookup("vote-extensions-enable-height"))
	require.Nil(t, findImmediateSubcommand(rootCommand, "vote-extension-preflight"))
}

func findImmediateSubcommand(root *cobra.Command, name string) *cobra.Command {
	for _, command := range root.Commands() {
		if command.Name() == name {
			return command
		}
	}

	return nil
}
