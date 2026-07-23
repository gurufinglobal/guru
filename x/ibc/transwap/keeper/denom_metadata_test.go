package keeper

import (
	"context"
	"testing"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	"github.com/stretchr/testify/require"
)

type metadataCaptureBankKeeper struct {
	*refundAccountingBankKeeper
	metadata map[string]banktypes.Metadata
}

func newMetadataCaptureBankKeeper() *metadataCaptureBankKeeper {
	return &metadataCaptureBankKeeper{
		refundAccountingBankKeeper: newRefundAccountingBankKeeper(),
		metadata:                   make(map[string]banktypes.Metadata),
	}
}

func (m *metadataCaptureBankKeeper) HasDenomMetaData(_ context.Context, denom string) bool {
	_, found := m.metadata[denom]
	return found
}

func (m *metadataCaptureBankKeeper) SetDenomMetaData(_ context.Context, metadata banktypes.Metadata) {
	m.metadata[metadata.Base] = metadata
}

func TestSetDenomMetadataIsBankGenesisValid(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	bank := newMetadataCaptureBankKeeper()
	k.BankKeeper = bank
	denom := types.NewDenom("ufoo", types.NewHop(types.PortID, "channel-7"))
	bankDenom := types.DenomIBCDenom(denom)

	k.SetDenomMetadata(ctx, denom)

	metadata, found := bank.metadata[bankDenom]
	require.True(t, found)
	require.NoError(t, metadata.Validate())
	require.Equal(t, bankDenom, metadata.Base)
	require.Equal(t, bankDenom, metadata.Display)
	require.Len(t, metadata.DenomUnits, 1)
	require.Equal(t, bankDenom, metadata.DenomUnits[0].Denom)
	require.Zero(t, metadata.DenomUnits[0].Exponent)
	require.ElementsMatch(t, []string{"ufoo", "transwap/channel-7/ufoo"}, metadata.DenomUnits[0].Aliases)
	require.Contains(t, metadata.Name, "transwap/channel-7/ufoo")
	require.Equal(t, "UFOO", metadata.Symbol)
}

func TestSetDenomMetadataDeduplicatesUntracedAlias(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	bank := newMetadataCaptureBankKeeper()
	k.BankKeeper = bank
	denom := types.NewDenom("ufoo")

	k.SetDenomMetadata(ctx, denom)

	metadata := bank.metadata[types.DenomIBCDenom(denom)]
	require.NoError(t, metadata.Validate())
	require.Empty(t, metadata.DenomUnits[0].Aliases)
}

func TestInitGenesisPreservesExistingBankMetadata(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)
	bank := newMetadataCaptureBankKeeper()
	k.BankKeeper = bank
	denom := types.NewDenom("ufoo", types.NewHop(types.PortID, "channel-7"))
	bankDenom := types.DenomIBCDenom(denom)
	existing := banktypes.Metadata{
		Description: "operator managed",
		DenomUnits: []*banktypes.DenomUnit{{
			Denom:    bankDenom,
			Exponent: 0,
		}},
		Base:    bankDenom,
		Display: bankDenom,
		Name:    "Operator IBC Asset",
		Symbol:  "OPIBC",
		URI:     "https://example.invalid/metadata.json",
		URIHash: "sha256:example",
	}
	require.NoError(t, existing.Validate())
	bank.metadata[bankDenom] = existing
	genesis := types.NewGenesisState(types.PortID, types.Denoms{denom}, nil)

	k.InitGenesis(ctx, genesis)

	require.Equal(t, existing, bank.metadata[bankDenom])
}
