package main

import (
	"os"

	"github.com/gurufinglobal/guru/v2/oracle/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
