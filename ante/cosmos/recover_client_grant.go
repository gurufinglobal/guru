package cosmos

import (
	"fmt"

	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	transwaptypes "github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

// RecoverClientGrantDecorator validates authz.MsgGrant that embeds the custom
// transwap RecoverClientAuthorization at *grant-time* (Setup phase).
//
// It enforces:
// - granter must be GOV_ADDR
// - allowed_paths must reference existing (port_id, channel_id)
// - the channel must be usable to derive a client_id (connection hop exists, connection exists)
type RecoverClientGrantDecorator struct {
	ibcKeeper  *ibckeeper.Keeper
	govAddrStr string
}

func NewRecoverClientGrantDecorator(ibcKeeper *ibckeeper.Keeper) RecoverClientGrantDecorator {
	return RecoverClientGrantDecorator{
		ibcKeeper:  ibcKeeper,
		govAddrStr: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	}
}

func (d RecoverClientGrantDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 1); err != nil {
		return ctx, errorsmod.Wrapf(errortypes.ErrUnauthorized, "%s", err.Error())
	}
	return next(ctx, tx, simulate)
}

func (d RecoverClientGrantDecorator) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, nestedLvl int) error {
	if nestedLvl >= maxNestedMsgs {
		return fmt.Errorf("found more nested msgs than permitted; got: %d, expected: <%d", nestedLvl, maxNestedMsgs)
	}

	for _, msg := range msgs {
		switch msg := msg.(type) {
		case *authztypes.MsgExec:
			innerMsgs, err := msg.GetMessages()
			if err != nil {
				return err
			}
			if err := d.checkMsgs(ctx, innerMsgs, nestedLvl+1); err != nil {
				return err
			}
		case *authztypes.MsgGrant:
			if err := d.checkGrant(ctx, msg); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d RecoverClientGrantDecorator) checkGrant(ctx sdk.Context, grant *authztypes.MsgGrant) error {
	authorization, err := grant.GetAuthorization()
	if err != nil {
		return err
	}

	rcAuth, ok := authorization.(*transwaptypes.RecoverClientAuthorization)
	if !ok {
		return nil
	}

	// Strict msg type-url check for operational clarity & defense-in-depth.
	if rcAuth.MsgTypeUrl != transwaptypes.MsgTypeURLRecoverClient || rcAuth.MsgTypeURL() != transwaptypes.MsgTypeURLRecoverClient {
		return fmt.Errorf("recover-client-grant: invalid authorization msg_type_url (payload=%s impl=%s expected=%s)", rcAuth.MsgTypeUrl, rcAuth.MsgTypeURL(), transwaptypes.MsgTypeURLRecoverClient)
	}

	if grant.Granter != d.govAddrStr {
		return fmt.Errorf("recover-client-grant: granter must be GOV_ADDR (got=%s expected=%s)", grant.Granter, d.govAddrStr)
	}

	for _, p := range rcAuth.AllowedPaths {
		if p.PortId != transwaptypes.ModuleName {
			return fmt.Errorf("recover-client-grant: allowed_paths port_id must be %s (got %s)", transwaptypes.ModuleName, p.PortId)
		}

		ch, found := d.ibcKeeper.ChannelKeeper.GetChannel(ctx, p.PortId, p.ChannelId)
		if !found {
			return fmt.Errorf("recover-client-grant: allowed_path channel not found (path=%s/%s)", p.PortId, p.ChannelId)
		}
		if len(ch.ConnectionHops) == 0 {
			return fmt.Errorf("recover-client-grant: allowed_path has no connection hops (path=%s/%s)", p.PortId, p.ChannelId)
		}

		connID := ch.ConnectionHops[0]
		conn, found := d.ibcKeeper.ConnectionKeeper.GetConnection(ctx, connID)
		if !found {
			return fmt.Errorf("recover-client-grant: allowed_path references missing connection (path=%s/%s connection_id=%s)", p.PortId, p.ChannelId, connID)
		}
		if conn.ClientId == "" {
			return fmt.Errorf("recover-client-grant: allowed_path resolves to empty client_id (path=%s/%s connection_id=%s)", p.PortId, p.ChannelId, connID)
		}
	}

	return nil
}
