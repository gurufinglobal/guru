package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestAddedCLICommandsAreRegistered(t *testing.T) {
	tests := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{name: "tx oracle", cmd: txCommand, args: []string{"oracle"}},
		{name: "query oracle", cmd: queryCommand, args: []string{"oracle"}},
		{name: "query constitution min gas price", cmd: queryCommand, args: []string{"constitution", "min-gas-price"}},
		{name: "tx feepolicy", cmd: txCommand, args: []string{"feepolicy"}},
		{name: "query feepolicy", cmd: queryCommand, args: []string{"feepolicy"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd()
			if found, _, err := cmd.Find(tc.args); err != nil || found == cmd {
				t.Fatalf("expected %q command to be registered, found=%v err=%v", tc.name, found != nil, err)
			}
		})
	}
}
