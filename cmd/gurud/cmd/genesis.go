package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/input"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/go-bip39"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v2/app"
	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

// newInitCommand creates the CometBFT node files and Guru's default genesis in
// one pass. Network values are command inputs; the compiled values are defaults.
func newInitCommand(defaultHome string) *cobra.Command {
	command := &cobra.Command{
		Use:   "init [moniker]",
		Short: "Initialize validator, P2P, application, and genesis files",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			clientContext := client.GetClientContextFromCmd(command)
			serverContext := server.GetServerContextFromCmd(command)
			cometConfig := serverContext.Config
			cometConfig.SetRoot(clientContext.HomeDir)

			chainID, err := command.Flags().GetString(flags.FlagChainID)
			if err != nil {
				return err
			}
			if chainID == "" {
				chainID = chainconfig.DefaultChainID
			}
			defaultDenom, err := command.Flags().GetString(genutilcli.FlagDefaultBondDenom)
			if err != nil {
				return err
			}
			if defaultDenom == "" {
				defaultDenom = chainconfig.BaseDenom
			}
			constitutionBaseAddress, err := command.Flags().GetString("constitution-base-address")
			if err != nil {
				return err
			}
			if constitutionBaseAddress == "" {
				return fmt.Errorf("--constitution-base-address is required")
			}
			constitutionModeratorAddress, err := command.Flags().GetString("constitution-moderator-address")
			if err != nil {
				return err
			}
			if constitutionModeratorAddress == "" {
				return fmt.Errorf("--constitution-moderator-address is required")
			}

			var mnemonic string
			recoverKey, err := command.Flags().GetBool(genutilcli.FlagRecover)
			if err != nil {
				return err
			}
			if recoverKey {
				mnemonic, err = input.GetString(
					"Enter your bip39 mnemonic",
					bufio.NewReader(command.InOrStdin()),
				)
				if err != nil {
					return err
				}
				if !bip39.IsMnemonicValid(mnemonic) {
					return fmt.Errorf("invalid mnemonic")
				}
			}

			initialHeight, err := command.Flags().GetInt64(flags.FlagInitHeight)
			if err != nil {
				return err
			}
			if initialHeight < 1 {
				initialHeight = 1
			}
			nodeID, _, err := genutil.InitializeNodeValidatorFilesFromMnemonic(cometConfig, mnemonic)
			if err != nil {
				return fmt.Errorf("initialize validator files: %w", err)
			}
			cometConfig.Moniker = args[0]
			genesisPath := cometConfig.GenesisFile()
			overwrite, err := command.Flags().GetBool(genutilcli.FlagOverwrite)
			if err != nil {
				return err
			}
			if _, err := os.Stat(genesisPath); err == nil && !overwrite {
				return fmt.Errorf("genesis.json file already exists: %s", genesisPath)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}

			application, err := app.New(app.Options{
				Logger:     log.NewNopLogger(),
				DB:         dbm.NewMemDB(),
				HomePath:   clientContext.HomeDir,
				ChainID:    chainID,
				EVMChainID: chainconfig.DefaultEVMChainID,
			})
			if err != nil {
				return fmt.Errorf("construct application genesis: %w", err)
			}
			defer application.Close() //nolint:errcheck
			appState := application.DefaultGenesis()
			if err := application.ConfigureConstitutionGenesis(
				appState,
				constitutionBaseAddress,
				constitutionModeratorAddress,
			); err != nil {
				return err
			}
			stakingGenesis := stakingtypes.DefaultGenesisState()
			application.AppCodec().MustUnmarshalJSON(
				appState[stakingtypes.ModuleName],
				stakingGenesis,
			)
			stakingGenesis.Params.BondDenom = defaultDenom
			appState[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(stakingGenesis)
			if err := application.ValidateGenesis(appState); err != nil {
				return fmt.Errorf("validate default Guru genesis: %w", err)
			}
			consensusParams := cmttypes.DefaultConsensusParams()
			consensusParams.ABCI.VoteExtensionsEnableHeight = 0
			appStateJSON, err := json.MarshalIndent(appState, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal Guru genesis: %w", err)
			}

			consensusKey, err := command.Flags().GetString(genutilcli.FlagConsensusKeyAlgo)
			if err != nil {
				return err
			}
			genesis := &genutiltypes.AppGenesis{
				AppName:       version.AppName,
				AppVersion:    version.Version,
				ChainID:       chainID,
				InitialHeight: initialHeight,
				AppState:      appStateJSON,
				Consensus:     &genutiltypes.ConsensusGenesis{Params: consensusParams},
			}
			genesis.Consensus.Params.Validator.PubKeyTypes = []string{consensusKey}
			if err := genutil.ExportGenesisFile(genesis, genesisPath); err != nil {
				return fmt.Errorf("write Guru genesis: %w", err)
			}
			cmtcfg.WriteConfigFile(filepath.Join(cometConfig.RootDir, "config", "config.toml"), cometConfig)

			return json.NewEncoder(command.OutOrStdout()).Encode(struct {
				Moniker string `json:"moniker"`
				ChainID string `json:"chain_id"`
				NodeID  string `json:"node_id"`
				Home    string `json:"home"`
			}{
				Moniker: cometConfig.Moniker,
				ChainID: chainID,
				NodeID:  nodeID,
				Home:    clientContext.HomeDir,
			})
		},
	}
	command.Flags().String(flags.FlagHome, defaultHome, "node's home directory")
	command.Flags().BoolP(genutilcli.FlagOverwrite, "o", false, "overwrite an existing genesis file")
	command.Flags().Bool(genutilcli.FlagRecover, false, "recover the validator key from a seed phrase")
	command.Flags().String(flags.FlagChainID, chainconfig.DefaultChainID, "genesis chain ID")
	command.Flags().String(genutilcli.FlagDefaultBondDenom, chainconfig.BaseDenom, "native staking denomination")
	command.Flags().String("constitution-base-address", "", "Constitution fee base recipient address")
	command.Flags().String("constitution-moderator-address", "", "Constitution policy moderator address")
	command.Flags().Int64(flags.FlagInitHeight, 1, "initial block height")
	command.Flags().String(genutilcli.FlagConsensusKeyAlgo, ed25519.KeyType, "consensus key algorithm")
	return command
}

func newGenesisCommand(txConfig client.TxConfig, defaultHome string) *cobra.Command {
	command := genutilcli.Commands(txConfig, app.NewBasicManager(), defaultHome)
	for _, child := range command.Commands() {
		if child.Name() == "validate" {
			command.RemoveCommand(child)
		}
	}
	command.AddCommand(newValidateGenesisCommand())
	return command
}

func newValidateGenesisCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "validate [file]",
		Aliases: []string{"validate-genesis"},
		Args:    cobra.RangeArgs(0, 1),
		Short:   "Validate the CometBFT document and Guru application genesis",
		RunE: func(command *cobra.Command, args []string) error {
			serverContext := server.GetServerContextFromCmd(command)
			clientContext := client.GetClientContextFromCmd(command)
			genesisPath := serverContext.Config.GenesisFile()
			if len(args) == 1 {
				genesisPath = args[0]
			}

			genesis, err := genutiltypes.AppGenesisFromFile(genesisPath)
			if err != nil {
				return fmt.Errorf("read genesis %q: %w", genesisPath, err)
			}
			if err := genesis.ValidateAndComplete(); err != nil {
				return fmt.Errorf("validate CometBFT genesis: %w", err)
			}
			var appState app.GenesisState
			if err := json.Unmarshal(genesis.AppState, &appState); err != nil {
				return fmt.Errorf("decode application genesis: %w", err)
			}
			application, err := app.New(app.Options{
				Logger:     log.NewNopLogger(),
				DB:         dbm.NewMemDB(),
				HomePath:   clientContext.HomeDir,
				ChainID:    genesis.ChainID,
				EVMChainID: chainconfig.DefaultEVMChainID,
			})
			if err != nil {
				return fmt.Errorf("construct genesis validator: %w", err)
			}
			defer application.Close() //nolint:errcheck
			err = application.ValidateGenesis(appState)
			if err != nil {
				return fmt.Errorf("validate Guru application genesis: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "File at %s is a valid Guru genesis file\n", genesisPath)
			return err
		},
	}
	return command
}
