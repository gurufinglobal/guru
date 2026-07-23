package cmd

import (
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

func TestApplyConstitutionGenesisAddresses(t *testing.T) {
	minValidatorBond := sdk.NewInt64Coin(appparams.BaseDenom, 10)
	initialState := &constitutiontypes.GenesisState{
		Params: &constitutiontypes.Params{
			MinValidatorBondAmount: &minValidatorBond,
		},
		BaseAddress:      "",
		ModeratorAddress: "",
		SeparationRatio: &constitutiontypes.SeparationRatio{
			BasePpm:       100_000,
			BurnPpm:       200_000,
			ValidatorsPpm: 700_000,
		},
	}
	initialStateBz, err := json.Marshal(initialState)
	require.NoError(t, err)

	tests := []struct {
		name          string
		appGenesis    map[string]json.RawMessage
		baseAddress   string
		moderatorAddr string
		shouldErr     bool
	}{
		{
			name: "fails when base address is empty",
			appGenesis: map[string]json.RawMessage{
				constitutiontypes.ModuleName: initialStateBz,
			},
			baseAddress:   "",
			moderatorAddr: "guru1moderatormoderatormoderatormoderat0f9j9a",
			shouldErr:     true,
		},
		{
			name: "fails when moderator address is empty",
			appGenesis: map[string]json.RawMessage{
				constitutiontypes.ModuleName: initialStateBz,
			},
			baseAddress:   "guru1basebasebasebasebasebasebasebaseh6n2d8",
			moderatorAddr: "",
			shouldErr:     true,
		},
		{
			name:          "fails when constitution module genesis is missing",
			appGenesis:    map[string]json.RawMessage{},
			baseAddress:   "guru1basebasebasebasebasebasebasebaseh6n2d8",
			moderatorAddr: "guru1moderatormoderatormoderatormoderat0f9j9a",
			shouldErr:     true,
		},
		{
			name: "fails when constitution module genesis is invalid json",
			appGenesis: map[string]json.RawMessage{
				constitutiontypes.ModuleName: json.RawMessage(`{`),
			},
			baseAddress:   "guru1basebasebasebasebasebasebasebaseh6n2d8",
			moderatorAddr: "guru1moderatormoderatormoderatormoderat0f9j9a",
			shouldErr:     true,
		},
		{
			name: "updates constitution addresses successfully",
			appGenesis: map[string]json.RawMessage{
				constitutiontypes.ModuleName: initialStateBz,
			},
			baseAddress:   "guru1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq2mxvt8",
			moderatorAddr: "guru1qyqszqgpqyqszqgpqyqszqgpqyqszqgp0n3f5k",
			shouldErr:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := cloneGenesisMap(tc.appGenesis)

			err := applyConstitutionGenesisAddresses(genesis, tc.baseAddress, tc.moderatorAddr)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var updated constitutiontypes.GenesisState
			require.NoError(t, json.Unmarshal(genesis[constitutiontypes.ModuleName], &updated))
			require.Equal(t, tc.baseAddress, updated.GetBaseAddress())
			require.Equal(t, tc.moderatorAddr, updated.GetModeratorAddress())
			require.Equal(t, uint32(100_000), updated.GetSeparationRatio().GetBasePpm())
			require.Equal(t, uint32(200_000), updated.GetSeparationRatio().GetBurnPpm())
			require.Equal(t, uint32(700_000), updated.GetSeparationRatio().GetValidatorsPpm())
		})
	}
}

func cloneGenesisMap(input map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(input))
	for key, value := range input {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}
