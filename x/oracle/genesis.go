package oracle

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/GPTx-global/guru-v2/x/oracle/keeper"
	"github.com/GPTx-global/guru-v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// InitGenesis new oracle genesis
func InitGenesis(ctx sdk.Context, k keeper.Keeper, data types.GenesisState) {
	// Set genesis state
	params := data.Params
	err := k.SetParams(ctx, params)
	if err != nil {
		panic(errorsmod.Wrapf(err, "error setting params"))
	}

	// Set moderator address
	moderatorAddress := data.ModeratorAddress
	if moderatorAddress == "" {
		panic(errorsmod.Wrapf(errortypes.ErrInvalidRequest, "%s: moderator address cannot be empty", types.ModuleName))
	}

	err = k.SetModeratorAddress(ctx, moderatorAddress)
	if err != nil {
		panic(errorsmod.Wrapf(err, "error setting moderator address"))
	}

	// Set oracle request documents
	oracleDocs := data.OracleRequestDocs
	for _, doc := range oracleDocs {
		err := doc.Validate()
		if err != nil {
			panic(errorsmod.Wrapf(err, "error validating oracle request doc"))
		}
		k.SetOracleRequestDoc(ctx, doc)
	}

	if uint64(len(data.OracleRequestDocs)) > 0 && data.OracleRequestDocCount < uint64(len(data.OracleRequestDocs)) {
		panic(errorsmod.Wrapf(errortypes.ErrInvalidRequest, "%s: oracle request doc count is less than the number of oracle request docs", types.ModuleName))
	}

	// Set oracle request doc count
	k.SetOracleRequestDocCount(ctx, data.OracleRequestDocCount)
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func ExportGenesis(ctx sdk.Context, keeper keeper.Keeper) types.GenesisState {
	// Get the current parameters from the keeper
	params := keeper.GetParams(ctx)

	// Get the current moderator address from the keeper
	moderatorAddress := keeper.GetModeratorAddress(ctx)

	// Get the current oracle request doc count from the keeper
	oracleRequestDocCount := keeper.GetOracleRequestDocCount(ctx)

	// Get the current oracle request documents from the keeper
	tmpDocs := keeper.GetOracleRequestDocs(ctx)

	// Initialize a new slice to hold the oracle request documents
	docs := make([]types.OracleRequestDoc, len(tmpDocs))

	// Copy the oracle request documents from the temporary slice to the new slice
	for i, doc := range tmpDocs {
		docs[i] = *doc
	}

	return types.NewGenesisState(params, docs, moderatorAddress, oracleRequestDocCount)
}
