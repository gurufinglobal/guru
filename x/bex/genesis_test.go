package bex_test

import (
	"testing"
	"time"

	"github.com/GPTx-global/guru-v2/testutil/constants"
	"github.com/GPTx-global/guru-v2/x/bex"
	"github.com/GPTx-global/guru-v2/x/bex/types"

	exampleapp "github.com/GPTx-global/guru-v2/gurud"
	utiltx "github.com/GPTx-global/guru-v2/testutil/tx"
	"github.com/cometbft/cometbft/crypto/tmhash"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/suite"
)

type GenesisTestSuite struct {
	suite.Suite
	ctx     sdk.Context
	app     *exampleapp.EVMD
	genesis types.GenesisState
}

func TestGenesisTestSuite(t *testing.T) {
	suite.Run(t, new(GenesisTestSuite))
}

func (suite *GenesisTestSuite) SetupTest() {
	// consensus key
	consAddress := sdk.ConsAddress(utiltx.GenerateAddress().Bytes())

	chainID := constants.ExampleChainID
	suite.app = exampleapp.Setup(suite.T(), chainID.ChainID, chainID.EVMChainID)
	suite.ctx = suite.app.NewContextLegacy(false, tmproto.Header{
		Height:          1,
		ChainID:         chainID.ChainID,
		Time:            time.Now().UTC(),
		ProposerAddress: consAddress.Bytes(),

		Version: tmversion.Consensus{
			Block: version.BlockProtocol,
		},
		LastBlockId: tmproto.BlockID{
			Hash: tmhash.Sum([]byte("block_id")),
			PartSetHeader: tmproto.PartSetHeader{
				Total: 11,
				Hash:  tmhash.Sum([]byte("partset_header")),
			},
		},
		AppHash:            tmhash.Sum([]byte("app")),
		DataHash:           tmhash.Sum([]byte("data")),
		EvidenceHash:       tmhash.Sum([]byte("evidence")),
		ValidatorsHash:     tmhash.Sum([]byte("validators")),
		NextValidatorsHash: tmhash.Sum([]byte("next_validators")),
		ConsensusHash:      tmhash.Sum([]byte("consensus")),
		LastResultsHash:    tmhash.Sum([]byte("last_result")),
	})

	suite.genesis = *types.DefaultGenesisState()
}

func (suite *GenesisTestSuite) TestInitGenesis() {
	// ctx, k := setupTest(suite.T())

	tests := []struct {
		name     string
		genesis  types.GenesisState
		expPanic bool
	}{
		{
			name: "1. valid genesis state",
			genesis: types.GenesisState{
				ModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
				Exchanges:        []types.Exchange{},
				Ratemeter:        types.DefaultRatemeter(),
			},
			expPanic: false,
		},
		{
			name: "2. empty moderator address",
			genesis: types.GenesisState{
				ModeratorAddress: "",
				Exchanges:        []types.Exchange{},
				Ratemeter:        types.DefaultRatemeter(),
			},
			expPanic: false,
		},
		// {
		// 	name: "3. invalid ratemeter",
		// 	genesis: types.GenesisState{
		// 		ModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		// 		Exchanges:        []types.Exchange{},
		// 		Ratemeter: types.Ratemeter{
		// 			RequestCountLimit: 0,
		// 			RequestPeriod:     0,
		// 		},
		// 	},
		// 	expPanic: true,
		// },
		// {
		// 	name: "4. invalid exchange",
		// 	genesis: types.GenesisState{
		// 		ModeratorAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		// 		Exchanges: []types.Exchange{
		// 			{
		// 				Id:             math.NewInt(1),
		// 				AdminAddress:   "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		// 				ReserveAddress: "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft",
		// 				DenomA:         "guru",
		// 				DenomB:         "guru",
		// 				IbcDenomA:      "guru",
		// 				IbcDenomB:      "guru",
		// 				PortA:          "guru",
		// 				ChannelA:       "guru",
		// 				PortB:          "guru",
		// 				ChannelB:       "guru",
		// 				Rate:           math.LegacyNewDec(0),
		// 				Fee:            math.LegacyNewDec(0),
		// 				Limit:          math.LegacyNewDec(0),
		// 				Status:         types.ExchangeStatusActive,
		// 			},
		// 		},
		// 		Ratemeter: types.DefaultRatemeter(),
		// 	},
		// 	expPanic: true,
		// },
	}

	for _, tc := range tests {
		suite.Run(tc.name, func() {
			if tc.expPanic {
				suite.Require().Panics(func() {
					bex.InitGenesis(suite.ctx, suite.app.BexKeeper, tc.genesis)
				})
			} else {
				suite.Require().NotPanics(func() {
					bex.InitGenesis(suite.ctx, suite.app.BexKeeper, tc.genesis)
				})
			}
		})
	}
}
