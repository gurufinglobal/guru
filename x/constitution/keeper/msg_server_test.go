package keeper

import (
	"testing"

	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestMsgServerUpdateParams(t *testing.T) {
	tests := []struct {
		name          string
		withInitParam bool
		request       *constitutiontypes.MsgUpdateParams
		shouldErr     bool
	}{
		{
			name:          "fails on nil request",
			withInitParam: true,
			request:       nil,
			shouldErr:     true,
		},
		{
			name:          "fails on invalid authority",
			withInitParam: true,
			request: &constitutiontypes.MsgUpdateParams{
				Authority: "invalid-authority",
				Params:    testParams("12"),
			},
			shouldErr: true,
		},
		{
			name:          "fails on invalid params",
			withInitParam: true,
			request: &constitutiontypes.MsgUpdateParams{
				Params: testParams("0"),
			},
			shouldErr: true,
		},
		{
			name:          "updates params even when current params are missing",
			withInitParam: false,
			request: &constitutiontypes.MsgUpdateParams{
				Params: testParams("12"),
			},
			shouldErr: false,
		},
		{
			name:          "updates params successfully",
			withInitParam: true,
			request: &constitutiontypes.MsgUpdateParams{
				Params: testParams("12"),
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f keeperTestFixture
			if tc.withInitParam {
				f = setupKeeperFixture(t)
			} else {
				f = setupKeeperFixtureWithoutParams(t)
			}

			var req *constitutiontypes.MsgUpdateParams
			if tc.request != nil {
				req = &constitutiontypes.MsgUpdateParams{
					Authority: tc.request.Authority,
					Params:    tc.request.Params,
				}
			}
			if req != nil && req.Authority == "" {
				req.Authority = f.authority
			}

			msgServer := NewMsgServer(&f.keeper)
			_, err := msgServer.UpdateParams(f.ctx, req)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			params, err := f.keeper.GetParams(f.ctx)
			require.NoError(t, err)
			require.Equal(t, "12", params.GetMinValidatorBondAmount().Amount.String())
		})
	}
}

func TestMsgServerUpdateParamsRejectsUnrelatedAuthority(t *testing.T) {
	f := setupKeeperFixture(t)
	consensusAuthority := testAddress(t, f.keeper.accountCodec, 0x08)
	msgServer := NewMsgServer(&f.keeper)

	_, err := msgServer.UpdateParams(f.ctx, &constitutiontypes.MsgUpdateParams{
		Authority: consensusAuthority,
		Params:    testParams("12"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, constitutiontypes.ErrInvalidAuthority)

	_, err = msgServer.UpdateParams(f.ctx, &constitutiontypes.MsgUpdateParams{
		Authority: f.authority,
		Params:    testParams("12"),
	})
	require.NoError(t, err)
}

func TestMsgServerUpdateParamsAcceptsAuthorityHexEquivalent(t *testing.T) {
	f := setupKeeperFixture(t)
	msgServer := NewMsgServer(&f.keeper)

	_, err := msgServer.UpdateParams(f.ctx, &constitutiontypes.MsgUpdateParams{
		Authority: testHexAddress(0x01),
		Params:    testParams("12"),
	})
	require.NoError(t, err)
}

func TestMsgServerUpdateBaseAddress(t *testing.T) {
	tests := []struct {
		name      string
		request   *constitutiontypes.MsgUpdateBaseAddress
		shouldErr bool
	}{
		{
			name:      "fails on nil request",
			request:   nil,
			shouldErr: true,
		},
		{
			name: "fails on invalid moderator",
			request: &constitutiontypes.MsgUpdateBaseAddress{
				Moderator:   "invalid-moderator",
				BaseAddress: "invalid-base",
			},
			shouldErr: true,
		},
		{
			name: "fails on invalid base address",
			request: &constitutiontypes.MsgUpdateBaseAddress{
				BaseAddress: "invalid-base",
			},
			shouldErr: true,
		},
		{
			name: "fails when base address equals authority",
			request: &constitutiontypes.MsgUpdateBaseAddress{
				BaseAddress: "",
			},
			shouldErr: true,
		},
		{
			name: "updates base address successfully",
			request: &constitutiontypes.MsgUpdateBaseAddress{
				BaseAddress: "",
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			var req *constitutiontypes.MsgUpdateBaseAddress
			if tc.request != nil {
				req = &constitutiontypes.MsgUpdateBaseAddress{
					Moderator:   tc.request.Moderator,
					BaseAddress: tc.request.BaseAddress,
				}
			}
			if req != nil && req.Moderator == "" {
				req.Moderator = f.moderatorAddress
			}
			if req != nil && req.BaseAddress == "" {
				if tc.name == "fails when base address equals authority" {
					req.BaseAddress = f.authority
				} else {
					req.BaseAddress = testAddress(t, f.keeper.accountCodec, 0x04)
				}
			}

			_, err := msgServer.UpdateBaseAddress(f.ctx, req)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			baseAddress, err := f.keeper.GetBaseAddress(f.ctx)
			require.NoError(t, err)
			require.Equal(t, req.BaseAddress, baseAddress)
		})
	}
}

func TestMsgServerModeratorMessagesRejectUnrelatedAddress(t *testing.T) {
	tests := []struct {
		name string
		run  func(MsgServer, keeperTestFixture, string) error
	}{
		{
			name: "update base address",
			run: func(msgServer MsgServer, f keeperTestFixture, moderator string) error {
				_, err := msgServer.UpdateBaseAddress(f.ctx, &constitutiontypes.MsgUpdateBaseAddress{
					Moderator:   moderator,
					BaseAddress: testAddress(t, f.keeper.accountCodec, 0x09),
				})
				return err
			},
		},
		{
			name: "update moderator address",
			run: func(msgServer MsgServer, f keeperTestFixture, moderator string) error {
				_, err := msgServer.UpdateModeratorAddress(f.ctx, &constitutiontypes.MsgUpdateModeratorAddress{
					Moderator:        moderator,
					ModeratorAddress: testAddress(t, f.keeper.accountCodec, 0x0a),
				})
				return err
			},
		},
		{
			name: "update separation ratio",
			run: func(msgServer MsgServer, f keeperTestFixture, moderator string) error {
				_, err := msgServer.UpdateSeparationRatio(f.ctx, &constitutiontypes.MsgUpdateSeparationRatio{
					Moderator:       moderator,
					SeparationRatio: testSeparationRatio(100_000, 200_000, 700_000),
				})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			consensusAuthority := testAddress(t, f.keeper.accountCodec, 0x0b)
			msgServer := NewMsgServer(&f.keeper)

			err := tc.run(msgServer, f, consensusAuthority)
			require.Error(t, err)
			require.ErrorIs(t, err, constitutiontypes.ErrInvalidAuthority)

			err = tc.run(msgServer, f, f.moderatorAddress)
			require.NoError(t, err)
		})
	}
}

func TestMsgServerModeratorMessagesAcceptModeratorHexEquivalent(t *testing.T) {
	tests := []struct {
		name string
		run  func(MsgServer, keeperTestFixture, string) error
	}{
		{
			name: "update base address",
			run: func(msgServer MsgServer, f keeperTestFixture, moderator string) error {
				_, err := msgServer.UpdateBaseAddress(f.ctx, &constitutiontypes.MsgUpdateBaseAddress{
					Moderator:   moderator,
					BaseAddress: testAddress(t, f.keeper.accountCodec, 0x0c),
				})
				return err
			},
		},
		{
			name: "update moderator address",
			run: func(msgServer MsgServer, f keeperTestFixture, moderator string) error {
				_, err := msgServer.UpdateModeratorAddress(f.ctx, &constitutiontypes.MsgUpdateModeratorAddress{
					Moderator:        moderator,
					ModeratorAddress: testAddress(t, f.keeper.accountCodec, 0x0d),
				})
				return err
			},
		},
		{
			name: "update separation ratio",
			run: func(msgServer MsgServer, f keeperTestFixture, moderator string) error {
				_, err := msgServer.UpdateSeparationRatio(f.ctx, &constitutiontypes.MsgUpdateSeparationRatio{
					Moderator:       moderator,
					SeparationRatio: testSeparationRatio(100_000, 200_000, 700_000),
				})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			err := tc.run(msgServer, f, testHexAddress(0x02))
			require.NoError(t, err)
		})
	}
}

func TestMsgServerUpdateBaseAddressRejectsBlockedAddressHexEquivalent(t *testing.T) {
	f := setupKeeperFixture(t)
	blockedAddress := testAddress(t, f.keeper.accountCodec, 0x0e)
	f.bankKeeper.SetBlockedAddressString(blockedAddress, true)
	msgServer := NewMsgServer(&f.keeper)

	_, err := msgServer.UpdateBaseAddress(f.ctx, &constitutiontypes.MsgUpdateBaseAddress{
		Moderator:   f.moderatorAddress,
		BaseAddress: testHexAddress(0x0e),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, constitutiontypes.ErrInvalidParams)
}

func TestMsgServerUpdateModeratorAddress(t *testing.T) {
	tests := []struct {
		name      string
		request   *constitutiontypes.MsgUpdateModeratorAddress
		shouldErr bool
	}{
		{
			name:      "fails on nil request",
			request:   nil,
			shouldErr: true,
		},
		{
			name: "fails on invalid moderator",
			request: &constitutiontypes.MsgUpdateModeratorAddress{
				Moderator:        "invalid-moderator",
				ModeratorAddress: "invalid-address",
			},
			shouldErr: true,
		},
		{
			name: "fails on invalid new moderator address",
			request: &constitutiontypes.MsgUpdateModeratorAddress{
				ModeratorAddress: "invalid-address",
			},
			shouldErr: true,
		},
		{
			name: "fails when moderator address equals authority",
			request: &constitutiontypes.MsgUpdateModeratorAddress{
				ModeratorAddress: "",
			},
			shouldErr: true,
		},
		{
			name: "updates moderator address successfully",
			request: &constitutiontypes.MsgUpdateModeratorAddress{
				ModeratorAddress: "",
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			var req *constitutiontypes.MsgUpdateModeratorAddress
			if tc.request != nil {
				req = &constitutiontypes.MsgUpdateModeratorAddress{
					Moderator:        tc.request.Moderator,
					ModeratorAddress: tc.request.ModeratorAddress,
				}
			}
			if req != nil && req.Moderator == "" {
				req.Moderator = f.moderatorAddress
			}
			if req != nil && req.ModeratorAddress == "" {
				if tc.name == "fails when moderator address equals authority" {
					req.ModeratorAddress = f.authority
				} else {
					req.ModeratorAddress = testAddress(t, f.keeper.accountCodec, 0x05)
				}
			}

			_, err := msgServer.UpdateModeratorAddress(f.ctx, req)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			moderatorAddress, err := f.keeper.GetModeratorAddress(f.ctx)
			require.NoError(t, err)
			require.Equal(t, req.ModeratorAddress, moderatorAddress)
		})
	}
}

func TestMsgServerUpdateModeratorAddressReplacesSigner(t *testing.T) {
	f := setupKeeperFixture(t)
	msgServer := NewMsgServer(&f.keeper)

	newModerator := testAddress(t, f.keeper.accountCodec, 0x06)
	_, err := msgServer.UpdateModeratorAddress(f.ctx, &constitutiontypes.MsgUpdateModeratorAddress{
		Moderator:        f.moderatorAddress,
		ModeratorAddress: newModerator,
	})
	require.NoError(t, err)

	_, err = msgServer.UpdateBaseAddress(f.ctx, &constitutiontypes.MsgUpdateBaseAddress{
		Moderator:   f.moderatorAddress,
		BaseAddress: testAddress(t, f.keeper.accountCodec, 0x07),
	})
	require.Error(t, err)

	_, err = msgServer.UpdateBaseAddress(f.ctx, &constitutiontypes.MsgUpdateBaseAddress{
		Moderator:   newModerator,
		BaseAddress: testAddress(t, f.keeper.accountCodec, 0x07),
	})
	require.NoError(t, err)
}

func TestMsgServerUpdateSeparationRatio(t *testing.T) {
	tests := []struct {
		name      string
		request   *constitutiontypes.MsgUpdateSeparationRatio
		shouldErr bool
	}{
		{
			name:      "fails on nil request",
			request:   nil,
			shouldErr: true,
		},
		{
			name: "fails on invalid moderator",
			request: &constitutiontypes.MsgUpdateSeparationRatio{
				Moderator:       "invalid-moderator",
				SeparationRatio: testSeparationRatio(200_000, 300_000, 500_000),
			},
			shouldErr: true,
		},
		{
			name: "fails on invalid separation ratio",
			request: &constitutiontypes.MsgUpdateSeparationRatio{
				SeparationRatio: testSeparationRatio(200_000, 300_000, 400_000),
			},
			shouldErr: true,
		},
		{
			name: "updates separation ratio successfully",
			request: &constitutiontypes.MsgUpdateSeparationRatio{
				SeparationRatio: testSeparationRatio(100_000, 200_000, 700_000),
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			var req *constitutiontypes.MsgUpdateSeparationRatio
			if tc.request != nil {
				req = &constitutiontypes.MsgUpdateSeparationRatio{
					Moderator:       tc.request.Moderator,
					SeparationRatio: tc.request.SeparationRatio,
				}
			}
			if req != nil && req.Moderator == "" {
				req.Moderator = f.moderatorAddress
			}

			_, err := msgServer.UpdateSeparationRatio(f.ctx, req)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			ratio, err := f.keeper.GetSeparationRatio(f.ctx)
			require.NoError(t, err)
			require.Equal(t, req.SeparationRatio.GetBasePpm(), ratio.GetBasePpm())
			require.Equal(t, req.SeparationRatio.GetBurnPpm(), ratio.GetBurnPpm())
			require.Equal(t, req.SeparationRatio.GetValidatorsPpm(), ratio.GetValidatorsPpm())
		})
	}
}
