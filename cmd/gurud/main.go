package main

import (
	"fmt"
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"github.com/gurufinglobal/guru/v2/cmd/gurud/cmd"
	"github.com/gurufinglobal/guru/v2/config"
)

func run() error {
	rootCommand, err := cmd.NewRootCmd()
	if err != nil {
		return err
	}
	home, err := config.DefaultNodeHome()
	if err != nil {
		return err
	}

	return svrcmd.Execute(rootCommand, config.EnvPrefix, home)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
