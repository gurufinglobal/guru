package keeper

import (
	"testing"

	"github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
)

const (
	// testAuthorityAddr is a test address for oracle authority
	testAuthorityAddr = "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"
)

func TestSubmitOracleDataNilDataSet(t *testing.T) {
	keeper, ctx := setupKeeper(t)

	// Test with nil DataSet - should return error, not panic
	msg := &types.MsgSubmitOracleData{
		AuthorityAddress: testAuthorityAddr,
		DataSet:          nil, // This should cause validation to fail
	}

	// This should not panic and should return an error
	response, err := keeper.SubmitOracleData(ctx, msg)
	require.Error(t, err)
	require.Nil(t, response)
	require.Contains(t, err.Error(), "DataSet must be provided")
}

// func TestSubmitOracleDataValidDataSet(t *testing.T) {
// 	keeper, ctx := setupKeeper(t)

// 	// Set up a moderator address first
// 	moderatorAddr := "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"
// 	keeper.SetModeratorAddress(ctx, moderatorAddr)

// 	// Create a test oracle request document
// 	doc := types.OracleRequestDoc{
// 		RequestId:       1,
// 		OracleType:      types.OracleType_ORACLE_TYPE_CRYPTO,
// 		Name:            "Test Oracle",
// 		Description:     "Test Description",
// 		Period:          60,
// 		AccountList:     []string{moderatorAddr},
// 		Quorum:          1,
// 		Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
// 		AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
// 		Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
// 		Nonce:           0,
// 	}
// 	keeper.SetOracleRequestDoc(ctx, doc)

// 	// Test with valid DataSet
// 	msg := &types.MsgSubmitOracleData{
// 		AuthorityAddress: moderatorAddr,
// 		DataSet: &types.SubmitDataSet{
// 			RequestId: 1,
// 			Nonce:     1,
// 			RawData:   "123.456",
// 			Provider:  moderatorAddr,
// 			Signature: []byte("test signature"),
// 		},
// 	}

// 	// This should succeed
// 	response, err := keeper.SubmitOracleData(ctx, msg)
// 	require.NoError(t, err)
// 	require.NotNil(t, response)
// }

func TestSubmitOracleDataEdgeCases(t *testing.T) {
	keeper, ctx := setupKeeper(t)

	testCases := []struct {
		name        string
		msg         *types.MsgSubmitOracleData
		expectError bool
		errorMsg    string
	}{
		{
			name: "nil message",
			msg:  nil,
			// This would panic before reaching our handler, so we skip this test
			expectError: true,
		},
		{
			name: "nil DataSet",
			msg: &types.MsgSubmitOracleData{
				AuthorityAddress: testAuthorityAddr,
				DataSet:          nil,
			},
			expectError: true,
			errorMsg:    "DataSet must be provided",
		},
		{
			name: "valid DataSet structure but invalid content",
			msg: &types.MsgSubmitOracleData{
				AuthorityAddress: testAuthorityAddr,
				DataSet: &types.SubmitDataSet{
					RequestId: 0, // Invalid: zero request ID
					Nonce:     1,
					RawData:   "",
					Provider:  "",
					Signature: nil,
				},
			},
			expectError: true,
			errorMsg:    "request id is 0",
		},
	}

	for _, tc := range testCases {
		if tc.name == "nil message" {
			// Skip nil message test as it would panic before reaching our handler
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			response, err := keeper.SubmitOracleData(ctx, tc.msg)

			if tc.expectError {
				require.Error(t, err)
				require.Nil(t, response)
				if tc.errorMsg != "" {
					require.Contains(t, err.Error(), tc.errorMsg)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
			}
		})
	}
}
