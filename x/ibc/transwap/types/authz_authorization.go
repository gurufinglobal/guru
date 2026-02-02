package types

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// MsgTypeURLRecoverClient is the fully-qualified Msg URL for IBC RecoverClient.
//
// We keep it as a string constant (instead of importing ibc-go client packages)
// to reduce coupling and keep Phase 1 lightweight.
const MsgTypeURLRecoverClient = "/ibc.core.client.v1.MsgRecoverClient"

var _ authz.Authorization = &RecoverClientAuthorization{}

// MsgTypeURL implements authz.Authorization.
func (a RecoverClientAuthorization) MsgTypeURL() string {
	return MsgTypeURLRecoverClient
}

// Accept implements authz.Authorization.
//
// Phase 1: only checks the msg type. Parameter-level scoping (allowed_paths) is
// enforced in later phases (e.g., Ante decorator for MsgExec).
func (a RecoverClientAuthorization) Accept(_ context.Context, msg sdk.Msg) (authz.AcceptResponse, error) {
	if sdk.MsgTypeURL(msg) != MsgTypeURLRecoverClient {
		return authz.AcceptResponse{}, sdkerrors.ErrUnauthorized.Wrapf("unexpected msg type: %s", sdk.MsgTypeURL(msg))
	}
	return authz.AcceptResponse{Accept: true}, nil
}

// ValidateBasic implements authz.Authorization.
//
// We validate allowed_paths at grant-time so that a governance proposal cannot
// accidentally create a "blank" grant.
func (a RecoverClientAuthorization) ValidateBasic() error {
	if a.MsgTypeUrl == "" {
		return sdkerrors.ErrInvalidRequest.Wrap("msg_type_url must not be empty")
	}
	if a.MsgTypeUrl != MsgTypeURLRecoverClient {
		return sdkerrors.ErrInvalidRequest.Wrapf("msg_type_url must be %s", MsgTypeURLRecoverClient)
	}
	if len(a.AllowedPaths) == 0 {
		return sdkerrors.ErrInvalidRequest.Wrap("allowed_paths must not be empty")
	}

	for i, p := range a.AllowedPaths {
		if p.PortId == "" {
			return sdkerrors.ErrInvalidRequest.Wrapf("allowed_paths[%d].port_id must not be empty", i)
		}
		if p.ChannelId == "" {
			return sdkerrors.ErrInvalidRequest.Wrapf("allowed_paths[%d].channel_id must not be empty", i)
		}
	}

	// Prevent duplicates to reduce operator mistakes.
	seen := map[string]struct{}{}
	for _, p := range a.AllowedPaths {
		key := fmt.Sprintf("%s/%s", p.PortId, p.ChannelId)
		if _, ok := seen[key]; ok {
			return sdkerrors.ErrInvalidRequest.Wrapf("duplicate allowed_path: %s", key)
		}
		seen[key] = struct{}{}
	}

	return nil
}
