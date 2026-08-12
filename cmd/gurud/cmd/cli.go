package cmd

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/x/evidence"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantcli "cosmossdk.io/x/feegrant/client/cli"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/upgrade"
	upgradecli "cosmossdk.io/x/upgrade/client/cli"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	auth "github.com/cosmos/cosmos-sdk/x/auth"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingcli "github.com/cosmos/cosmos-sdk/x/auth/vesting/client/cli"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzcli "github.com/cosmos/cosmos-sdk/x/authz/client/cli"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	bank "github.com/cosmos/cosmos-sdk/x/bank"
	bankcli "github.com/cosmos/cosmos-sdk/x/bank/client/cli"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution"
	distrcli "github.com/cosmos/cosmos-sdk/x/distribution/client/cli"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/mint"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking"
	stakingcli "github.com/cosmos/cosmos-sdk/x/staking/client/cli"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/cosmos/evm/x/erc20"
	evmcli "github.com/cosmos/evm/x/vm/client/cli"
	transfer "github.com/cosmos/ibc-go/v10/modules/apps/transfer"
	"github.com/spf13/cobra"

	guruapp "github.com/gurufinglobal/guru/v2/app"
	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

func newQueryCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	command.AddCommand(
		rpc.QueryEventForTxCmd(),
		rpc.ValidatorCommand(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
		sdkserver.QueryBlockCmd(),
		sdkserver.QueryBlockResultsCmd(),
	)
	// Cosmos EVM, FeeMarket, and IBC core still publish hand-written query
	// commands rather than AutoCLI descriptors. The stateless basic manager
	// adds those commands without constructing an App or consuming EVM globals.
	guruapp.NewBasicManager().AddQueryCommands(command)
	command.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return command
}

func newTxCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	command.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
	)
	// Install the hand-written command roots before AutoCLI augments them. The
	// application BasicManager intentionally stores zero-value module basics;
	// using it here would leave several tx commands with a nil address codec.
	accountCodec := evmaddress.NewEvmCodec(chainconfig.Bech32PrefixAccAddr)
	validatorCodec := evmaddress.NewEvmCodec(chainconfig.Bech32PrefixValAddr)
	command.AddCommand(
		bankcli.NewTxCmd(accountCodec),
		stakingcli.NewTxCmd(validatorCodec, accountCodec),
		distrcli.NewTxCmd(validatorCodec, accountCodec),
		evmcli.NewTxCmd(accountCodec),
		gov.NewAppModuleBasic(nil).GetTxCmd(),
		authzcli.GetTxCmd(accountCodec),
		feegrantcli.GetTxCmd(accountCodec),
		upgradecli.GetTxCmd(accountCodec),
		vestingcli.GetTxCmd(accountCodec),
		erc20.AppModuleBasic{}.GetTxCmd(),
		transfer.AppModuleBasic{}.GetTxCmd(),
	)
	command.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return command
}

// enhanceAutoCLI adds the operator-facing module commands without
// constructing an App. Their descriptors are stateless, while the live gRPC
// connection is resolved from the selected command's client context.
func enhanceAutoCLI(rootCommand *cobra.Command, clientCtx client.Context) error {
	moduleOptions := map[string]*autocliv1.ModuleOptions{
		authtypes.ModuleName:           setGovProposalCommandsSkipped((auth.AppModule{}).AutoCLIOptions()),
		banktypes.ModuleName:           setGovProposalCommandsSkipped((bank.AppModule{}).AutoCLIOptions()),
		stakingtypes.ModuleName:        setGovProposalCommandsSkipped((staking.AppModule{}).AutoCLIOptions()),
		minttypes.ModuleName:           setGovProposalCommandsSkipped((mint.AppModule{}).AutoCLIOptions()),
		distrtypes.ModuleName:          setGovProposalCommandsSkipped((distribution.AppModule{}).AutoCLIOptions()),
		slashingtypes.ModuleName:       setGovProposalCommandsSkipped((slashing.AppModule{}).AutoCLIOptions()),
		govtypes.ModuleName:            setGovProposalCommandsSkipped((gov.AppModule{}).AutoCLIOptions()),
		consensusparamtypes.ModuleName: setGovProposalCommandsSkipped((consensus.AppModule{}).AutoCLIOptions()),
		authz.ModuleName:               setGovProposalCommandsSkipped((authzmodule.AppModule{}).AutoCLIOptions()),
		feegrant.ModuleName:            setGovProposalCommandsSkipped((feegrantmodule.AppModule{}).AutoCLIOptions()),
		upgradetypes.ModuleName:        setGovProposalCommandsSkipped((upgrade.AppModule{}).AutoCLIOptions()),
		evidencetypes.ModuleName:       setGovProposalCommandsSkipped((evidence.AppModule{}).AutoCLIOptions()),
		vestingtypes.ModuleName:        setGovProposalCommandsSkipped((vesting.AppModule{}).AutoCLIOptions()),
	}
	accountCodec := evmaddress.NewEvmCodec(chainconfig.Bech32PrefixAccAddr)
	validatorCodec := evmaddress.NewEvmCodec(chainconfig.Bech32PrefixValAddr)
	consensusCodec := evmaddress.NewEvmCodec(chainconfig.Bech32PrefixConsAddr)
	return (autocli.AppOptions{
		ModuleOptions:         moduleOptions,
		AddressCodec:          accountCodec,
		ValidatorAddressCodec: validatorCodec,
		ConsensusAddressCodec: consensusCodec,
		ClientCtx:             clientCtx,
	}).EnhanceRootCommand(rootCommand)
}

// setGovProposalCommandsSkipped removes proposal pseudo-commands that the selected
// client/v2 beta does not yet wrap in MsgSubmitProposal. Leaving them visible
// would create direct authority messages that can never succeed for an
// operator key. Proposals remain available through `tx gov submit-proposal`.
func setGovProposalCommandsSkipped(options *autocliv1.ModuleOptions) *autocliv1.ModuleOptions {
	if options == nil || options.Tx == nil {
		return options
	}
	for _, rpc := range options.Tx.RpcCommandOptions {
		if rpc != nil && rpc.GovProposal {
			rpc.Skip = true
		}
	}
	return options
}
