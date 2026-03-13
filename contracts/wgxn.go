package contracts

import (
	_ "embed"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	contractutils "github.com/gurufinglobal/guru/v2/contracts/utils"
)

var (
	// WGXNJSON are the compiled bytes of the WGXNContract
	//
	//go:embed solidity/WGXN.json
	WGXNJSON []byte

	// WGXNContract is the compiled wgxn contract
	WGXNContract evmtypes.CompiledContract
)

func init() {
	var err error
	if WGXNContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		WGXNJSON,
	); err != nil {
		panic(err)
	}
}
