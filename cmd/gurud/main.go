package main

import (
	"fmt"
	"os"

	"github.com/cosmos/evm/cmd/gurud/cmd"
	gurudconfig "github.com/cosmos/evm/cmd/gurud/config"
	examplechain "github.com/cosmos/evm/gurud"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func main() {
	setupSDKConfig()

	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, "gurud", examplechain.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}

func setupSDKConfig() {
	config := sdk.GetConfig()
	gurudconfig.SetBech32Prefixes(config)
	config.Seal()
}
