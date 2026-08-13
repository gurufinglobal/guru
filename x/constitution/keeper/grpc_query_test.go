package keeper

import (
	"testing"

	constitutionv1 "github.com/gurufinglobal/guru/v2/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestQueryServerParams(t *testing.T) {
	tests := []struct {
		name          string
		withInitParam bool
		request       *constitutionv1.QueryParamsRequest
		shouldErr     bool
	}{
		{"returns params on nil request", true, nil, false},
		{"returns params on non-nil request", true, &constitutionv1.QueryParamsRequest{}, false},
		{"fails when params are missing", false, &constitutionv1.QueryParamsRequest{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f keeperTestFixture
			if tc.withInitParam {
				f = setupKeeperFixture(t)
			} else {
				f = setupKeeperFixtureWithoutParams(t)
			}

			queryServer := NewQueryServer(&f.keeper)
			resp, err := queryServer.Params(f.ctx, tc.request)
			if tc.shouldErr {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "10", resp.GetParams().GetMinValidatorBondAmount().Amount.String())
		})
	}
}

func TestQueryServerBaseAddress(t *testing.T) {
	tests := []struct {
		name              string
		withInitializedKV bool
		request           *constitutionv1.QueryBaseAddressRequest
		shouldErr         bool
	}{
		{"returns base address on nil request", true, nil, false},
		{"returns base address on non-nil request", true, &constitutionv1.QueryBaseAddressRequest{}, false},
		{"fails when base address is missing", false, &constitutionv1.QueryBaseAddressRequest{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f keeperTestFixture
			if tc.withInitializedKV {
				f = setupKeeperFixture(t)
			} else {
				f = setupKeeperFixtureWithoutParams(t)
			}

			queryServer := NewQueryServer(&f.keeper)
			resp, err := queryServer.BaseAddress(f.ctx, tc.request)
			if tc.shouldErr {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, f.baseAddress, resp.GetBaseAddress())
		})
	}
}

func TestQueryServerModeratorAddress(t *testing.T) {
	tests := []struct {
		name              string
		withInitializedKV bool
		request           *constitutionv1.QueryModeratorAddressRequest
		shouldErr         bool
	}{
		{"returns moderator address on nil request", true, nil, false},
		{"returns moderator address on non-nil request", true, &constitutionv1.QueryModeratorAddressRequest{}, false},
		{"fails when moderator address is missing", false, &constitutionv1.QueryModeratorAddressRequest{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f keeperTestFixture
			if tc.withInitializedKV {
				f = setupKeeperFixture(t)
			} else {
				f = setupKeeperFixtureWithoutParams(t)
			}

			queryServer := NewQueryServer(&f.keeper)
			resp, err := queryServer.ModeratorAddress(f.ctx, tc.request)
			if tc.shouldErr {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, f.moderatorAddress, resp.GetModeratorAddress())
		})
	}
}

func TestQueryServerSeparationRatio(t *testing.T) {
	tests := []struct {
		name              string
		withInitializedKV bool
		request           *constitutionv1.QuerySeparationRatioRequest
		shouldErr         bool
	}{
		{"returns separation ratio on nil request", true, nil, false},
		{"returns separation ratio on non-nil request", true, &constitutionv1.QuerySeparationRatioRequest{}, false},
		{"fails when separation ratio is missing", false, &constitutionv1.QuerySeparationRatioRequest{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f keeperTestFixture
			if tc.withInitializedKV {
				f = setupKeeperFixture(t)
			} else {
				f = setupKeeperFixtureWithoutParams(t)
			}

			queryServer := NewQueryServer(&f.keeper)
			resp, err := queryServer.SeparationRatio(f.ctx, tc.request)
			if tc.shouldErr {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, uint32(200_000), resp.GetSeparationRatio().GetBasePpm())
			require.Equal(t, uint32(300_000), resp.GetSeparationRatio().GetBurnPpm())
			require.Equal(t, uint32(500_000), resp.GetSeparationRatio().GetValidatorsPpm())
		})
	}
}
