package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func setupTest(t *testing.T) (sdk.Context, *Keeper) {
	t.Helper()
	keeper, ctx := setupKeeper(t)
	return ctx, keeper
}

func TestAggregateData(t *testing.T) {
	tests := []struct {
		name       string
		rule       types.AggregationRule
		submitData []*types.SubmitDataSet
		want       string
		wantErr    bool
	}{
		{
			name: "average_aggregation",
			rule: types.AggregationRule_AGGREGATION_RULE_AVG,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "15.5"},
				{RawData: "20.5"},
			},
			want:    "15.5",
			wantErr: false,
		},
		{
			name: "min_aggregation",
			rule: types.AggregationRule_AGGREGATION_RULE_MIN,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "15.5"},
				{RawData: "20.5"},
			},
			want:    "10.5",
			wantErr: false,
		},
		{
			name: "max_aggregation",
			rule: types.AggregationRule_AGGREGATION_RULE_MAX,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "15.5"},
				{RawData: "20.5"},
			},
			want:    "20.5",
			wantErr: false,
		},
		{
			name: "median_aggregation_odd",
			rule: types.AggregationRule_AGGREGATION_RULE_MEDIAN,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "15.5"},
				{RawData: "20.5"},
			},
			want:    "15.5",
			wantErr: false,
		},
		{
			name: "median_aggregation_even",
			rule: types.AggregationRule_AGGREGATION_RULE_MEDIAN,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "15.5"},
				{RawData: "20.5"},
				{RawData: "25.5"},
			},
			want:    "18",
			wantErr: false,
		},
		{
			name:       "empty_data_set",
			rule:       types.AggregationRule_AGGREGATION_RULE_AVG,
			submitData: []*types.SubmitDataSet{},
			want:       "",
			wantErr:    true,
		},
		{
			name: "unsupported_aggregation_rule",
			rule: types.AggregationRule_AGGREGATION_RULE_UNSPECIFIED,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
			},
			want:    "",
			wantErr: true,
		},
		// Test cases for invalid decimal numbers
		{
			name: "invalid_decimal_average",
			rule: types.AggregationRule_AGGREGATION_RULE_AVG,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "abc"}, // Invalid decimal
				{RawData: "20.5"},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "invalid_decimal_min",
			rule: types.AggregationRule_AGGREGATION_RULE_MIN,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "12,34"}, // Invalid decimal (comma instead of dot)
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "invalid_decimal_max",
			rule: types.AggregationRule_AGGREGATION_RULE_MAX,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "$15.50"}, // Invalid decimal (currency symbol)
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "invalid_decimal_median",
			rule: types.AggregationRule_AGGREGATION_RULE_MEDIAN,
			submitData: []*types.SubmitDataSet{
				{RawData: "10.5"},
				{RawData: "15.5"},
				{RawData: "NaN"}, // Invalid decimal
			},
			want:    "",
			wantErr: true,
		},
	}

	ctx, k := setupTest(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := k.AggregateData(ctx, tt.rule, tt.submitData)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

// TestProcessOracleDataSetAggregation disabled temporarily due to store setup issues
func TestProcessOracleDataSetAggregation(t *testing.T) {
	ctx, k := setupTest(t)

	// Create test oracle request document
	doc := types.OracleRequestDoc{
		RequestId:       1,
		OracleType:      types.OracleType_ORACLE_TYPE_CRYPTO,
		Name:            "Test Oracle",
		Description:     "Test Description",
		Period:          60,
		AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft", "guru172m263wcg98cph8e32tv0xky94eq4r4wzhwgaz"},
		Quorum:          2,
		Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
		AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
		Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
	}

	// Store the document
	k.SetOracleRequestDoc(ctx, doc)

	// Submit test data
	submitData1 := types.SubmitDataSet{
		RequestId: 1,
		Nonce:     1,
		RawData:   "10.5",
		Provider:  "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
	}
	submitData2 := types.SubmitDataSet{
		RequestId: 1,
		Nonce:     1,
		RawData:   "15.5",
		Provider:  "guru172m263wcg98cph8e32tv0xky94eq4r4wzhwgaz",
	}

	k.SetSubmitData(ctx, submitData1)
	k.SetSubmitData(ctx, submitData2)

	// Process aggregation
	k.ProcessOracleDataSetAggregation(ctx)

	// Verify the result
	dataSet, err := k.GetDataSet(ctx, 1, 1)
	require.NoError(t, err)
	require.Equal(t, "13", dataSet.RawData) // Average of 10.5 and 15.5

	// Verify nonce increment
	updatedDoc, err := k.GetOracleRequestDoc(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), updatedDoc.Nonce) // 0 -> 1
}

// TestProcessOracleDataSetAggregationWithInsufficientQuorum disabled temporarily due to store setup issues
func TestProcessOracleDataSetAggregationWithInsufficientQuorum(t *testing.T) {
	ctx, k := setupTest(t)

	// Create test oracle request document with quorum 2
	doc := types.OracleRequestDoc{
		RequestId:       1,
		OracleType:      types.OracleType_ORACLE_TYPE_CRYPTO,
		Name:            "Test Oracle",
		Description:     "Test Description",
		Period:          60,
		AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft", "guru172m263wcg98cph8e32tv0xky94eq4r4wzhwgaz"},
		Quorum:          2,
		Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
		AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
		Status:          types.RequestStatus_REQUEST_STATUS_ENABLED,
	}

	// Store the document
	k.SetOracleRequestDoc(ctx, doc)

	// Submit only one data point (insufficient for quorum)
	submitData := types.SubmitDataSet{
		RequestId: 1,
		Nonce:     1,
		RawData:   "10.5",
		Provider:  "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
	}

	k.SetSubmitData(ctx, submitData)

	// Process aggregation
	k.ProcessOracleDataSetAggregation(ctx)

	// Verify no data set was created
	_, err := k.GetDataSet(ctx, 1, 1)
	require.Error(t, err)

	// Verify nonce was not incremented
	updatedDoc, err := k.GetOracleRequestDoc(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(0), updatedDoc.Nonce)
}

// TestProcessOracleDataSetAggregationWithDisabledStatus disabled temporarily due to store setup issues
func TestProcessOracleDataSetAggregationWithDisabledStatus(t *testing.T) {
	ctx, k := setupTest(t)

	// Create test oracle request document with disabled status
	doc := types.OracleRequestDoc{
		RequestId:       1,
		OracleType:      types.OracleType_ORACLE_TYPE_CRYPTO,
		Name:            "Test Oracle",
		Description:     "Test Description",
		Period:          60,
		AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft", "guru172m263wcg98cph8e32tv0xky94eq4r4wzhwgaz"},
		Quorum:          1,
		Endpoints:       []*types.OracleEndpoint{{Url: "http://test.com", ParseRule: "test"}},
		AggregationRule: types.AggregationRule_AGGREGATION_RULE_AVG,
		Status:          types.RequestStatus_REQUEST_STATUS_DISABLED,
	}

	// Store the document
	k.SetOracleRequestDoc(ctx, doc)

	// Submit test data
	submitData := types.SubmitDataSet{
		RequestId: 1,
		Nonce:     1,
		RawData:   "10.5",
		Provider:  "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
	}

	k.SetSubmitData(ctx, submitData)

	// Process aggregation
	k.ProcessOracleDataSetAggregation(ctx)

	// Verify no data set was created
	_, err := k.GetDataSet(ctx, 1, 1)
	require.Error(t, err)

	// Verify nonce was not incremented
	updatedDoc, err := k.GetOracleRequestDoc(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(0), updatedDoc.Nonce)
}
