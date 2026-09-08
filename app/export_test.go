package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/privval"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibccoretypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	"github.com/ethereum/go-ethereum/common"
	ethparams "github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

// Test-only key for an isolated loopback node. Never use it on a real network.
func exportRuntimeValidatorKey() cmted25519.PrivKey {
	return cmted25519.GenPrivKeyFromSecret([]byte("guru-export-restart-test-only"))
}

type exportRestartFixture struct {
	Export servertypes.ExportedApp
	Time   time.Time
}

func testExportAppRestart(t *testing.T, exported servertypes.ExportedApp, sourceTime time.Time) {
	t.Helper()
	dir := t.TempDir()
	// InitChain derives validators from staking genesis. Avoid encoding the
	// CometBFT PubKey interface with encoding/json in this private fixture.
	exported.Validators = nil
	raw, err := json.Marshal(exportRestartFixture{exported, sourceTime})
	require.NoError(t, err)
	path := filepath.Join(dir, "export.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestExportRestartProcess$", "-test.v")
	command.Env = append(os.Environ(), "GURU_EXPORT_RESTART_FIXTURE="+path)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Logf("fresh-process InitChain/blocks/time-based completion: %s", output)
}

// A separate process is necessary because Cosmos EVM seals process-global
// configuration. This test executes real InitChain and committed ABCI blocks,
// not a JSON-only validation or a replacement staking InitGenesis.
func TestExportRestartProcess(t *testing.T) {
	path := os.Getenv("GURU_EXPORT_RESTART_FIXTURE")
	if path == "" {
		t.Skip("subprocess helper")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var fixture exportRestartFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	application, err := New(Options{
		Logger: log.NewNopLogger(), DB: dbm.NewMemDB(), LoadLatest: true,
		HomePath: t.TempDir(), ChainID: "guru-test-1", EVMChainID: 9631,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, application.Close()) })
	initialHeight := fixture.Export.Height
	if initialHeight == 0 {
		initialHeight = 1
	}
	_, err = application.InitChain(&abci.RequestInitChain{
		ChainId: application.ChainID(), InitialHeight: initialHeight,
		Time: fixture.Time, AppStateBytes: fixture.Export.AppState,
		ConsensusParams: &fixture.Export.ConsensusParams,
	})
	require.NoError(t, err)
	// The fork records the current header hash at height % HistoryServeWindow.
	// Supply a nonzero hash to prove the cleared contract resumes recording.
	restartedHash := common.HexToHash("0x2935")
	_, err = application.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: initialHeight, Time: fixture.Time.Add(time.Second),
		ProposerAddress: exportRuntimeValidatorKey().PubKey().Address(), Hash: restartedHash.Bytes(),
	})
	require.NoError(t, err)
	_, err = application.Commit()
	require.NoError(t, err)
	ctx := committedContext(t, application)
	require.Equal(t, restartedHash, application.EVMKeeper.GetHeaderHash(ctx, uint64(initialHeight)))
	t.Logf("EIP-2935 rebuilt history height=%d hash=%s", initialHeight, restartedHash)
	params, err := application.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	stakingBefore := application.StakingKeeper.ExportGenesis(ctx)
	pending := stakingBefore.UnbondingDelegations
	withdrawals := make(map[string]sdkmath.Int)
	balancesBefore := make(map[string]sdkmath.Int)
	for _, ubd := range pending {
		address, err := application.AccountKeeper.AddressCodec().StringToBytes(ubd.DelegatorAddress)
		require.NoError(t, err)
		balancesBefore[ubd.DelegatorAddress] = application.BankKeeper.GetBalance(ctx, address, params.BondDenom).Amount
		if _, exists := withdrawals[ubd.DelegatorAddress]; !exists {
			withdrawals[ubd.DelegatorAddress] = sdkmath.ZeroInt()
		}
		for _, entry := range ubd.Entries {
			if entry.UnbondingOnHoldRefCount == 0 {
				withdrawals[ubd.DelegatorAddress] = withdrawals[ubd.DelegatorAddress].Add(entry.Balance)
			}
			// Held-state migration is outside this upstream-only restart contract.
			require.Zero(t, entry.UnbondingOnHoldRefCount)
		}
	}
	// Advance real block time beyond all fixture completion times.
	finalizeAndCommit(t, application, initialHeight+1,
		fixture.Time.Add(params.UnbondingTime+24*time.Hour),
		exportRuntimeValidatorKey().PubKey().Address(), nil)
	ctx = committedContext(t, application)
	for address, amount := range withdrawals {
		decoded, err := application.AccountKeeper.AddressCodec().StringToBytes(address)
		require.NoError(t, err)
		require.True(t, application.BankKeeper.GetBalance(ctx, decoded, params.BondDenom).Amount.Equal(
			balancesBefore[address].Add(amount)), "withdrawal balance did not arrive at %s", address)
	}
	stakingAfter := application.StakingKeeper.ExportGenesis(ctx)
	remaining := stakingAfter.UnbondingDelegations
	require.Empty(t, remaining, "unheld withdrawals did not complete")
	redelegations := stakingAfter.Redelegations
	require.Empty(t, redelegations, "unheld redelegations did not complete")
	// Check economics after upstream InitGenesis and actual end-block queues.
	result, err := application.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	var genesis GenesisState
	require.NoError(t, json.Unmarshal(result.AppState, &genesis))
	require.NoError(t, application.validateExportInvariants(genesis))
	t.Logf("initial_height=%d committed_height=%d pending_before=%d pending_after=%d app_hash=%X",
		initialHeight, application.LastBlockHeight(), len(pending), len(remaining), application.LastCommitID().Hash)
}

// Optional real gurud/CometBFT smoke, run only by the IDC runtime command.
// After restarting the source, export through the official CLI and restart a
// fresh node from that exact output. cliGenesis prevents recursive re-export.
func testExportNodeRestart(t *testing.T, exported servertypes.ExportedApp, sourceTime time.Time, cliGenesis ...string) {
	t.Helper()
	binary := os.Getenv("GURU_EXPORT_RUNTIME_BIN")
	if binary == "" {
		t.Log("real gurud restart NOT RUN: set GURU_EXPORT_RUNTIME_BIN via IDC runner")
		return
	}
	root := os.Getenv("GURU_EXPORT_RUNTIME_ARTIFACTS")
	require.NotEmpty(t, root, "runtime receipts require a persistent artifact directory")
	require.True(t, filepath.IsAbs(root))
	dir, err := os.MkdirTemp(root, fmt.Sprintf("height-%d-", exported.Height))
	require.NoError(t, err)
	home := filepath.Join(dir, "node")
	run := func(args ...string) []byte {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		return out
	}
	run("init", "export-restart", "--home", home, "--chain-id", "guru-test-1",
		"--constitution-base-address", sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		"--constitution-moderator-address", sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String())
	// Match the CLI's legacy optional-field completion for SDK App exports.
	consensus := exported.ConsensusParams
	if consensus.Version == nil {
		consensus.Version = &cmtproto.VersionParams{}
	}
	if consensus.Abci == nil {
		consensus.Abci = &cmtproto.ABCIParams{}
	}
	params := cmttypes.ConsensusParamsFromProto(consensus)
	doc := genutiltypes.AppGenesis{
		AppName: "gurud", ChainID: "guru-test-1", GenesisTime: sourceTime,
		InitialHeight: exported.Height, AppState: exported.AppState,
		Consensus: &genutiltypes.ConsensusGenesis{Params: &params, Validators: exported.Validators},
	}
	genesisPath := filepath.Join(home, "config", "genesis.json")
	require.NoError(t, doc.SaveAs(genesisPath))
	if len(cliGenesis) != 0 {
		raw, err := os.ReadFile(cliGenesis[0])
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(genesisPath, raw, 0o600))
	}
	pv := privval.NewFilePV(exportRuntimeValidatorKey(),
		filepath.Join(home, "config", "priv_validator_key.json"),
		filepath.Join(home, "data", "priv_validator_state.json"))
	pv.Save()
	configPath := filepath.Join(home, "config", "config.toml")
	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	configText := strings.ReplaceAll(string(configBytes), "timeout_commit = \"5s\"", "timeout_commit = \"200ms\"")
	require.NoError(t, os.WriteFile(configPath, []byte(configText), 0o600))
	appConfigPath := filepath.Join(home, "config", "app.toml")
	appConfig, err := os.ReadFile(appConfigPath)
	require.NoError(t, err)
	oracleOffset := strings.Index(string(appConfig), "[oracle]")
	require.NotEqual(t, -1, oracleOffset)
	appConfigText := string(appConfig[:oracleOffset]) + strings.Replace(string(appConfig[oracleOffset:]), "enabled = true", "enabled = false", 1)
	require.NoError(t, os.WriteFile(appConfigPath, []byte(appConfigText), 0o600))
	freeAddress := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		address := listener.Addr().String()
		require.NoError(t, listener.Close())
		return address
	}
	rpc, p2p := freeAddress(), freeAddress()
	client := &http.Client{Timeout: time.Second}
	status := func() (int64, string) {
		response, err := client.Get("http://" + rpc + "/status")
		if err != nil {
			return 0, ""
		}
		defer response.Body.Close()
		var payload struct {
			Result struct {
				SyncInfo struct {
					Height string `json:"latest_block_height"`
					Hash   string `json:"latest_app_hash"`
				} `json:"sync_info"`
			} `json:"result"`
		}
		if json.NewDecoder(response.Body).Decode(&payload) != nil {
			return 0, ""
		}
		height, _ := strconv.ParseInt(payload.Result.SyncInfo.Height, 10, 64)
		return height, payload.Result.SyncInfo.Hash
	}
	initialHeight := exported.Height
	if initialHeight == 0 {
		initialHeight = 1
	}
	var previousHeight int64
	for phase := 0; phase < 2; phase++ {
		logPath := filepath.Join(dir, fmt.Sprintf("node-%d.log", phase))
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		require.NoError(t, err)
		command := exec.Command(binary, "start", "--home", home,
			"--chain-id", "guru-test-1", "--evm.evm-chain-id", "9631",
			"--minimum-gas-prices", "0agxn", "--evm.min-tip", "0",
			"--rpc.laddr", "tcp://"+rpc, "--p2p.laddr", "tcp://"+p2p,
			"--grpc.enable=false", "--grpc-web.enable=false", "--api.enable=false",
			"--json-rpc.enable=false")
		command.Stdout, command.Stderr = logFile, logFile
		require.NoError(t, command.Start())
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = command.Process.Signal(os.Interrupt)
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				<-exited
			}
			_ = logFile.Close()
		}
		t.Cleanup(stop)
		target := initialHeight + 2
		if phase == 1 {
			target = previousHeight + 2
		}
		deadline := time.NewTimer(75 * time.Second)
		ticker := time.NewTicker(200 * time.Millisecond)
		var height int64
		var hash string
	wait:
		for {
			select {
			case err := <-exited:
				stopped = true
				_ = logFile.Close()
				logBytes, _ := os.ReadFile(logPath)
				t.Fatalf("node exited: %v\n%s", err, logBytes)
			case <-deadline.C:
				stop()
				logBytes, _ := os.ReadFile(logPath)
				t.Fatalf("node failed to reach height %d\n%s", target, logBytes)
			case <-ticker.C:
				height, hash = status()
				if height >= target {
					break wait
				}
			}
		}
		deadline.Stop()
		ticker.Stop()
		require.NotEmpty(t, hash)
		stop()
		previousHeight = height
		t.Logf("gurud runtime phase=%d height=%d app_hash=%s artifacts=%s", phase, height, hash, dir)
	}
	raw, err := os.ReadFile(genesisPath)
	require.NoError(t, err)
	fixtureKind := "application-export"
	if len(cliGenesis) != 0 {
		fixtureKind = "official-cli-zero-height-export"
	}
	receipt := fmt.Sprintf("fixture=%s\ninitial_height=%d\nrestart_height=%d\ngenesis_sha256=%x\n", fixtureKind, initialHeight, previousHeight, sha256.Sum256(raw))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "receipt.txt"), []byte(receipt), 0o600))
	if len(cliGenesis) != 0 {
		t.Logf("official CLI zero-height genesis restarted successfully: %s", dir)
		return
	}
	// Exercise the official command on the stopped node's committed database.
	outPath := filepath.Join(dir, "zero-height.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "export", "--home", home,
		"--for-zero-height", "--output-document", outPath).CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "official-cli-export.log"), out, 0o600))
	cliDoc, err := genutiltypes.AppGenesisFromFile(outPath)
	require.NoError(t, err)
	require.EqualValues(t, 0, cliDoc.InitialHeight)
	require.NotNil(t, cliDoc.Consensus)
	require.NotNil(t, cliDoc.Consensus.Params)
	testExportNodeRestart(t, servertypes.ExportedApp{
		AppState: cliDoc.AppState, Height: cliDoc.InitialHeight,
		Validators: cliDoc.Consensus.Validators, ConsensusParams: cliDoc.Consensus.Params.ToProto(),
	}, cliDoc.GenesisTime, outPath)
}

func TestZeroHeightStakingTargetValidatesHeightsWithoutBanningPendingEntries(t *testing.T) {
	state := &stakingtypes.GenesisState{
		Validators: []stakingtypes.Validator{{
			Status: stakingtypes.Unbonding, UnbondingIds: []uint64{12}, UnbondingOnHoldRefCount: 1,
		}},
		UnbondingDelegations: []stakingtypes.UnbondingDelegation{{
			Entries: []stakingtypes.UnbondingDelegationEntry{{UnbondingId: 13, UnbondingOnHoldRefCount: 1}},
		}},
		Redelegations: []stakingtypes.Redelegation{{
			Entries: []stakingtypes.RedelegationEntry{{UnbondingId: 14, UnbondingOnHoldRefCount: 1}},
		}},
	}
	require.NoError(t, validateZeroHeightStakingTarget(state))
	state.UnbondingDelegations[0].Entries[0].CreationHeight = 4
	require.ErrorContains(t, validateZeroHeightStakingTarget(state), "nonzero creation height 4")
	state.UnbondingDelegations[0].Entries[0].CreationHeight = 0
	state.Redelegations[0].Entries[0].CreationHeight = 5
	require.ErrorContains(t, validateZeroHeightStakingTarget(state), "nonzero creation height 5")
	state.Redelegations[0].Entries[0].CreationHeight = 0
	require.NoError(t, validateZeroHeightStakingTarget(state))
}

// Called from the single application harness: Cosmos EVM configuration is
// process-global. Every scenario uses a discarded cache of the committed app.
func testZeroHeightUpstreamStaking(
	t *testing.T,
	application *App,
	committed sdk.Context,
	consensusParams cmtproto.ConsensusParams,
) {
	newSource := func(t *testing.T) (sdk.Context, stakingtypes.Validator, sdk.ValAddress) {
		ctx, _ := committed.CacheContext()
		validators, err := application.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		require.Len(t, validators, 1)
		validator := validators[0]
		require.True(t, validator.IsBonded())
		operator, err := application.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.OperatorAddress)
		require.NoError(t, err)
		// Keep at least one voting-power unit after the pending withdrawals.
		additional := sdk.NewCoins(sdk.NewCoin(config.BaseDenom, sdk.DefaultPowerReduction))
		require.NoError(t, application.BankKeeper.MintCoins(ctx, minttypes.ModuleName, additional))
		require.NoError(t, application.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, sdk.AccAddress(operator), additional))
		_, err = application.StakingKeeper.Delegate(ctx, sdk.AccAddress(operator), sdk.DefaultPowerReduction, stakingtypes.Unbonded, validator, true)
		require.NoError(t, err)
		_, err = application.CustomStakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
		require.NoError(t, err)
		validator, err = application.StakingKeeper.GetValidator(ctx, sdk.ValAddress(operator))
		require.NoError(t, err)
		consensusAddress, err := validator.GetConsAddr()
		require.NoError(t, err)
		require.NoError(t, application.SlashingKeeper.SetValidatorSigningInfo(ctx, consensusAddress,
			slashingtypes.NewValidatorSigningInfo(consensusAddress, ctx.BlockHeight(), 0, time.Unix(0, 0), false, 0)))
		return ctx, validator, sdk.ValAddress(operator)
	}
	addDestination := func(t *testing.T, ctx sdk.Context) sdk.ValAddress {
		pubkey := ed25519.GenPrivKeyFromSecret([]byte("zero-height-upstream-destination")).PubKey()
		operator := sdk.ValAddress(pubkey.Address())
		encoded, err := application.StakingKeeper.ValidatorAddressCodec().BytesToString(operator)
		require.NoError(t, err)
		validator, err := stakingtypes.NewValidator(encoded, pubkey, stakingtypes.Description{})
		require.NoError(t, err)
		require.NoError(t, application.StakingKeeper.SetValidator(ctx, validator))
		require.NoError(t, application.StakingKeeper.SetValidatorByConsAddr(ctx, validator))
		require.NoError(t, application.StakingKeeper.SetValidatorByPowerIndex(ctx, validator))
		require.NoError(t, application.DistrKeeper.Hooks().AfterValidatorCreated(ctx, operator))
		return operator
	}
	export := func(t *testing.T, ctx sdk.Context, allowed []string) *stakingtypes.GenesisState {
		before, err := application.ModuleManager.ExportGenesisForModules(ctx, application.AppCodec(), nil)
		require.NoError(t, err)
		result, err := application.exportZeroHeightGenesis(ctx, allowed, consensusParams)
		require.NoError(t, err)
		repeated, err := application.exportZeroHeightGenesis(ctx, allowed, consensusParams)
		require.NoError(t, err)
		require.Equal(t, result, repeated)
		after, err := application.ModuleManager.ExportGenesisForModules(ctx, application.AppCodec(), nil)
		require.NoError(t, err)
		require.Equal(t, before, after, "export must not change its source cache")
		var genesis GenesisState
		require.NoError(t, json.Unmarshal(result.AppState, &genesis))
		require.NoError(t, application.ValidateGenesisAtHeight(genesis, 1))
		state := new(stakingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(genesis[stakingtypes.ModuleName], state))
		return state
	}

	t.Run("pending delegations retain upstream fields and completion queues", func(t *testing.T) {
		ctx, validator, operator := newSource(t)
		destination := addDestination(t, ctx)
		shares, err := validator.SharesFromTokens(sdkmath.NewInt(7))
		require.NoError(t, err)
		_, _, err = application.StakingKeeper.Undelegate(ctx, sdk.AccAddress(operator), operator, shares)
		require.NoError(t, err)
		_, err = application.StakingKeeper.BeginRedelegation(ctx, sdk.AccAddress(operator), operator, destination, shares)
		require.NoError(t, err)
		_, err = application.CustomStakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
		require.NoError(t, err)

		unbonding, err := application.StakingKeeper.GetUnbondingDelegation(ctx, sdk.AccAddress(operator), operator)
		require.NoError(t, err)
		redelegation, err := application.StakingKeeper.GetRedelegation(ctx, sdk.AccAddress(operator), operator, destination)
		require.NoError(t, err)
		require.Len(t, unbonding.Entries, 1)
		require.Len(t, redelegation.Entries, 1)
		require.Positive(t, unbonding.Entries[0].CreationHeight)
		require.Positive(t, redelegation.Entries[0].CreationHeight)

		state := export(t, ctx, nil)
		// Check the real upstream importer in a new process, including time-based
		// completion of ordinary unheld entries.
		restartCtx, _ := ctx.CacheContext()
		_, _, err = application.StakingKeeper.Undelegate(restartCtx, sdk.AccAddress(operator), operator, shares)
		require.NoError(t, err)
		_, err = application.CustomStakingKeeper.ApplyAndReturnValidatorSetUpdates(restartCtx)
		require.NoError(t, err)
		result, err := application.exportZeroHeightGenesis(restartCtx, nil, consensusParams)
		require.NoError(t, err)
		testExportAppRestart(t, result, ctx.BlockTime())
		unbonding.Entries[0].CreationHeight = 0
		redelegation.Entries[0].CreationHeight = 0
		require.Equal(t, []stakingtypes.UnbondingDelegation{unbonding}, state.UnbondingDelegations)
		require.Equal(t, []stakingtypes.Redelegation{redelegation}, state.Redelegations)

		// Inspect the transformed cache as well as JSON: creation-height resets
		// must leave the upstream completion-time queue entries in place.
		targetCtx, _ := ctx.CacheContext()
		receipt, witness, err := application.prepareZeroHeightState(targetCtx, nil)
		require.NoError(t, err)
		require.Equal(t, 1, receipt.UnbondingEntriesReset)
		require.Equal(t, 1, receipt.RedelegationEntriesReset)
		queue, err := application.StakingKeeper.GetUBDQueueTimeSlice(targetCtx, unbonding.Entries[0].CompletionTime)
		require.NoError(t, err)
		require.NotEmpty(t, queue)
		redQueue, err := application.StakingKeeper.GetRedelegationQueueTimeSlice(targetCtx, redelegation.Entries[0].CompletionTime)
		require.NoError(t, err)
		require.NotEmpty(t, redQueue)
		require.NotEmpty(t, witness.StakingValidators)

		// A timestamp edit is still rejected; using upstream does not authorize
		// changing a pending withdrawal's economic or completion-time fields.
		sourceGenesis, err := application.ModuleManager.ExportGenesisForModules(ctx, application.AppCodec(), nil)
		require.NoError(t, err)
		targetGenesis, err := application.ModuleManager.ExportGenesisForModules(targetCtx, application.AppCodec(), nil)
		require.NoError(t, err)
		require.NoError(t, application.validateZeroHeightStakingContinuity(
			GenesisState(sourceGenesis), GenesisState(targetGenesis), witness))
		state.UnbondingDelegations[0].Entries[0].CompletionTime = state.UnbondingDelegations[0].Entries[0].CompletionTime.Add(time.Second)
		targetGenesis[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
		require.ErrorContains(t, application.validateZeroHeightStakingContinuity(
			GenesisState(sourceGenesis), GenesisState(targetGenesis), witness),
			"staking state changed outside approved zero-height lifecycle fields")
	})

	t.Run("upstream can rebond an unbonding validator", func(t *testing.T) {
		ctx, validator, operator := newSource(t)
		consensusAddress, err := validator.GetConsAddr()
		require.NoError(t, err)
		require.NoError(t, application.StakingKeeper.Jail(ctx, consensusAddress))
		_, err = application.CustomStakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
		require.NoError(t, err)
		require.NoError(t, application.StakingKeeper.Unjail(ctx, consensusAddress))
		source, err := application.StakingKeeper.GetValidator(ctx, operator)
		require.NoError(t, err)
		require.True(t, source.IsUnbonding())
		require.NotEmpty(t, source.UnbondingIds)

		state := export(t, ctx, nil)
		require.Len(t, state.Validators, 1)
		expected := source
		expected.Status = stakingtypes.Bonded
		expected.UnbondingHeight = 0
		require.Equal(t, expected, state.Validators[0])
	})

	t.Run("nonzero historical counter does not block export", func(t *testing.T) {
		ctx, _, _ := newSource(t)
		id, err := application.StakingKeeper.IncrementUnbondingID(ctx)
		require.NoError(t, err)
		require.Positive(t, id)
		state := export(t, ctx, nil)
		require.Empty(t, state.UnbondingDelegations)
		require.Empty(t, state.Redelegations)
	})

	t.Run("allowlist jails exclusions and preserves existing jail", func(t *testing.T) {
		ctx, validator, operator := newSource(t)
		destination := addDestination(t, ctx)
		shares, err := validator.SharesFromTokens(sdkmath.NewInt(7))
		require.NoError(t, err)
		_, err = application.StakingKeeper.BeginRedelegation(ctx, sdk.AccAddress(operator), operator, destination, shares)
		require.NoError(t, err)
		_, err = application.CustomStakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
		require.NoError(t, err)
		dest, err := application.StakingKeeper.GetValidator(ctx, destination)
		require.NoError(t, err)
		destConsensus, err := dest.GetConsAddr()
		require.NoError(t, err)
		require.NoError(t, application.StakingKeeper.Jail(ctx, destConsensus))

		targetCtx, _ := ctx.CacheContext()
		_, witness, err := application.prepareZeroHeightState(targetCtx, []string{dest.OperatorAddress})
		require.NoError(t, err)
		excluded, err := application.StakingKeeper.GetValidator(targetCtx, operator)
		require.NoError(t, err)
		require.True(t, excluded.Jailed)
		require.True(t, excluded.IsUnbonding())
		require.Zero(t, excluded.UnbondingHeight)
		require.NotEmpty(t, excluded.UnbondingIds)
		params, err := application.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		require.Equal(t, ctx.BlockTime().Add(params.UnbondingTime), excluded.UnbondingTime)
		included, err := application.StakingKeeper.GetValidator(targetCtx, destination)
		require.NoError(t, err)
		require.True(t, included.Jailed, "allowlist must not unjail an already jailed validator")
		sourceGenesis, err := application.ModuleManager.ExportGenesisForModules(ctx, application.AppCodec(), nil)
		require.NoError(t, err)
		targetGenesis, err := application.ModuleManager.ExportGenesisForModules(targetCtx, application.AppCodec(), nil)
		require.NoError(t, err)
		require.NoError(t, application.validateZeroHeightStakingContinuity(
			GenesisState(sourceGenesis), GenesisState(targetGenesis), witness))
		require.NoError(t, application.validateExportInvariants(GenesisState(targetGenesis)))
	})
}

func testApplicationExport(t *testing.T, application *App, blockTime time.Time, committedConsensusParams cmtproto.ConsensusParams, applicationLog *bytes.Buffer, cosmosSender sdk.AccAddress) {
	exported, err := application.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(5), exported.Height)
	require.NotNil(t, exported.ConsensusParams.Abci)
	require.Equal(t, int64(1), exported.ConsensusParams.Abci.VoteExtensionsEnableHeight)
	var exportedGenesis GenesisState
	require.NoError(t, json.Unmarshal(exported.AppState, &exportedGenesis))
	require.NoError(t, application.ValidateGenesisAtHeight(exportedGenesis, exported.Height))
	t.Run("normal export restarts in a fresh process", func(t *testing.T) {
		testExportAppRestart(t, exported, blockTime)
		testExportNodeRestart(t, exported, blockTime)
	})
	t.Run("staking export invariant includes active unbonding balances", func(t *testing.T) {
		fixture := cloneGenesisState(exportedGenesis)
		stakingFixture := new(stakingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[stakingtypes.ModuleName],
			stakingFixture,
		))
		require.NotEmpty(t, stakingFixture.Validators)
		unbondingBalance := sdkmath.NewInt(7)
		stakingFixture.UnbondingDelegations = append(
			stakingFixture.UnbondingDelegations,
			stakingtypes.UnbondingDelegation{
				DelegatorAddress: cosmosSender.String(),
				ValidatorAddress: stakingFixture.Validators[0].OperatorAddress,
				Entries: []stakingtypes.UnbondingDelegationEntry{{
					CreationHeight: 4,
					CompletionTime: blockTime.Add(24 * time.Hour),
					InitialBalance: unbondingBalance,
					Balance:        unbondingBalance,
					UnbondingId:    1,
				}},
			},
		)
		fixture[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(stakingFixture)

		bankFixture := new(banktypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[banktypes.ModuleName],
			bankFixture,
		))
		notBondedPoolAddress := authtypes.NewModuleAddress(stakingtypes.NotBondedPoolName).String()
		foundNotBondedPool := false
		for i := range bankFixture.Balances {
			if bankFixture.Balances[i].Address == notBondedPoolAddress {
				bankFixture.Balances[i].Coins = bankFixture.Balances[i].Coins.Add(
					sdk.NewCoin(config.BaseDenom, unbondingBalance),
				)
				foundNotBondedPool = true
			}
		}
		if !foundNotBondedPool {
			bankFixture.Balances = append(bankFixture.Balances, banktypes.Balance{
				Address: notBondedPoolAddress,
				Coins:   sdk.NewCoins(sdk.NewCoin(config.BaseDenom, unbondingBalance)),
			})
		}
		fixture[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankFixture)
		require.NoError(t, application.validateStakingExportInvariants(fixture))

		// SDK InitGenesis accounts for pending withdrawals even when their
		// original validator record has already been removed.
		removedOperator, err := application.StakingKeeper.ValidatorAddressCodec().BytesToString(
			bytes.Repeat([]byte{0x6b}, 20),
		)
		require.NoError(t, err)
		stakingFixture.UnbondingDelegations[0].ValidatorAddress = removedOperator
		fixture[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(stakingFixture)
		require.NoError(t, application.validateStakingExportInvariants(fixture))

		bankFixture.Balances = slices.DeleteFunc(bankFixture.Balances, func(balance banktypes.Balance) bool {
			return balance.Address == notBondedPoolAddress
		})
		fixture[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankFixture)
		require.ErrorContains(
			t,
			application.validateStakingExportInvariants(fixture),
			"not-bonded pool balance",
		)
	})
	t.Run("staking export invariant rejects extra pool denominations", func(t *testing.T) {
		fixture := cloneGenesisState(exportedGenesis)
		bankFixture := new(banktypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[banktypes.ModuleName],
			bankFixture,
		))
		bondedPoolAddress := authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String()
		found := false
		for i := range bankFixture.Balances {
			if bankFixture.Balances[i].Address == bondedPoolAddress {
				bankFixture.Balances[i].Coins = bankFixture.Balances[i].Coins.Add(
					sdk.NewInt64Coin("unexpected", 1),
				)
				found = true
			}
		}
		require.True(t, found)
		fixture[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankFixture)
		require.ErrorContains(
			t,
			application.validateStakingExportInvariants(fixture),
			"bonded pool balance",
		)
	})
	t.Run("distribution export invariant rejects extra module denominations", func(t *testing.T) {
		fixture := cloneGenesisState(exportedGenesis)
		bankFixture := new(banktypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[banktypes.ModuleName],
			bankFixture,
		))
		distributionAddress := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
		found := false
		for i := range bankFixture.Balances {
			if bankFixture.Balances[i].Address == distributionAddress {
				bankFixture.Balances[i].Coins = bankFixture.Balances[i].Coins.Add(
					sdk.NewInt64Coin("unexpected", 1),
				)
				found = true
			}
		}
		if !found {
			bankFixture.Balances = append(bankFixture.Balances, banktypes.Balance{
				Address: distributionAddress,
				Coins:   sdk.NewCoins(sdk.NewInt64Coin("unexpected", 1)),
			})
		}
		fixture[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankFixture)
		require.ErrorContains(
			t,
			application.validateDistributionExportInvariants(fixture),
			"module balance",
		)
	})

	_, err = application.ExportAppStateAndValidators(false, []string{"invalid"}, nil)
	require.ErrorContains(t, err, "jail allowlist requires zero-height export")
	_, err = application.ExportAppStateAndValidators(true, nil, []string{banktypes.ModuleName})
	require.ErrorContains(t, err, "zero-height export requires a complete module export")
	_, err = application.ExportAppStateAndValidators(true, []string{"invalid"}, nil)
	require.Error(t, err)
	_, err = application.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)

	var validatedZeroGenesis GenesisState
	t.Run("internal zero-height pipeline is isolated and emits bootable application state", func(t *testing.T) {
		exportCtx, err := application.newExportContext(true)
		require.NoError(t, err)
		require.Equal(t, committedContext(t, application).BlockTime(), exportCtx.BlockTime())
		require.Equal(t, application.LastBlockHeight(), exportCtx.BlockHeight())
		before, err := application.ExportAppStateAndValidators(false, nil, nil)
		require.NoError(t, err)

		// Export real, nonempty EIP-2935 history without altering the source fixture.
		sourceCtx, _ := committedContext(t, application).CacheContext()
		historyKeys := make([]common.Hash, 0)
		application.EVMKeeper.ForEachStorage(
			sourceCtx,
			ethparams.HistoryStorageAddress,
			func(key, _ common.Hash) bool {
				historyKeys = append(historyKeys, key)
				return true
			},
		)
		require.NotEmpty(t, historyKeys)
		// The source fixture contains the signing state of a real bonded validator.
		validators, err := application.StakingKeeper.GetAllValidators(sourceCtx)
		require.NoError(t, err)
		for _, validator := range validators {
			if !validator.IsBonded() {
				continue
			}
			address, err := validator.GetConsAddr()
			require.NoError(t, err)
			_, err = application.SlashingKeeper.GetValidatorSigningInfo(sourceCtx, address)
			require.NoError(t, err)
		}

		zeroExport, err := application.exportZeroHeightGenesis(
			sourceCtx,
			nil,
			committedConsensusParams,
		)
		require.NoError(t, err)
		require.Equal(t, int64(0), zeroExport.Height)
		require.Equal(t, committedConsensusParams, zeroExport.ConsensusParams)
		var zeroGenesis GenesisState
		require.NoError(t, json.Unmarshal(zeroExport.AppState, &zeroGenesis))
		validatedZeroGenesis = cloneGenesisState(zeroGenesis)
		testExportAppRestart(t, zeroExport, blockTime)
		testExportNodeRestart(t, zeroExport, blockTime)
		require.NoError(t, application.ValidateGenesisAtHeight(
			zeroGenesis,
			zeroHeightEffectiveInitialHeight,
		))
		require.NoError(t, application.ValidateGenesisConsensusAtHeight(
			zeroGenesis,
			zeroHeightEffectiveInitialHeight,
			&committedConsensusParams,
		))
		require.NoError(t, application.validateExportInvariants(zeroGenesis))

		t.Run("preserved enabled task restarts with Oracle disabled", func(t *testing.T) {
			taskCtx, _ := sourceCtx.CacheContext()
			task := &oracletypes.OracleTask{
				Symbol: "BTC/USD", ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled: true, SubmissionInterval: 5,
			}
			require.NoError(t, application.OracleKeeper.SetTask(taskCtx, task))
			target, err := application.exportZeroHeightGenesis(taskCtx, nil, committedConsensusParams)
			require.NoError(t, err)
			var state GenesisState
			require.NoError(t, json.Unmarshal(target.AppState, &state))
			oracleState := new(oracletypes.GenesisState)
			require.NoError(t, application.AppCodec().UnmarshalJSON(state[oracletypes.ModuleName], oracleState))
			require.Contains(t, oracleState.Tasks, task)
			require.Empty(t, oracleState.TaskSchedule)
			require.Empty(t, oracleState.LatestValues)
			require.Empty(t, oracleState.History)
			// This is an explicit target launch choice, not source E continuity.
			target.ConsensusParams.Abci = &cmtproto.ABCIParams{}
			require.NoError(t, application.ValidateGenesisConsensusAtHeight(state, 1, &target.ConsensusParams))
			testExportAppRestart(t, target, blockTime)
			testExportNodeRestart(t, target, blockTime)
		})

		after, err := application.ExportAppStateAndValidators(false, nil, nil)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})

	t.Run("zero-height follows upstream staking lifecycle", func(t *testing.T) {
		testZeroHeightUpstreamStaking(t, application,
			committedContext(t, application).WithBlockTime(blockTime.Add(4*time.Second)),
			committedConsensusParams)
	})

	t.Run("zero-height continuity rejects unapproved module mutations", func(t *testing.T) {
		require.NotNil(t, validatedZeroGenesis)
		emptyWitness := zeroHeightMutationWitness{}
		require.NoError(t, application.validateZeroHeightEconomicContinuity(
			validatedZeroGenesis,
			cloneGenesisState(validatedZeroGenesis),
			emptyWitness,
		))

		mutatedAuth := cloneGenesisState(validatedZeroGenesis)
		mutatedAuthState := new(authtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			mutatedAuth[authtypes.ModuleName],
			mutatedAuthState,
		))
		mutatedAuthState.Params.MaxMemoCharacters++
		mutatedAuth[authtypes.ModuleName] = application.AppCodec().MustMarshalJSON(mutatedAuthState)
		require.ErrorContains(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				mutatedAuth,
				emptyWitness,
			),
			"auth params changed during zero-height export",
		)

		missingAuth := cloneGenesisState(validatedZeroGenesis)
		delete(missingAuth, authtypes.ModuleName)
		require.ErrorContains(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				missingAuth,
				emptyWitness,
			),
			`module "auth" disappeared during zero-height export`,
		)

		missingOracle := cloneGenesisState(validatedZeroGenesis)
		delete(missingOracle, oracletypes.ModuleName)
		require.ErrorContains(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				missingOracle,
				emptyWitness,
			),
			`module "oracle" disappeared during zero-height export`,
		)

		mutableOracle := cloneGenesisState(validatedZeroGenesis)
		mutableOracleState := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			mutableOracle[oracletypes.ModuleName],
			mutableOracleState,
		))
		mutableOracleState.TaskSchedule = nil
		mutableOracle[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			mutableOracleState,
		)
		require.NoError(t, application.validateZeroHeightEconomicContinuity(
			validatedZeroGenesis,
			mutableOracle,
			emptyWitness,
		))

		for _, tc := range []struct {
			name   string
			want   string
			mutate func(GenesisState)
		}{
			{
				name: "bank configuration",
				want: "bank configuration changed",
				mutate: func(target GenesisState) {
					state := new(banktypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[banktypes.ModuleName], state))
					state.Params.DefaultSendEnabled = !state.Params.DefaultSendEnabled
					target[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "distribution configuration",
				want: "distribution configuration or withdrawal routing changed",
				mutate: func(target GenesisState) {
					state := new(distrtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[distrtypes.ModuleName], state))
					state.Params.CommunityTax = sdkmath.LegacyOneDec()
					target[distrtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "distribution fractional reward ledger",
				want: "distribution decimal reward ledger changed",
				mutate: func(target GenesisState) {
					state := new(distrtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[distrtypes.ModuleName], state))
					state.FeePool.CommunityPool = state.FeePool.CommunityPool.Add(
						sdk.NewDecCoinFromDec(
							config.BaseDenom,
							sdkmath.LegacyMustNewDecFromStr("0.5"),
						),
					)
					target[distrtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "staking delegation",
				want: "staking state changed outside approved zero-height lifecycle fields",
				mutate: func(target GenesisState) {
					state := new(stakingtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], state))
					require.NotEmpty(t, state.Delegations)
					state.Delegations[0].Shares = state.Delegations[0].Shares.Add(sdkmath.LegacyOneDec())
					target[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "staking validator jail state",
				want: "jailed state",
				mutate: func(target GenesisState) {
					state := new(stakingtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], state))
					require.NotEmpty(t, state.Validators)
					state.Validators[0].Jailed = !state.Validators[0].Jailed
					target[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "staking validator completion time",
				want: "lifecycle changed without an upstream keeper result",
				mutate: func(target GenesisState) {
					state := new(stakingtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], state))
					require.NotEmpty(t, state.Validators)
					state.Validators[0].UnbondingTime = state.Validators[0].UnbondingTime.Add(time.Second)
					target[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "staking validator unbonding hold count",
				want: "staking state changed outside approved zero-height lifecycle fields",
				mutate: func(target GenesisState) {
					state := new(stakingtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], state))
					require.NotEmpty(t, state.Validators)
					state.Validators[0].UnbondingOnHoldRefCount++
					target[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "slashing configuration",
				want: "slashing state changed outside approved signing-window fields",
				mutate: func(target GenesisState) {
					state := new(slashingtypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[slashingtypes.ModuleName], state))
					state.Params.SignedBlocksWindow++
					target[slashingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "oracle params",
				want: "oracle params or task definitions changed",
				mutate: func(target GenesisState) {
					state := new(oracletypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[oracletypes.ModuleName], state))
					require.NotNil(t, state.Params)
					state.Params.HistoryLimit++
					target[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
			{
				name: "constitution base address",
				want: "constitution state changed outside pending minimum gas price",
				mutate: func(target GenesisState) {
					state := new(constitutiontypes.GenesisState)
					require.NoError(t, application.AppCodec().UnmarshalJSON(target[constitutiontypes.ModuleName], state))
					state.BaseAddress += "-mutated"
					target[constitutiontypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				target := cloneGenesisState(validatedZeroGenesis)
				tc.mutate(target)
				require.ErrorContains(
					t,
					application.validateZeroHeightEconomicContinuity(
						validatedZeroGenesis,
						target,
						emptyWitness,
					),
					tc.want,
				)
			})
		}

		tombstoneSource := cloneGenesisState(validatedZeroGenesis)
		tombstoneTarget := cloneGenesisState(validatedZeroGenesis)
		for _, fixture := range []GenesisState{tombstoneSource, tombstoneTarget} {
			state := new(slashingtypes.GenesisState)
			require.NoError(t, application.AppCodec().UnmarshalJSON(fixture[slashingtypes.ModuleName], state))
			state.SigningInfos = append(state.SigningInfos, slashingtypes.SigningInfo{
				Address: "synthetic-consensus-address",
				ValidatorSigningInfo: slashingtypes.ValidatorSigningInfo{
					Address:    "synthetic-consensus-address",
					Tombstoned: true,
				},
			})
			state.MissedBlocks = append(state.MissedBlocks, slashingtypes.ValidatorMissedBlocks{
				Address: "synthetic-consensus-address",
			})
			fixture[slashingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(state)
		}
		targetSlashing := new(slashingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			tombstoneTarget[slashingtypes.ModuleName],
			targetSlashing,
		))
		targetSlashing.SigningInfos[len(targetSlashing.SigningInfos)-1].ValidatorSigningInfo.Tombstoned = false
		tombstoneTarget[slashingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(targetSlashing)
		require.ErrorContains(
			t,
			application.validateZeroHeightEconomicContinuity(
				tombstoneSource,
				tombstoneTarget,
				emptyWitness,
			),
			"slashing state changed outside approved signing-window fields",
		)

		unauthorizedBalanceMove := cloneGenesisState(validatedZeroGenesis)
		bankState := new(banktypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			unauthorizedBalanceMove[banktypes.ModuleName],
			bankState,
		))
		movedCoin := sdk.NewInt64Coin(config.BaseDenom, 1)
		debited := false
		for i := range bankState.Balances {
			if bankState.Balances[i].Coins.AmountOf(config.BaseDenom).IsPositive() {
				bankState.Balances[i].Coins = bankState.Balances[i].Coins.Sub(movedCoin)
				debited = true
				break
			}
		}
		require.True(t, debited)
		unauthorizedAddress := sdk.AccAddress(bytes.Repeat([]byte{0x7f}, 20)).String()
		bankState.Balances = append(bankState.Balances, banktypes.Balance{
			Address: unauthorizedAddress,
			Coins:   sdk.NewCoins(movedCoin),
		})
		unauthorizedBalanceMove[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankState)
		require.ErrorContains(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				unauthorizedBalanceMove,
				emptyWitness,
			),
			"bank balance for",
		)

		t.Run("recorded payout may create only its exact base account", func(t *testing.T) {
			source := cloneGenesisState(validatedZeroGenesis)
			target := cloneGenesisState(validatedZeroGenesis)
			payoutCoin := sdk.NewInt64Coin(config.BaseDenom, 1)
			payout := sdk.NewCoins(payoutCoin)
			newAddressBytes := sdk.AccAddress(bytes.Repeat([]byte{0x6a}, 20))
			newAddress, err := application.AccountKeeper.AddressCodec().BytesToString(newAddressBytes)
			require.NoError(t, err)
			distributionAddress, err := application.AccountKeeper.AddressCodec().BytesToString(
				authtypes.NewModuleAddress(distrtypes.ModuleName),
			)
			require.NoError(t, err)

			sourceBank := new(banktypes.GenesisState)
			require.NoError(t, application.AppCodec().UnmarshalJSON(
				source[banktypes.ModuleName],
				sourceBank,
			))
			debited := false
			for i := range sourceBank.Balances {
				balance := &sourceBank.Balances[i]
				if balance.Address != distributionAddress &&
					balance.Coins.AmountOf(config.BaseDenom).IsPositive() {
					balance.Coins = balance.Coins.Sub(payoutCoin)
					debited = true
					break
				}
			}
			require.True(t, debited)
			distributionFound := false
			for i := range sourceBank.Balances {
				if sourceBank.Balances[i].Address == distributionAddress {
					sourceBank.Balances[i].Coins = sourceBank.Balances[i].Coins.Add(payoutCoin)
					distributionFound = true
					break
				}
			}
			if !distributionFound {
				sourceBank.Balances = append(sourceBank.Balances, banktypes.Balance{
					Address: distributionAddress,
					Coins:   payout,
				})
			}
			source[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(sourceBank)

			targetBank := new(banktypes.GenesisState)
			require.NoError(t, application.AppCodec().UnmarshalJSON(
				source[banktypes.ModuleName],
				targetBank,
			))
			for i := range targetBank.Balances {
				if targetBank.Balances[i].Address == distributionAddress {
					targetBank.Balances[i].Coins = targetBank.Balances[i].Coins.Sub(payoutCoin)
					break
				}
			}
			targetBank.Balances = append(targetBank.Balances, banktypes.Balance{
				Address: newAddress,
				Coins:   payout,
			})
			target[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(targetBank)

			sourceDistribution := new(distrtypes.GenesisState)
			require.NoError(t, application.AppCodec().UnmarshalJSON(
				source[distrtypes.ModuleName],
				sourceDistribution,
			))
			sourceDistribution.FeePool.CommunityPool = sourceDistribution.FeePool.CommunityPool.Add(
				sdk.NewDecCoin(config.BaseDenom, sdkmath.OneInt()),
			)
			source[distrtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
				sourceDistribution,
			)

			targetAuth := new(authtypes.GenesisState)
			require.NoError(t, application.AppCodec().UnmarshalJSON(
				target[authtypes.ModuleName],
				targetAuth,
			))
			accounts, err := authtypes.UnpackAccounts(targetAuth.Accounts)
			require.NoError(t, err)
			nextAccountNumber := uint64(0)
			for _, account := range accounts {
				if account.GetAccountNumber() >= nextAccountNumber {
					nextAccountNumber = account.GetAccountNumber() + 1
				}
			}
			newAccount := authtypes.NewBaseAccount(
				newAddressBytes,
				nil,
				nextAccountNumber,
				0,
			)
			newAccountAny, err := codectypes.NewAnyWithValue(newAccount)
			require.NoError(t, err)
			targetAuth.Accounts = append(targetAuth.Accounts, newAccountAny)
			target[authtypes.ModuleName] = application.AppCodec().MustMarshalJSON(targetAuth)

			witness := zeroHeightMutationWitness{
				RewardPayouts:     map[string]sdk.Coins{newAddress: payout},
				NewRewardAccounts: []string{newAddress},
			}
			require.NoError(t, application.validateZeroHeightEconomicContinuity(
				source,
				target,
				witness,
			))

			newAccount.Sequence = 1
			newAccountAny, err = codectypes.NewAnyWithValue(newAccount)
			require.NoError(t, err)
			targetAuth.Accounts[len(targetAuth.Accounts)-1] = newAccountAny
			target[authtypes.ModuleName] = application.AppCodec().MustMarshalJSON(targetAuth)
			require.ErrorContains(t, application.validateZeroHeightEconomicContinuity(
				source,
				target,
				witness,
			), "invalid base-account state")
		})

		introducedModules := cloneGenesisState(validatedZeroGenesis)
		introducedModules["zzz-future-module"] = json.RawMessage(`{}`)
		introducedModules["aaa-future-module"] = json.RawMessage(`{}`)
		require.EqualError(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				introducedModules,
				emptyWitness,
			),
			`zero-height export introduced modules ["aaa-future-module" "zzz-future-module"]`,
		)

		malformedBank := cloneGenesisState(validatedZeroGenesis)
		malformedBank[banktypes.ModuleName] = json.RawMessage(`{`)
		require.EqualError(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				malformedBank,
				emptyWitness,
			),
			`target module "bank" genesis is invalid JSON`,
		)

		nullAuth := cloneGenesisState(validatedZeroGenesis)
		nullAuth[authtypes.ModuleName] = json.RawMessage(`null`)
		require.EqualError(
			t,
			application.validateZeroHeightEconomicContinuity(
				validatedZeroGenesis,
				nullAuth,
				emptyWitness,
			),
			`target module "auth" genesis must be a JSON object`,
		)

		missingBankSource := cloneGenesisState(validatedZeroGenesis)
		missingBankTarget := cloneGenesisState(validatedZeroGenesis)
		delete(missingBankSource, banktypes.ModuleName)
		delete(missingBankTarget, banktypes.ModuleName)
		require.EqualError(
			t,
			application.validateZeroHeightEconomicContinuity(
				missingBankSource,
				missingBankTarget,
				emptyWitness,
			),
			"source bank genesis is missing",
		)
	})

	t.Run("zero-height Guru state transformation is deterministic and explicit", func(t *testing.T) {
		fixture := cloneGenesisState(exportedGenesis)

		oracleFixture := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[oracletypes.ModuleName],
			oracleFixture,
		))
		originalOracleParams := oracleFixture.Params
		oracleFixture.Tasks = []*oracletypes.OracleTask{
			{
				Symbol:             "ZZZ/USD",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 5,
			},
			{
				Symbol:             " aaa/usd ",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            true,
				SubmissionInterval: 3,
			},
			{
				Symbol:             "DISABLED/USD",
				ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Enabled:            false,
				SubmissionInterval: 7,
			},
		}
		oracleFixture.TaskSchedule = []*oracletypes.OracleTaskScheduleEntry{
			{Symbol: "ZZZ/USD", Height: 100},
		}
		observed := &oracletypes.OracleValue{
			Symbol:        "ZZZ/USD",
			ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:         "1.25",
			BlockHeight:   4,
			BlockTimeUnix: blockTime.Unix(),
		}
		oracleFixture.LatestValues = []*oracletypes.OracleValue{observed}
		oracleFixture.History = []*oracletypes.OracleHistory{{
			Symbol: "ZZZ/USD",
			Values: []*oracletypes.OracleValue{observed},
		}}
		fixture[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(oracleFixture)

		constitutionFixture := new(constitutiontypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[constitutiontypes.ModuleName],
			constitutionFixture,
		))
		constitutionFixture.PendingMinGasPrice = &constitutiontypes.MinGasPriceSchedule{
			EffectiveHeight:                20,
			ScheduledMinGasPrice:           "0.5",
			SourceSymbol:                   "ZZZ/USD",
			SourceValue:                    "0.5",
			SourceOracleHeight:             4,
			SourceSubmissionIntervalBlocks: 5,
			PendingDelayBlocks:             16,
			PendingDelayCapBlocks:          20,
			RawMinGasPrice:                 "0.5",
			PreviousMinGasPrice:            "1.0",
		}
		fixture[constitutiontypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			constitutionFixture,
		)

		originalEVM := slices.Clone(fixture[evmtypes.ModuleName])
		receipt := zeroHeightExportReceipt{}
		require.NoError(t, application.transformZeroHeightGenesis(fixture, &receipt))
		// This helper rewrites only Guru-owned genesis documents. The full
		// export path applies the SDK staking/slashing state transition first.
		require.NoError(t, application.ValidateGenesis(fixture))
		require.Equal(t, 3, receipt.OracleTasksPreserved)
		require.Equal(t, 1, receipt.OracleSchedulesRemoved)
		require.Equal(t, 1, receipt.OracleLatestValuesRemoved)
		require.Equal(t, 1, receipt.OracleHistoriesRemoved)
		require.Equal(t, 1, receipt.ConstitutionPendingRemoved)
		require.NoError(t, application.validateZeroHeightEVMContinuity(
			GenesisState{evmtypes.ModuleName: originalEVM}, fixture))

		transformedOracle := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[oracletypes.ModuleName],
			transformedOracle,
		))
		require.Equal(t, originalOracleParams, transformedOracle.Params)
		require.Equal(t, oracleFixture.Tasks, transformedOracle.Tasks)
		require.Empty(t, transformedOracle.LatestValues)
		require.Empty(t, transformedOracle.History)
		require.Empty(t, transformedOracle.TaskSchedule)

		transformedConstitution := new(constitutiontypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			fixture[constitutiontypes.ModuleName],
			transformedConstitution,
		))
		require.Nil(t, transformedConstitution.PendingMinGasPrice)

		consensusParams := &cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{
			VoteExtensionsEnableHeight: 10,
		}}
		require.ErrorContains(t, application.ValidateGenesisConsensusAtHeight(
			fixture, zeroHeightEffectiveInitialHeight, consensusParams,
		), "initial Oracle schedule has 0 entries")
		require.NoError(t, application.ValidateGenesisConsensusAtHeight(
			fixture, zeroHeightEffectiveInitialHeight,
			&cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{}},
		))
		require.ErrorContains(t, application.ValidateGenesisConsensusAtHeight(
			fixture, exported.Height, consensusParams,
		), "configure target Oracle before startup")
		// Operations prepares a fresh target schedule after export.
		transformedOracle.TaskSchedule, err = buildInitialOracleSchedule(transformedOracle.Tasks, 10)
		require.NoError(t, err)
		fixture[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(transformedOracle)
		require.ErrorContains(t, application.ValidateGenesisConsensusAtHeight(
			fixture, zeroHeightEffectiveInitialHeight,
			&cmtproto.ConsensusParams{Abci: &cmtproto.ABCIParams{}},
		), "disabled target Oracle must have an empty initial schedule")
		require.NoError(t, application.ValidateGenesisConsensusAtHeight(
			fixture,
			zeroHeightEffectiveInitialHeight,
			consensusParams,
		))

		require.NotNil(t, validatedZeroGenesis)
		restartFixture := cloneGenesisState(validatedZeroGenesis)
		staleDistribution := cloneGenesisState(restartFixture)
		staleDistributionState := new(distrtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			staleDistribution[distrtypes.ModuleName],
			staleDistributionState,
		))
		require.NotEmpty(t, staleDistributionState.OutstandingRewards)
		staleDistributionState.OutstandingRewards[0].OutstandingRewards =
			staleDistributionState.OutstandingRewards[0].OutstandingRewards.Add(
				sdk.NewDecCoinFromDec(
					config.BaseDenom,
					sdkmath.LegacyMustNewDecFromStr("0.5"),
				),
			)
		staleDistribution[distrtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			staleDistributionState,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			staleDistribution,
			zeroHeightEffectiveInitialHeight,
		), "target distribution outstanding rewards")

		staleOracle := cloneGenesisState(restartFixture)
		staleOracleState := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			staleOracle[oracletypes.ModuleName],
			staleOracleState,
		))
		staleOracleState.LatestValues = []*oracletypes.OracleValue{observed}
		staleOracle[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			staleOracleState,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			staleOracle,
			zeroHeightEffectiveInitialHeight,
		), "target Oracle observations were not removed")

		pendingConstitution := cloneGenesisState(restartFixture)
		pendingConstitutionState := new(constitutiontypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			pendingConstitution[constitutiontypes.ModuleName],
			pendingConstitutionState,
		))
		pendingConstitutionState.PendingMinGasPrice = &constitutiontypes.MinGasPriceSchedule{
			EffectiveHeight:                15,
			ScheduledMinGasPrice:           "630000000000.000000000000000000",
			SourceSymbol:                   config.MinGasPriceOracleSymbol,
			SourceValue:                    "1.0",
			SourceOracleHeight:             10,
			SourceSubmissionIntervalBlocks: 5,
			PendingDelayBlocks:             5,
			PendingDelayCapBlocks:          constitutiontypes.MinGasPricePendingDelayCap,
			RawMinGasPrice:                 constitutiontypes.MinGasPriceScaleFactor,
			PreviousMinGasPrice:            "630000000000.000000000000000000",
		}
		pendingConstitution[constitutiontypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			pendingConstitutionState,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			pendingConstitution,
			zeroHeightEffectiveInitialHeight,
		), "target Constitution pending minimum gas price was not removed")

		nonzeroStakingHeight := cloneGenesisState(restartFixture)
		nonzeroStakingState := new(stakingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			nonzeroStakingHeight[stakingtypes.ModuleName],
			nonzeroStakingState,
		))
		require.NotEmpty(t, nonzeroStakingState.Validators)
		nonzeroStakingState.Validators[0].UnbondingHeight = 4
		nonzeroStakingHeight[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			nonzeroStakingState,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			nonzeroStakingHeight,
			zeroHeightEffectiveInitialHeight,
		), "has nonzero unbonding height 4")

		pendingStaking := cloneGenesisState(restartFixture)
		pendingStakingState := new(stakingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			pendingStaking[stakingtypes.ModuleName],
			pendingStakingState,
		))
		require.NotEmpty(t, pendingStakingState.Validators)
		pendingStakingState.Validators[0].UnbondingIds = []uint64{1}
		pendingStaking[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			pendingStakingState,
		)
		// Upstream owns unbonding IDs; the zero-height boundary must not ban them.
		require.NoError(t, application.ValidateGenesisAtHeight(
			pendingStaking,
			zeroHeightEffectiveInitialHeight,
		))

		staleSlashing := cloneGenesisState(restartFixture)
		staleSlashingState := new(slashingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			staleSlashing[slashingtypes.ModuleName],
			staleSlashingState,
		))
		consensusAddress, err := nonzeroStakingState.Validators[0].GetConsAddr()
		require.NoError(t, err)
		encodedConsensusAddress, err := application.StakingKeeper.ConsensusAddressCodec().BytesToString(
			consensusAddress,
		)
		require.NoError(t, err)
		staleSlashingState.SigningInfos = []slashingtypes.SigningInfo{{
			Address: encodedConsensusAddress,
			ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
				sdk.ConsAddress(consensusAddress),
				0,
				0,
				time.Unix(0, 0),
				false,
				1,
			),
		}}
		staleSlashingState.MissedBlocks = []slashingtypes.ValidatorMissedBlocks{{
			Address: encodedConsensusAddress,
		}}
		staleSlashing[slashingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			staleSlashingState,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			staleSlashing,
			zeroHeightEffectiveInitialHeight,
		), "target signing info")

		missingActiveSlashing := cloneGenesisState(restartFixture)
		missingActiveSlashingState := new(slashingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			missingActiveSlashing[slashingtypes.ModuleName],
			missingActiveSlashingState,
		))
		require.NotEmpty(t, missingActiveSlashingState.SigningInfos)
		missingActiveSlashingState.SigningInfos = nil
		missingActiveSlashingState.MissedBlocks = nil
		missingActiveSlashing[slashingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			missingActiveSlashingState,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			missingActiveSlashing,
			zeroHeightEffectiveInitialHeight,
		), "missing signing info")

		nonemptyRestartHistory := cloneGenesisState(restartFixture)
		nonemptyRestartEVM := new(evmtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			nonemptyRestartHistory[evmtypes.ModuleName],
			nonemptyRestartEVM,
		))
		historyFound := false
		for i := range nonemptyRestartEVM.Accounts {
			if common.HexToAddress(nonemptyRestartEVM.Accounts[i].Address) ==
				ethparams.HistoryStorageAddress {
				nonemptyRestartEVM.Accounts[i].Storage = evmtypes.Storage{
					evmtypes.NewState(common.Hash{}, common.HexToHash("0x01")),
				}
				historyFound = true
			}
		}
		require.True(t, historyFound)
		nonemptyRestartHistory[evmtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			nonemptyRestartEVM,
		)
		require.ErrorContains(t, application.ValidateGenesisAtHeight(
			nonemptyRestartHistory,
			zeroHeightEffectiveInitialHeight,
		), "EIP-2935")

		require.ErrorContains(t, application.ValidateGenesisConsensusAtHeight(
			fixture,
			zeroHeightEffectiveInitialHeight,
			nil,
		), "consensus params are required")
		require.ErrorContains(t, application.ValidateGenesisConsensusAtHeight(
			fixture,
			zeroHeightEffectiveInitialHeight,
			&cmtproto.ConsensusParams{},
		), "ABCI consensus params are required")
		mismatchedSchedule := cloneGenesisState(fixture)
		mismatchedOracle := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			mismatchedSchedule[oracletypes.ModuleName],
			mismatchedOracle,
		))
		mismatchedOracle.TaskSchedule[0].Height++
		mismatchedSchedule[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			mismatchedOracle,
		)
		require.ErrorContains(t, application.ValidateGenesisConsensusAtHeight(
			mismatchedSchedule,
			zeroHeightEffectiveInitialHeight,
			consensusParams,
		), "expected AAA/USD@13")
		require.NoError(t, application.ValidateGenesisConsensusAtHeight(
			mismatchedSchedule,
			exported.Height,
			nil,
		))

		zeroInterval := cloneGenesisState(exportedGenesis)
		invalidOracle := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			zeroInterval[oracletypes.ModuleName],
			invalidOracle,
		))
		invalidOracle.Tasks = []*oracletypes.OracleTask{{
			Symbol:    "INVALID/USD",
			ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:   true,
		}}
		zeroInterval[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(invalidOracle)
		require.ErrorContains(
			t,
			application.transformZeroHeightGenesis(
				zeroInterval,
				&zeroHeightExportReceipt{},
			),
			"zero submission_interval",
		)

		nonemptyHistory := cloneGenesisState(exportedGenesis)
		evmFixture := new(evmtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			nonemptyHistory[evmtypes.ModuleName],
			evmFixture,
		))
		nonemptyHistoryFound := false
		for i := range evmFixture.Accounts {
			if common.HexToAddress(evmFixture.Accounts[i].Address) == ethparams.HistoryStorageAddress {
				nonemptyHistoryFound = true
				evmFixture.Accounts[i].Storage = evmtypes.Storage{
					evmtypes.NewState(common.Hash{}, common.HexToHash("0x01")),
				}
			}
		}
		require.True(t, nonemptyHistoryFound)
		evmFixture.Accounts = append(evmFixture.Accounts, evmtypes.GenesisAccount{
			Address: "0x0000000000000000000000000000000000123456", Code: "0x6000",
			Storage: evmtypes.Storage{evmtypes.NewState(common.Hash{}, common.HexToHash("0x02"))},
		})
		nonemptyHistory[evmtypes.ModuleName] = application.AppCodec().MustMarshalJSON(evmFixture)
		nonemptyEVM := slices.Clone(nonemptyHistory[evmtypes.ModuleName])
		require.NoError(t, application.validateEVMHistoryContract(nonemptyHistory, false))
		require.Equal(t, nonemptyEVM, nonemptyHistory[evmtypes.ModuleName])
		historyReceipt := new(zeroHeightExportReceipt)
		require.NoError(t, application.transformZeroHeightEVM(nonemptyHistory, historyReceipt))
		require.Equal(t, 1, historyReceipt.EVMHistorySlotsRemoved)
		require.NoError(t, application.validateEVMHistoryContract(nonemptyHistory, true))
		require.NoError(t, application.validateZeroHeightEVMContinuity(
			GenesisState{evmtypes.ModuleName: nonemptyEVM}, nonemptyHistory))
		for _, mutation := range []string{"other storage", "other code", "history code", "history retained", "duplicate history", "missing history"} {
			t.Run(mutation, func(t *testing.T) {
				state := new(evmtypes.GenesisState)
				require.NoError(t, application.AppCodec().UnmarshalJSON(nonemptyHistory[evmtypes.ModuleName], state))
				for i := range state.Accounts {
					if common.HexToAddress(state.Accounts[i].Address) != ethparams.HistoryStorageAddress {
						continue
					}
					switch mutation {
					case "history code":
						state.Accounts[i].Code = "0x6000"
					case "history retained":
						state.Accounts[i].Storage = evmtypes.Storage{evmtypes.NewState(common.Hash{}, common.HexToHash("0x01"))}
					case "duplicate history":
						state.Accounts = append(state.Accounts, state.Accounts[i])
					case "missing history":
						state.Accounts = append(state.Accounts[:i], state.Accounts[i+1:]...)
					}
					break
				}
				if mutation == "other storage" {
					state.Accounts[len(state.Accounts)-1].Storage = nil
				}
				if mutation == "other code" {
					state.Accounts[len(state.Accounts)-1].Code = "0x6001"
				}
				target := GenesisState{evmtypes.ModuleName: application.AppCodec().MustMarshalJSON(state)}
				require.Error(t, application.validateZeroHeightEVMContinuity(GenesisState{evmtypes.ModuleName: nonemptyEVM}, target))
			})
		}
	})
	t.Run("failed cached transformation leaves source export unchanged", func(t *testing.T) {
		before, err := application.ExportAppStateAndValidators(false, nil, nil)
		require.NoError(t, err)

		sourceCtx := committedContext(t, application)
		targetCtx, _ := sourceCtx.CacheContext()
		_, _, err = application.prepareZeroHeightState(targetCtx, nil)
		require.NoError(t, err)

		invalid := cloneGenesisState(exportedGenesis)
		invalidOracle := new(oracletypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			invalid[oracletypes.ModuleName],
			invalidOracle,
		))
		invalidOracle.Tasks = []*oracletypes.OracleTask{{
			Symbol:    "INVALID/USD",
			ValueType: oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:   true,
		}}
		invalid[oracletypes.ModuleName] = application.AppCodec().MustMarshalJSON(invalidOracle)
		require.ErrorContains(
			t,
			application.transformZeroHeightGenesis(invalid, &zeroHeightExportReceipt{}),
			"zero submission_interval",
		)

		after, err := application.ExportAppStateAndValidators(false, nil, nil)
		require.NoError(t, err)
		require.Equal(t, before.AppState, after.AppState)
		require.Equal(t, before.Validators, after.Validators)
		require.Equal(t, before.Height, after.Height)
		require.Equal(t, before.ConsensusParams, after.ConsensusParams)
	})

	t.Run("zero-height preflight rejects unhandled continuity state", func(t *testing.T) {
		unknownModule := cloneGenesisState(exportedGenesis)
		unknownModule["zzz-future-height-state"] = json.RawMessage(`{}`)
		unknownModule["aaa-future-height-state"] = json.RawMessage(`{}`)
		require.EqualError(
			t,
			validateHandledZeroHeightModules(unknownModule),
			`zero-height policy is undefined for modules ["aaa-future-height-state" "zzz-future-height-state"]`,
		)

		missingPersistentDocument := cloneGenesisState(exportedGenesis)
		delete(missingPersistentDocument, banktypes.ModuleName)
		require.EqualError(
			t,
			application.validateZeroHeightModuleInventory(missingPersistentDocument),
			`zero-height export is missing genesis documents for persistent stores ["bank"]`,
		)

		ibcFixture := cloneGenesisState(exportedGenesis)
		ibcGenesis := new(ibccoretypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			ibcFixture["ibc"],
			ibcGenesis,
		))
		ibcGenesis.ClientGenesis.NextClientSequence = 1
		ibcFixture["ibc"] = application.AppCodec().MustMarshalJSON(ibcGenesis)
		require.NoError(
			t,
			application.validateNoActiveIBCLifecycle(committedContext(t, application), ibcFixture),
		)
		nonCanonicalLocalhost := cloneGenesisState(exportedGenesis)
		nonCanonicalIBC := new(ibccoretypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			nonCanonicalLocalhost["ibc"],
			nonCanonicalIBC,
		))
		localhostFound := false
		for i := range nonCanonicalIBC.ConnectionGenesis.Connections {
			if nonCanonicalIBC.ConnectionGenesis.Connections[i].Id ==
				ibcexported.LocalhostConnectionID {
				nonCanonicalIBC.ConnectionGenesis.Connections[i].State = connectiontypes.INIT
				localhostFound = true
			}
		}
		require.True(t, localhostFound)
		nonCanonicalLocalhost["ibc"] = application.AppCodec().MustMarshalJSON(nonCanonicalIBC)
		require.ErrorContains(
			t,
			application.validateNoActiveIBCLifecycle(
				committedContext(t, application),
				nonCanonicalLocalhost,
			),
			"connection-localhost(non-canonical)",
		)
		ibcGenesis.ChannelGenesis.Commitments = []channeltypes.PacketState{{
			PortId:    "transfer",
			ChannelId: "channel-0",
			Sequence:  1,
			Data:      []byte{0x01},
		}}
		ibcFixture["ibc"] = application.AppCodec().MustMarshalJSON(ibcGenesis)
		require.ErrorContains(
			t,
			application.validateNoActiveIBCLifecycle(committedContext(t, application), ibcFixture),
			"packet_commitments=1",
		)

		governanceFixture := cloneGenesisState(exportedGenesis)
		governanceGenesis := new(govv1.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			governanceFixture[govtypes.ModuleName],
			governanceGenesis,
		))
		governanceGenesis.Proposals = append(governanceGenesis.Proposals, &govv1.Proposal{
			Id:     99,
			Status: govv1.StatusVotingPeriod,
		}, &govv1.Proposal{
			Id:     100,
			Status: govv1.StatusNil,
		})
		governanceFixture[govtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
			governanceGenesis,
		)
		require.ErrorContains(
			t,
			application.validateNoActiveGovernance(governanceFixture),
			"99(PROPOSAL_STATUS_VOTING_PERIOD)",
		)
		require.ErrorContains(
			t,
			application.validateNoActiveGovernance(governanceFixture),
			"100(PROPOSAL_STATUS_UNSPECIFIED)",
		)

		upgradeCtx, _ := committedContext(t, application).CacheContext()
		require.NoError(t, application.UpgradeKeeper.ScheduleUpgrade(upgradeCtx, upgradetypes.Plan{
			Name:   "pending-zero-height-test",
			Height: application.LastBlockHeight() + 100,
		}))
		require.ErrorContains(
			t,
			application.validateZeroHeightPreflight(upgradeCtx, exportedGenesis),
			"pending software upgrade",
		)

		evmMissingHistory := cloneGenesisState(exportedGenesis)
		evmGenesis := new(evmtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			evmMissingHistory[evmtypes.ModuleName],
			evmGenesis,
		))
		accounts := evmGenesis.Accounts[:0]
		for _, account := range evmGenesis.Accounts {
			if common.HexToAddress(account.Address) != ethparams.HistoryStorageAddress {
				accounts = append(accounts, account)
			}
		}
		evmGenesis.Accounts = accounts
		evmMissingHistory[evmtypes.ModuleName] = application.AppCodec().MustMarshalJSON(evmGenesis)
		require.ErrorContains(
			t,
			application.validateEVMHistoryContract(evmMissingHistory, false),
			"EIP-2935 history contract account is missing",
		)
	})

	t.Run("zero-height jail allowlist permits upstream bonded exclusion", func(t *testing.T) {
		ctx := committedContext(t, application)
		validators, err := application.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		bondedIndex := slices.IndexFunc(validators, func(validator stakingtypes.Validator) bool {
			return validator.IsBonded()
		})
		require.NotEqual(t, -1, bondedIndex)

		inactive := validators[bondedIndex]
		inactiveOperator, err := application.StakingKeeper.ValidatorAddressCodec().BytesToString(
			bytes.Repeat([]byte{0x7a}, 20),
		)
		require.NoError(t, err)
		require.NotEqual(t, inactive.OperatorAddress, inactiveOperator)
		inactive.OperatorAddress = inactiveOperator
		inactive.Status = stakingtypes.Unbonded
		validators = append(validators, inactive)

		_, err = application.validateJailAllowlist(ctx, validators, []string{inactiveOperator})
		require.NoError(t, err)
	})

	t.Run("zero-height jail allowlist cannot recover tombstoned validators", func(t *testing.T) {
		tombstoneCtx, _ := committedContext(t, application).CacheContext()
		exportedStaking := new(stakingtypes.GenesisState)
		require.NoError(t, application.AppCodec().UnmarshalJSON(
			exportedGenesis[stakingtypes.ModuleName],
			exportedStaking,
		))
		require.NotEmpty(t, exportedStaking.Validators)
		operator := exportedStaking.Validators[0].OperatorAddress
		operatorBytes, err := application.StakingKeeper.ValidatorAddressCodec().StringToBytes(operator)
		require.NoError(t, err)
		validator, err := application.StakingKeeper.GetValidator(tombstoneCtx, operatorBytes)
		require.NoError(t, err)
		consensusAddress, err := validator.GetConsAddr()
		require.NoError(t, err)
		signingInfo, err := application.SlashingKeeper.GetValidatorSigningInfo(
			tombstoneCtx,
			consensusAddress,
		)
		if err != nil {
			require.ErrorIs(t, err, slashingtypes.ErrNoSigningInfoFound)
			signingInfo = slashingtypes.NewValidatorSigningInfo(
				consensusAddress,
				application.LastBlockHeight(),
				0,
				blockTime,
				false,
				0,
			)
		}
		signingInfo.Tombstoned = true
		require.NoError(t, application.SlashingKeeper.SetValidatorSigningInfo(
			tombstoneCtx,
			consensusAddress,
			signingInfo,
		))
		validators, err := application.StakingKeeper.GetAllValidators(tombstoneCtx)
		require.NoError(t, err)
		_, err = application.validateJailAllowlist(tombstoneCtx, validators, []string{operator})
		require.ErrorContains(t, err, "cannot unjail tombstoned validator")
	})

}
