package keeper

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
)

var _ constitutiontypes.MsgServer = MsgServer{}

type MsgServer struct {
	constitutiontypes.UnimplementedMsgServer

	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) MsgServer {
	return MsgServer{keeper: keeper}
}

func (m MsgServer) UpdateParams(goCtx context.Context, req *constitutiontypes.MsgUpdateParams) (*constitutiontypes.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateAuthority(req.Authority); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &constitutiontypes.MsgUpdateParamsResponse{}, nil
}

func (m MsgServer) UpdateBaseAddress(goCtx context.Context, req *constitutiontypes.MsgUpdateBaseAddress) (*constitutiontypes.MsgUpdateBaseAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.Moderator); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateBaseAddress(ctx, req.BaseAddress); err != nil {
		return nil, err
	}

	return &constitutiontypes.MsgUpdateBaseAddressResponse{}, nil
}

func (m MsgServer) UpdateModeratorAddress(goCtx context.Context, req *constitutiontypes.MsgUpdateModeratorAddress) (*constitutiontypes.MsgUpdateModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.Moderator); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateModeratorAddress(ctx, req.ModeratorAddress); err != nil {
		return nil, err
	}

	return &constitutiontypes.MsgUpdateModeratorAddressResponse{}, nil
}

func (m MsgServer) UpdateSeparationRatio(goCtx context.Context, req *constitutiontypes.MsgUpdateSeparationRatio) (*constitutiontypes.MsgUpdateSeparationRatioResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.Moderator); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateSeparationRatio(ctx, req.SeparationRatio); err != nil {
		return nil, err
	}

	return &constitutiontypes.MsgUpdateSeparationRatioResponse{}, nil
}

func (m MsgServer) validateAuthority(authority string) error {
	expectedAuthority, err := m.keeper.AuthorityAddressString()
	if err != nil {
		return err
	}

	return m.validateAddressMatches("authority", expectedAuthority, authority)
}

func (m MsgServer) validateModerator(ctx sdk.Context, moderator string) error {
	currentModerator, err := m.keeper.GetModeratorAddress(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return constitutiontypes.ErrInvalidAuthority.Wrap("moderator_address is not initialized")
		}
		return err
	}

	return m.validateAddressMatches("moderator", currentModerator, moderator)
}

func (m MsgServer) validateAddressMatches(fieldName, expected, actual string) error {
	expectedAddress, err := m.decodeAuthorityAddress("expected "+fieldName, expected)
	if err != nil {
		return err
	}
	actualAddress, err := m.decodeAuthorityAddress(fieldName, actual)
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedAddress, actualAddress) {
		return constitutiontypes.ErrInvalidAuthority.Wrapf("invalid authority: expected %s, got %s", expected, actual)
	}

	return nil
}

func (m MsgServer) decodeAuthorityAddress(fieldName, value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, constitutiontypes.ErrInvalidAuthority.Wrapf("%s cannot be empty", fieldName)
	}

	address, err := m.keeper.accountCodec.StringToBytes(value)
	if err != nil {
		return nil, constitutiontypes.ErrInvalidAuthority.Wrapf("invalid %s: %v", fieldName, err)
	}

	return address, nil
}
