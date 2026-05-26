package keeper

import (
	"testing"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	"github.com/stretchr/testify/require"
)

func TestMsgServerUpdateParams(t *testing.T) {
	tests := []struct {
		name          string
		withInitParam bool
		request       *constitutionv1.MsgUpdateParams
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
			request: &constitutionv1.MsgUpdateParams{
				Authority: "invalid-authority",
				Params:    testParams("12"),
			},
			shouldErr: true,
		},
		{
			name:          "fails on invalid params",
			withInitParam: true,
			request: &constitutionv1.MsgUpdateParams{
				Params: testParams("0"),
			},
			shouldErr: true,
		},
		{
			name:          "updates params even when current params are missing",
			withInitParam: false,
			request: &constitutionv1.MsgUpdateParams{
				Params: testParams("12"),
			},
			shouldErr: false,
		},
		{
			name:          "updates params successfully",
			withInitParam: true,
			request: &constitutionv1.MsgUpdateParams{
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

			var req *constitutionv1.MsgUpdateParams
			if tc.request != nil {
				req = &constitutionv1.MsgUpdateParams{
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
			require.Equal(t, "12", params.GetMinValidatorBondAmount().Amount)
		})
	}
}

func TestMsgServerUpdateBaseAddress(t *testing.T) {
	tests := []struct {
		name      string
		request   *constitutionv1.MsgUpdateBaseAddress
		shouldErr bool
	}{
		{
			name:      "fails on nil request",
			request:   nil,
			shouldErr: true,
		},
		{
			name: "fails on invalid moderator",
			request: &constitutionv1.MsgUpdateBaseAddress{
				Moderator:   "invalid-moderator",
				BaseAddress: "invalid-base",
			},
			shouldErr: true,
		},
		{
			name: "fails on invalid base address",
			request: &constitutionv1.MsgUpdateBaseAddress{
				BaseAddress: "invalid-base",
			},
			shouldErr: true,
		},
		{
			name: "fails when base address equals authority",
			request: &constitutionv1.MsgUpdateBaseAddress{
				BaseAddress: "",
			},
			shouldErr: true,
		},
		{
			name: "updates base address successfully",
			request: &constitutionv1.MsgUpdateBaseAddress{
				BaseAddress: "",
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			var req *constitutionv1.MsgUpdateBaseAddress
			if tc.request != nil {
				req = &constitutionv1.MsgUpdateBaseAddress{
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

func TestMsgServerUpdateModeratorAddress(t *testing.T) {
	tests := []struct {
		name      string
		request   *constitutionv1.MsgUpdateModeratorAddress
		shouldErr bool
	}{
		{
			name:      "fails on nil request",
			request:   nil,
			shouldErr: true,
		},
		{
			name: "fails on invalid moderator",
			request: &constitutionv1.MsgUpdateModeratorAddress{
				Moderator:        "invalid-moderator",
				ModeratorAddress: "invalid-address",
			},
			shouldErr: true,
		},
		{
			name: "fails on invalid new moderator address",
			request: &constitutionv1.MsgUpdateModeratorAddress{
				ModeratorAddress: "invalid-address",
			},
			shouldErr: true,
		},
		{
			name: "fails when moderator address equals authority",
			request: &constitutionv1.MsgUpdateModeratorAddress{
				ModeratorAddress: "",
			},
			shouldErr: true,
		},
		{
			name: "updates moderator address successfully",
			request: &constitutionv1.MsgUpdateModeratorAddress{
				ModeratorAddress: "",
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			var req *constitutionv1.MsgUpdateModeratorAddress
			if tc.request != nil {
				req = &constitutionv1.MsgUpdateModeratorAddress{
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
	_, err := msgServer.UpdateModeratorAddress(f.ctx, &constitutionv1.MsgUpdateModeratorAddress{
		Moderator:        f.moderatorAddress,
		ModeratorAddress: newModerator,
	})
	require.NoError(t, err)

	_, err = msgServer.UpdateBaseAddress(f.ctx, &constitutionv1.MsgUpdateBaseAddress{
		Moderator:   f.moderatorAddress,
		BaseAddress: testAddress(t, f.keeper.accountCodec, 0x07),
	})
	require.Error(t, err)

	_, err = msgServer.UpdateBaseAddress(f.ctx, &constitutionv1.MsgUpdateBaseAddress{
		Moderator:   newModerator,
		BaseAddress: testAddress(t, f.keeper.accountCodec, 0x07),
	})
	require.NoError(t, err)
}

func TestMsgServerUpdateSeparationRatio(t *testing.T) {
	tests := []struct {
		name      string
		request   *constitutionv1.MsgUpdateSeparationRatio
		shouldErr bool
	}{
		{
			name:      "fails on nil request",
			request:   nil,
			shouldErr: true,
		},
		{
			name: "fails on invalid moderator",
			request: &constitutionv1.MsgUpdateSeparationRatio{
				Moderator:       "invalid-moderator",
				SeparationRatio: testSeparationRatio(200_000, 300_000, 500_000),
			},
			shouldErr: true,
		},
		{
			name: "fails on invalid separation ratio",
			request: &constitutionv1.MsgUpdateSeparationRatio{
				SeparationRatio: testSeparationRatio(200_000, 300_000, 400_000),
			},
			shouldErr: true,
		},
		{
			name: "updates separation ratio successfully",
			request: &constitutionv1.MsgUpdateSeparationRatio{
				SeparationRatio: testSeparationRatio(100_000, 200_000, 700_000),
			},
			shouldErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupKeeperFixture(t)
			msgServer := NewMsgServer(&f.keeper)

			var req *constitutionv1.MsgUpdateSeparationRatio
			if tc.request != nil {
				req = &constitutionv1.MsgUpdateSeparationRatio{
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
