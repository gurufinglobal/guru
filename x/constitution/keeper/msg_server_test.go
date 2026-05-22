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
				req.Authority = f.keeper.authority.String()
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
