package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	feepolicytypes "github.com/gurufinglobal/guru/v2/x/feepolicy/types"
)

type FeePolicyKeeper interface {
	GetDiscount(ctx sdk.Context, feePayerAddr string, msgs []sdk.Msg) feepolicytypes.Discount
}
