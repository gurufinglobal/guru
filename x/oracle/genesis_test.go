package oracle

import (
	"testing"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	"github.com/stretchr/testify/require"
)

func TestValidateGenesisRejectsDuplicateTaskSymbols(t *testing.T) {
	err := (AppModule{}).validateGenesisState(&oraclev1.GenesisState{
		Params: oraclekeeper.DefaultParams(),
		Tasks: []*oraclev1.OracleTask{
			{
				Symbol:             "BTC/USD",
				ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 1,
			},
			{
				Symbol:             " btc/usd ",
				ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 1,
			},
		},
	})
	require.Error(t, err)
}
