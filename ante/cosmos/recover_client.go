package cosmos

import (
	"fmt"

	ibcclienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	transwaptypes "github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

// RecoverClientDecorator enforces parameter-level scoping for MsgRecoverClient
// executed via authz.MsgExec (Incident phase).
//
// Rules (Phase 2):
//   - MsgRecoverClient.signer must be GOV_ADDR
//   - grant must exist for (grantee=MsgExec.grantee, granter=GOV_ADDR, msgType=MsgRecoverClient)
//   - grant.Authorization must be *transwaptypes.RecoverClientAuthorization (custom type)
//   - subject_client_id must be within the set derived from authorization.allowed_paths:
//     (port,channel) -> channel.connection_hops[0] -> connection.client_id
type RecoverClientDecorator struct {
	ibcKeeper   *ibckeeper.Keeper
	authzKeeper *authzkeeper.Keeper

	govAddr    sdk.AccAddress
	govAddrStr string
}

func NewRecoverClientDecorator(ibcKeeper *ibckeeper.Keeper, authzKeeper *authzkeeper.Keeper) RecoverClientDecorator {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	return RecoverClientDecorator{
		ibcKeeper:   ibcKeeper,
		authzKeeper: authzKeeper,
		govAddr:     govAddr,
		govAddrStr:  govAddr.String(),
	}
}

func (d RecoverClientDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 1); err != nil {
		return ctx, errorsmod.Wrapf(errortypes.ErrUnauthorized, "%s", err.Error())
	}
	return next(ctx, tx, simulate)
}

func (d RecoverClientDecorator) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, nestedLvl int) error {
	if nestedLvl >= maxNestedMsgs {
		return fmt.Errorf("found more nested msgs than permitted; got: %d, expected: <%d", nestedLvl, maxNestedMsgs)
	}

	for _, msg := range msgs {
		if exec, ok := msg.(*authztypes.MsgExec); ok {
			if err := d.checkExec(ctx, exec, nestedLvl+1); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d RecoverClientDecorator) checkExec(ctx sdk.Context, exec *authztypes.MsgExec, nestedLvl int) error {
	if nestedLvl >= maxNestedMsgs {
		return fmt.Errorf("found more nested msgs than permitted; got: %d, expected: <%d", nestedLvl, maxNestedMsgs)
	}

	grantee, err := sdk.AccAddressFromBech32(exec.Grantee)
	if err != nil {
		return fmt.Errorf("invalid MsgExec.grantee: %s", exec.Grantee)
	}

	innerMsgs, err := exec.GetMessages()
	if err != nil {
		return err
	}

	for _, im := range innerMsgs {
		switch m := im.(type) {
		case *authztypes.MsgExec:
			if err := d.checkExec(ctx, m, nestedLvl+1); err != nil {
				return err
			}
		case *ibcclienttypes.MsgRecoverClient:
			if err := d.checkRecoverClient(ctx, grantee, m); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d RecoverClientDecorator) checkRecoverClient(ctx sdk.Context, grantee sdk.AccAddress, rc *ibcclienttypes.MsgRecoverClient) error {
	if rc.Signer != d.govAddrStr {
		return fmt.Errorf("recover-client: signer must be GOV_ADDR (got=%s expected=%s)", rc.Signer, d.govAddrStr)
	}

	// sdk.Context implements context.Context; sdk.WrapSDKContext is deprecated.
	auth, exp := d.authzKeeper.GetAuthorization(ctx, grantee, d.govAddr, transwaptypes.MsgTypeURLRecoverClient)
	if auth == nil {
		return fmt.Errorf("recover-client: missing authz grant (granter=%s grantee=%s msg_type=%s)", d.govAddrStr, grantee.String(), transwaptypes.MsgTypeURLRecoverClient)
	}
	if exp != nil && exp.Before(ctx.BlockTime()) {
		return fmt.Errorf("recover-client: authz grant expired (granter=%s grantee=%s expired_at=%s)", d.govAddrStr, grantee.String(), exp.UTC().Format("2006-01-02T15:04:05Z"))
	}

	scoped, ok := auth.(*transwaptypes.RecoverClientAuthorization)
	if !ok {
		return fmt.Errorf("recover-client: authz grant must be RecoverClientAuthorization (got=%T)", auth)
	}
	// Strict type-url check for operational clarity & defense-in-depth.
	if scoped.MsgTypeUrl != transwaptypes.MsgTypeURLRecoverClient || scoped.MsgTypeURL() != transwaptypes.MsgTypeURLRecoverClient {
		return fmt.Errorf("recover-client: invalid authorization msg_type_url (payload=%s impl=%s expected=%s)", scoped.MsgTypeUrl, scoped.MsgTypeURL(), transwaptypes.MsgTypeURLRecoverClient)
	}

	allowed := map[string]struct{}{}
	for _, p := range scoped.AllowedPaths {
		ch, found := d.ibcKeeper.ChannelKeeper.GetChannel(ctx, p.PortId, p.ChannelId)
		if !found {
			return fmt.Errorf("recover-client: allowed_path channel not found (path=%s/%s)", p.PortId, p.ChannelId)
		}
		if len(ch.ConnectionHops) == 0 {
			return fmt.Errorf("recover-client: allowed_path has no connection hops (path=%s/%s)", p.PortId, p.ChannelId)
		}

		connID := ch.ConnectionHops[0]
		conn, found := d.ibcKeeper.ConnectionKeeper.GetConnection(ctx, connID)
		if !found {
			return fmt.Errorf("recover-client: allowed_path connection not found (path=%s/%s connection_id=%s)", p.PortId, p.ChannelId, connID)
		}
		allowed[conn.ClientId] = struct{}{}
	}

	if _, ok := allowed[rc.SubjectClientId]; !ok {
		return fmt.Errorf("recover-client: subject_client_id out of scope (subject=%s allowed_count=%d)", rc.SubjectClientId, len(allowed))
	}

	return nil
}
