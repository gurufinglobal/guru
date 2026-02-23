package keeper

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/vm/statedb"
	"github.com/gurufinglobal/guru/v2/x/vm/types"
)

// ApplyPreinstall deploys a preinstalled contract at the given address with the given code.
func (k *Keeper) ApplyPreinstall(ctx sdk.Context, preinstall *types.Preinstall) error {
	if preinstall == nil {
		return fmt.Errorf("preinstall is nil")
	}

	addr := common.HexToAddress(preinstall.Address)

	code, err := hex.DecodeString(preinstall.Code)
	if err != nil {
		return fmt.Errorf("failed to decode preinstall code for %s: %w", preinstall.Name, err)
	}

	codeHash := crypto.Keccak256Hash(code)

	// Set the account with zero nonce and balance
	if err := k.SetAccount(ctx, addr, statedb.Account{
		Nonce:    0,
		Balance:  uint256.NewInt(0),
		CodeHash: codeHash.Bytes(),
	}); err != nil {
		return fmt.Errorf("failed to set account for preinstall %s: %w", preinstall.Name, err)
	}

	// Set the code
	k.SetCode(ctx, codeHash.Bytes(), code)

	k.Logger(ctx).Info(
		"preinstall contract deployed",
		"name", preinstall.Name,
		"address", addr.Hex(),
		"code_hash", codeHash.Hex(),
	)

	return nil
}
