package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

// InitGenesis initializes the ibc-transfer state and binds to PortID.
func (k Keeper) InitGenesis(ctx sdk.Context, state *transwapv1.GenesisState) {
	k.SetPort(ctx, state.PortId)
	if err := k.SetParams(ctx, state.GetParams()); err != nil {
		panic(err)
	}

	for _, denom := range state.Denoms {
		k.SetDenom(ctx, denom)
		// Bank genesis is initialized before TransSwap and may contain richer
		// operator-managed metadata. Preserve it exactly; only synthesize the
		// minimal valid IBC metadata when the bank store has no entry.
		bankDenom := types.DenomIBCDenom(denom)
		if !k.BankKeeper.HasDenomMetaData(ctx, bankDenom) {
			k.SetDenomMetadata(ctx, denom)
		}
	}

	// Every denom will have only one total escrow amount, since any
	// duplicate entry will fail validation in Validate of GenesisState
	totalEscrowed, err := types.ProtoCoinsToSDK(state.TotalEscrowed)
	if err != nil {
		panic(err)
	}
	for _, denomEscrow := range totalEscrowed {
		k.SetTotalEscrowForDenom(ctx, denomEscrow)
	}
	for _, refund := range state.GetRefunds() {
		if err := k.SetRefundRecord(ctx, refund); err != nil {
			panic(err)
		}
		switch refund.GetStatus() {
		case transwapv1.RefundStatus_REFUND_STATUS_PENDING:
			key := refundPacketIndexKey(
				refund.GetOriginalOutputPort(),
				refund.GetOriginalOutputChannel(),
				refund.GetOriginalOutputSequence(),
			)
			k.refundOutputStore(ctx).Set([]byte(key), []byte(refund.GetId()))
		case transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT:
			if err := k.setActiveRefundPacketIndex(ctx, refund); err != nil {
				panic(err)
			}
		case transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE:
			if err := k.restoreRefundRetrySchedule(ctx, refund); err != nil {
				panic(err)
			}
		}
	}
}

// ExportGenesis exports ibc-transfer module's portID and denom trace info into its genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *transwapv1.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	return &transwapv1.GenesisState{
		PortId:        k.GetPort(ctx),
		Denoms:        k.GetAllDenoms(ctx),
		TotalEscrowed: types.SDKCoinsToProto(k.GetAllTotalEscrowed(ctx)),
		Params:        params,
		Refunds:       k.GetAllRefundRecords(ctx),
	}
}
