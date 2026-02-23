package main

import (
	"encoding/json"
	"fmt"

	evmtypes "github.com/cosmos/evm/x/vm/types"
)

func main() {
	preinstalls := evmtypes.DefaultPreinstalls
	b, err := json.MarshalIndent(preinstalls, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
