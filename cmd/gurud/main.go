package main

import (
	"fmt"
	"os"

	"github.com/GPTx-global/guru-v2/cmd/gurud/cmd"
	gurudconfig "github.com/GPTx-global/guru-v2/cmd/gurud/config"
	examplechain "github.com/GPTx-global/guru-v2/gurud"

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
