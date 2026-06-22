package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

func (k *Keeper) ValidateTxSelfBondConstraints(ctx context.Context, tx sdk.Tx) error {
	if tx == nil {
		return nil
	}

	var (
		minBond       sdkmath.Int
		minBondLoaded bool
	)
	getMinBond := func() (sdkmath.Int, error) {
		if minBondLoaded {
			return minBond, nil
		}
		loadedMinBond, err := k.minBondSource.GetMinValidatorBondAmount(ctx)
		if err != nil {
			return sdkmath.Int{}, err
		}
		minBond = loadedMinBond
		minBondLoaded = true
		return minBond, nil
	}

	for _, msg := range tx.GetMsgs() {
		if err := k.validateMsgSelfBondConstraints(msg, getMinBond); err != nil {
			return err
		}
	}

	return nil
}

type minBondGetter func() (sdkmath.Int, error)

func (k *Keeper) validateMsgSelfBondConstraints(
	msg sdk.Msg,
	getMinBond minBondGetter,
) error {
	switch m := msg.(type) {
	case *stakingtypes.MsgCreateValidator:
		return k.validateCreateValidatorSelfBond(m, getMinBond)
	case *authztypes.MsgExec:
		msgs, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, nested := range msgs {
			if err := k.validateMsgSelfBondConstraints(nested, getMinBond); err != nil {
				return err
			}
		}
	}

	return nil
}

func (k *Keeper) validateCreateValidatorSelfBond(
	msg *stakingtypes.MsgCreateValidator,
	getMinBond minBondGetter,
) error {
	if msg.Value.Denom != appparams.BaseDenom {
		return nil
	}

	minBond, err := getMinBond()
	if err != nil {
		return err
	}
	if msg.Value.Amount.LT(minBond) {
		return constitutiontypes.ErrSelfBondBelowMin.Wrapf(
			"msg create validator self-bond %s below minimum %s",
			msg.Value.Amount,
			minBond,
		)
	}

	return nil
}
