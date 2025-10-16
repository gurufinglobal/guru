package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func init() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("guru", "gurupub")
}

func TestMsgRegisterOracleRequestDoc(t *testing.T) {
	validMsg := MsgRegisterOracleRequestDoc{
		ModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		RequestDoc: OracleRequestDoc{
			Name:            "Test Request",
			OracleType:      OracleType_ORACLE_TYPE_CRYPTO,
			Endpoints:       []*OracleEndpoint{{Url: "https://api.coinbase.com/v2/prices/BTC-USD/spot", ParseRule: "data.amount"}},
			AggregationRule: AggregationRule_AGGREGATION_RULE_AVG,
			AccountList:     []string{"guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft"},
			Quorum:          1,
			Status:          RequestStatus_REQUEST_STATUS_ENABLED,
		},
	}
	require.NoError(t, validMsg.ValidateBasic())

	invalidMsg := MsgRegisterOracleRequestDoc{
		ModeratorAddress: "invalid-address",
		RequestDoc:       validMsg.RequestDoc,
	}
	require.Error(t, invalidMsg.ValidateBasic())
}

func TestMsgSubmitOracleData(t *testing.T) {
	// Valid message with valid decimal RawData
	validMsg := MsgSubmitOracleData{
		AuthorityAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		DataSet: &SubmitDataSet{
			RequestId: 1,
			Nonce:     1,
			RawData:   "123.456",
			Provider:  "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
			Signature: []byte("test signature"),
		},
	}
	require.NoError(t, validMsg.ValidateBasic())

	// Test various valid decimal formats
	validDecimals := []string{
		"123",
		"123.456",
		"0.123",
		".123",
		"123.",
		"0",
		"0.0",
		"-123.456",
		"+123.456",
		"1e10",
		"1.23e-4",
		"1.23E+5",
	}

	for _, decimal := range validDecimals {
		msg := MsgSubmitOracleData{
			AuthorityAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
			DataSet: &SubmitDataSet{
				RequestId: 1,
				Nonce:     1,
				RawData:   decimal,
				Provider:  "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				Signature: []byte("test signature"),
			},
		}
		require.NoError(t, msg.ValidateBasic(), "Expected %s to be valid decimal", decimal)
	}

	// Test invalid decimal formats
	invalidDecimals := []string{
		"abc",
		"123abc",
		"12.34.56",
		"12,34",
		"12 34",
		"$123",
		"123%",
		"infinity",
		"NaN",
		"",
	}

	for _, decimal := range invalidDecimals {
		msg := MsgSubmitOracleData{
			AuthorityAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
			DataSet: &SubmitDataSet{
				RequestId: 1,
				Nonce:     1,
				RawData:   decimal,
				Provider:  "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				Signature: []byte("test signature"),
			},
		}
		require.Error(t, msg.ValidateBasic(), "Expected %s to be invalid decimal", decimal)
	}

	// Test other validation errors
	invalidMsg := MsgSubmitOracleData{
		AuthorityAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		DataSet: &SubmitDataSet{
			RequestId: 0, // Invalid: zero request ID
			RawData:   "123.456",
			Provider:  "invalid-address", // Invalid: bad address format
			Signature: nil,               // Invalid: empty signature
		},
	}
	require.Error(t, invalidMsg.ValidateBasic())
}

func TestMsgUpdateModeratorAddress(t *testing.T) {
	validMsg := MsgUpdateModeratorAddress{
		ModeratorAddress:    "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		NewModeratorAddress: "guru133vfz58wdptepx460hl3s0sg9emep4tjjyvwap",
	}
	require.NoError(t, validMsg.ValidateBasic())

	// invalid moderator address: same as new moderator address
	invalidMsg := MsgUpdateModeratorAddress{
		ModeratorAddress:    "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		NewModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
	}
	require.Error(t, invalidMsg.ValidateBasic())

	// invalid new moderator address: empty
	invalidMsg2 := MsgUpdateModeratorAddress{
		ModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
	}
	require.Error(t, invalidMsg2.ValidateBasic())

	// invalid new moderator address: invalid bech32 address(ModeratorAddress)
	invalidMsg3 := MsgUpdateModeratorAddress{
		ModeratorAddress:    "h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		NewModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
	}
	require.Error(t, invalidMsg3.ValidateBasic())

	// invalid new moderator address: invalid bech32 address(NewModeratorAddress)
	invalidMsg4 := MsgUpdateModeratorAddress{
		ModeratorAddress:    "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		NewModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zk",
	}
	require.Error(t, invalidMsg4.ValidateBasic())
}
