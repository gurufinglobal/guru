package keeper

import (
	"testing"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
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
			require.Equal(t, "10", resp.GetParams().GetMinValidatorBondAmount().Amount)
		})
	}
}
