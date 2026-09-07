package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/rootmulti"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v10/modules/core/23-commitment/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibccoretypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	"github.com/ethereum/go-ethereum/common"
	ethparams "github.com/ethereum/go-ethereum/params"

	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

const zeroHeightEffectiveInitialHeight int64 = 1

// ValidateExportRequest validates flag relationships before any application or
// consensus database is opened.
func ValidateExportRequest(
	forZeroHeight bool,
	jailAllowedAddrs []string,
	modulesToExport []string,
) error {
	if !forZeroHeight && len(jailAllowedAddrs) != 0 {
		return fmt.Errorf("jail allowlist requires zero-height export")
	}
	if forZeroHeight && len(modulesToExport) != 0 {
		return fmt.Errorf("zero-height export requires a complete module export")
	}
	return nil
}

// zeroHeightExportReceipt is emitted through the structured application logger.
// It contains only deterministic counts and committed heights so independently
// generated receipts can be compared directly.
type zeroHeightExportReceipt struct {
	SourceHeight               int64
	Validators                 int
	Delegations                int
	UnbondingEntriesReset      int
	RedelegationEntriesReset   int
	SigningInfosReset          int
	MissedBlockWindowsReset    int
	OracleTasksPreserved       int
	OracleSchedulesRemoved     int
	OracleLatestValuesRemoved  int
	OracleHistoriesRemoved     int
	ConstitutionPendingRemoved int
	EVMHistorySlotsRemoved     int
	RewardPayoutRecipients     int
	AuthAccountsCreated        int
	RewardPayoutTotal          string
	CurrentMinGasPrice         string
}

// zeroHeightMutationWitness records the exact side effects returned by the
// upstream keepers while the isolated transformation runs. It is deliberately
// not serialized: it exists only to prove that the exported source-to-target
// delta is the one the keepers actually produced.
type zeroHeightMutationWitness struct {
	RewardPayouts         map[string]sdk.Coins
	NewRewardAccounts     []string
	JailAllowlistApplied  bool
	JailAllowedValidators map[string]struct{}
	StakingValidators     map[string]stakingtypes.Validator
}

// ExportAppStateAndValidators exports the currently loaded state. A zero-height
// export is built in a nested cache context and is never written back to the
// source application's committed or check state.
func (app *App) ExportAppStateAndValidators(
	forZeroHeight bool,
	jailAllowedAddrs []string,
	modulesToExport []string,
) (exported servertypes.ExportedApp, err error) {
	// Some upstream module exporters still panic on corrupt state. Convert those
	// panics at the operator boundary so a failed transform cannot be mistaken
	// for a completed genesis.
	defer func() {
		if recovered := recover(); recovered != nil {
			exported = servertypes.ExportedApp{}
			err = fmt.Errorf("export application state: %v", recovered)
		}
	}()

	return app.exportAppStateAndValidators(
		forZeroHeight,
		jailAllowedAddrs,
		modulesToExport,
	)
}

func (app *App) exportAppStateAndValidators(
	forZeroHeight bool,
	jailAllowedAddrs []string,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	if err := ValidateExportRequest(forZeroHeight, jailAllowedAddrs, modulesToExport); err != nil {
		return servertypes.ExportedApp{}, err
	}
	if app.LastBlockHeight() == math.MaxInt64 {
		return servertypes.ExportedApp{}, fmt.Errorf("source height cannot be incremented")
	}
	nextHeight := app.LastBlockHeight() + 1

	ctx, err := app.newExportContext(forZeroHeight)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	consensusParams := app.GetConsensusParams(ctx)
	if forZeroHeight {
		return app.exportZeroHeightGenesis(ctx, jailAllowedAddrs, consensusParams)
	}
	genesis, err := app.ModuleManager.ExportGenesisForModules(ctx, app.AppCodec(), modulesToExport)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	// A complete export must be accepted by the same general validator used by
	// InitChain. Partial module exports are intentionally not bootable documents.
	if len(modulesToExport) == 0 {
		if err := app.ValidateGenesis(GenesisState(genesis)); err != nil {
			return servertypes.ExportedApp{}, fmt.Errorf("validate exported application state: %w", err)
		}
	}

	return app.marshalExportedApp(
		ctx,
		GenesisState(genesis),
		nextHeight,
		consensusParams,
	)
}

// newExportContext uses only the loaded application store. Zero-height validator
// recalculation can begin unbonding, so it needs the committed block time rather
// than wall-clock time or a zero timestamp. No CometBFT database is consulted.
func (app *App) newExportContext(forZeroHeight bool) (sdk.Context, error) {
	header := cmtproto.Header{Height: app.LastBlockHeight(), ChainID: app.ChainID()}
	if forZeroHeight {
		store, ok := app.CommitMultiStore().(*rootmulti.Store)
		if !ok {
			return sdk.Context{}, fmt.Errorf("application store does not expose commit metadata")
		}
		info, err := store.GetCommitInfo(header.Height)
		if err != nil {
			return sdk.Context{}, fmt.Errorf("load application commit metadata at height %d: %w", header.Height, err)
		}
		if info == nil || info.Version != header.Height || info.Timestamp.IsZero() {
			return sdk.Context{}, fmt.Errorf("application commit time is unavailable at height %d", header.Height)
		}
		header.Time = info.Timestamp
	}
	return app.NewContextLegacy(true, header), nil
}

func (app *App) exportZeroHeightGenesis(
	sourceCtx sdk.Context,
	jailAllowedAddrs []string,
	consensusParams cmtproto.ConsensusParams,
) (servertypes.ExportedApp, error) {
	if app.LastBlockHeight() == math.MaxInt64 {
		return servertypes.ExportedApp{}, fmt.Errorf("source height cannot be incremented")
	}

	sourceGenesis, err := app.ModuleManager.ExportGenesisForModules(sourceCtx, app.AppCodec(), nil)
	if err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("export source application state: %w", err)
	}
	sourceState := GenesisState(sourceGenesis)
	if err := app.ValidateGenesisAtHeight(sourceState, app.LastBlockHeight()+1); err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("validate source application state: %w", err)
	}
	if err := app.validateExportInvariants(sourceState); err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("validate source export invariants: %w", err)
	}
	if err := app.validateZeroHeightPreflight(sourceCtx, sourceState); err != nil {
		return servertypes.ExportedApp{}, err
	}

	// Never call writeCache: all reward settlement and staking/slashing rewrites
	// remain isolated from the live DB and BaseApp's reusable check state.
	targetCtx, _ := sourceCtx.CacheContext()
	receipt, witness, err := app.prepareZeroHeightState(targetCtx, jailAllowedAddrs)
	if err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("prepare zero-height state: %w", err)
	}

	targetGenesis, err := app.ModuleManager.ExportGenesisForModules(targetCtx, app.AppCodec(), nil)
	if err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("export transformed application state: %w", err)
	}
	targetState := GenesisState(targetGenesis)
	if err := app.transformZeroHeightGenesis(targetState, &receipt); err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("transform zero-height genesis: %w", err)
	}
	if err := app.ValidateGenesisAtHeight(targetState, zeroHeightEffectiveInitialHeight); err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("validate zero-height application state: %w", err)
	}
	// Oracle activation and initial schedules are validated after target launch configuration.
	// ValidateGenesisAtHeight already checks the target export invariants.
	if err := app.validateZeroHeightEconomicContinuity(sourceState, targetState, witness); err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("validate zero-height economic continuity: %w", err)
	}

	// Cosmos SDK writes initial_height=0 for --for-zero-height. CometBFT
	// normalizes that sentinel to the effective first block height I=1.
	exported, err := app.marshalExportedApp(targetCtx, targetState, 0, consensusParams)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	receipt.SourceHeight = app.LastBlockHeight()
	feeMarket := new(feemarkettypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(targetState[feemarkettypes.ModuleName], feeMarket); err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("decode target feemarket receipt state: %w", err)
	}
	receipt.CurrentMinGasPrice = feeMarket.Params.MinGasPrice.String()
	app.logZeroHeightExportReceipt(receipt)
	return exported, nil
}

func (app *App) marshalExportedApp(
	ctx sdk.Context,
	genesis GenesisState,
	height int64,
	consensusParams cmtproto.ConsensusParams,
) (servertypes.ExportedApp, error) {
	appState, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("marshal application state: %w", err)
	}
	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	if err != nil {
		return servertypes.ExportedApp{}, fmt.Errorf("write genesis validators: %w", err)
	}

	return servertypes.ExportedApp{
		AppState:        appState,
		Validators:      validators,
		Height:          height,
		ConsensusParams: consensusParams,
	}, nil
}

func (app *App) logZeroHeightExportReceipt(receipt zeroHeightExportReceipt) {
	app.Logger().Info(
		"zero-height application-state transformation completed",
		"source_height", receipt.SourceHeight,
		"effective_initial_height", zeroHeightEffectiveInitialHeight,
		"validators", receipt.Validators,
		"delegations", receipt.Delegations,
		"unbonding_entries_reset", receipt.UnbondingEntriesReset,
		"redelegation_entries_reset", receipt.RedelegationEntriesReset,
		"signing_infos_reset", receipt.SigningInfosReset,
		"missed_block_windows_reset", receipt.MissedBlockWindowsReset,
		"oracle_tasks_preserved", receipt.OracleTasksPreserved,
		"oracle_schedules_removed", receipt.OracleSchedulesRemoved,
		"oracle_latest_values_removed", receipt.OracleLatestValuesRemoved,
		"oracle_histories_removed", receipt.OracleHistoriesRemoved,
		"evm_history_slots_removed", receipt.EVMHistorySlotsRemoved,
		"constitution_pending_removed", receipt.ConstitutionPendingRemoved,
		"reward_payout_recipients", receipt.RewardPayoutRecipients,
		"auth_accounts_created", receipt.AuthAccountsCreated,
		"reward_payout_total", receipt.RewardPayoutTotal,
		"current_min_gas_price", receipt.CurrentMinGasPrice,
		"chain_id", app.ChainID(),
		"evm_chain_id", app.EVMChainID(),
	)
}

// prepareZeroHeightState adapts Cosmos SDK v0.53.6 simapp/export.go to Guru's
// keeper graph. Standard state changes use upstream keeper APIs. The approved
// specification additionally requires errors instead of process exits, a
// discarded cache, payout receipts, and a fresh slashing missed-block window.
func (app *App) prepareZeroHeightState(
	ctx sdk.Context,
	jailAllowedAddrs []string,
) (zeroHeightExportReceipt, zeroHeightMutationWitness, error) {
	receipt := zeroHeightExportReceipt{}
	witness := zeroHeightMutationWitness{RewardPayouts: make(map[string]sdk.Coins)}
	witness.JailAllowlistApplied = len(jailAllowedAddrs) != 0
	validators, err := app.StakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return receipt, witness, fmt.Errorf("list validators: %w", err)
	}
	delegations, err := app.StakingKeeper.GetAllDelegations(ctx)
	if err != nil {
		return receipt, witness, fmt.Errorf("list delegations: %w", err)
	}
	receipt.Validators = len(validators)
	receipt.Delegations = len(delegations)

	allowed, err := app.validateJailAllowlist(ctx, validators, jailAllowedAddrs)
	if err != nil {
		return receipt, witness, err
	}
	witness.JailAllowedValidators = allowed
	recordPayout := func(
		recipient sdk.AccAddress,
		amount sdk.Coins,
		accountMissingBefore bool,
	) error {
		if amount.IsZero() {
			return nil
		}
		address, err := app.AccountKeeper.AddressCodec().BytesToString(recipient)
		if err != nil {
			return fmt.Errorf("encode reward recipient: %w", err)
		}
		witness.RewardPayouts[address] = witness.RewardPayouts[address].Add(amount...)
		if accountMissingBefore {
			witness.NewRewardAccounts = append(witness.NewRewardAccounts, address)
		}
		return nil
	}

	// Settle all distributable integral rewards before rebuilding the reward
	// periods. A missing commission is the only expected non-fatal condition.
	for _, validator := range validators {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			validator.GetOperator(),
		)
		if err != nil {
			return receipt, witness, fmt.Errorf("decode validator %q: %w", validator.GetOperator(), err)
		}
		recipient, err := app.DistrKeeper.GetDelegatorWithdrawAddr(
			ctx,
			sdk.AccAddress(validatorAddr),
		)
		if err != nil {
			return receipt, witness, fmt.Errorf(
				"read validator %s commission withdrawal address: %w",
				validator.GetOperator(),
				err,
			)
		}
		accountMissingBefore := app.AccountKeeper.GetAccount(ctx, recipient) == nil
		amount, err := app.DistrKeeper.WithdrawValidatorCommission(ctx, validatorAddr)
		if err != nil && !errors.Is(err, distrtypes.ErrNoValidatorCommission) {
			return receipt, witness, fmt.Errorf(
				"withdraw validator %s commission: %w",
				validator.GetOperator(),
				err,
			)
		}
		if err == nil {
			if err := recordPayout(recipient, amount, accountMissingBefore); err != nil {
				return receipt, witness, err
			}
		}
	}
	for _, delegation := range delegations {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			delegation.ValidatorAddress,
		)
		if err != nil {
			return receipt, witness, fmt.Errorf(
				"decode delegation validator %q: %w",
				delegation.ValidatorAddress,
				err,
			)
		}
		delegatorAddr, err := app.AccountKeeper.AddressCodec().StringToBytes(
			delegation.DelegatorAddress,
		)
		if err != nil {
			return receipt, witness, fmt.Errorf("decode delegator %q: %w", delegation.DelegatorAddress, err)
		}
		recipient, err := app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, delegatorAddr)
		if err != nil {
			return receipt, witness, fmt.Errorf(
				"read delegation %s/%s withdrawal address: %w",
				delegation.DelegatorAddress,
				delegation.ValidatorAddress,
				err,
			)
		}
		accountMissingBefore := app.AccountKeeper.GetAccount(ctx, recipient) == nil
		amount, err := app.DistrKeeper.WithdrawDelegationRewards(
			ctx,
			delegatorAddr,
			validatorAddr,
		)
		if err != nil {
			return receipt, witness, fmt.Errorf(
				"withdraw delegation rewards for %s/%s: %w",
				delegation.DelegatorAddress,
				delegation.ValidatorAddress,
				err,
			)
		}
		if err := recordPayout(recipient, amount, accountMissingBefore); err != nil {
			return receipt, witness, err
		}
	}
	rewardPayoutTotal := sdk.NewCoins()
	for _, payout := range witness.RewardPayouts {
		rewardPayoutTotal = rewardPayoutTotal.Add(payout...)
	}
	receipt.RewardPayoutRecipients = len(witness.RewardPayouts)
	receipt.AuthAccountsCreated = len(witness.NewRewardAccounts)
	receipt.RewardPayoutTotal = rewardPayoutTotal.String()

	app.DistrKeeper.DeleteAllValidatorSlashEvents(ctx)
	app.DistrKeeper.DeleteAllValidatorHistoricalRewards(ctx)

	zeroCtx := ctx.WithBlockHeight(0)
	for _, validator := range validators {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			validator.GetOperator(),
		)
		if err != nil {
			return receipt, witness, fmt.Errorf("decode validator %q: %w", validator.GetOperator(), err)
		}
		scraps, err := app.DistrKeeper.GetValidatorOutstandingRewardsCoins(zeroCtx, validatorAddr)
		if err != nil {
			return receipt, witness, fmt.Errorf(
				"read validator %s outstanding rewards: %w",
				validator.GetOperator(),
				err,
			)
		}
		feePool, err := app.DistrKeeper.FeePool.Get(zeroCtx)
		if err != nil {
			return receipt, witness, fmt.Errorf("read distribution fee pool: %w", err)
		}
		feePool.CommunityPool = feePool.CommunityPool.Add(scraps...)
		if err := app.DistrKeeper.FeePool.Set(zeroCtx, feePool); err != nil {
			return receipt, witness, fmt.Errorf("write distribution fee pool: %w", err)
		}
		if err := app.DistrKeeper.Hooks().AfterValidatorCreated(zeroCtx, validatorAddr); err != nil {
			return receipt, witness, fmt.Errorf(
				"reinitialize validator %s rewards: %w",
				validator.GetOperator(),
				err,
			)
		}
	}
	for _, delegation := range delegations {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			delegation.ValidatorAddress,
		)
		if err != nil {
			return receipt, witness, fmt.Errorf(
				"decode delegation validator %q: %w",
				delegation.ValidatorAddress,
				err,
			)
		}
		delegatorAddr, err := app.AccountKeeper.AddressCodec().StringToBytes(
			delegation.DelegatorAddress,
		)
		if err != nil {
			return receipt, witness, fmt.Errorf("decode delegator %q: %w", delegation.DelegatorAddress, err)
		}
		if err := app.DistrKeeper.Hooks().BeforeDelegationCreated(
			zeroCtx,
			delegatorAddr,
			validatorAddr,
		); err != nil {
			return receipt, witness, fmt.Errorf("reinitialize delegation period: %w", err)
		}
		if err := app.DistrKeeper.Hooks().AfterDelegationModified(
			zeroCtx,
			delegatorAddr,
			validatorAddr,
		); err != nil {
			return receipt, witness, fmt.Errorf("reinitialize delegation rewards: %w", err)
		}
	}

	if err := app.prepareZeroHeightStaking(ctx, validators, allowed, &receipt, &witness); err != nil {
		return receipt, witness, err
	}

	var callbackErr error
	if err := app.SlashingKeeper.IterateValidatorSigningInfos(
		ctx,
		func(address sdk.ConsAddress, info slashingtypes.ValidatorSigningInfo) bool {
			info.StartHeight = 0
			info.IndexOffset = 0
			info.MissedBlocksCounter = 0
			if err := app.SlashingKeeper.SetValidatorSigningInfo(ctx, address, info); err != nil {
				callbackErr = err
				return true
			}
			if err := app.SlashingKeeper.DeleteMissedBlockBitmap(ctx, address); err != nil {
				callbackErr = err
				return true
			}
			receipt.SigningInfosReset++
			receipt.MissedBlockWindowsReset++
			return false
		},
	); err != nil {
		return receipt, witness, fmt.Errorf("iterate validator signing infos: %w", err)
	}
	if callbackErr != nil {
		return receipt, witness, fmt.Errorf("reset validator signing state: %w", callbackErr)
	}

	return receipt, witness, nil
}

func (app *App) validateJailAllowlist(
	ctx sdk.Context,
	validators []stakingtypes.Validator,
	jailAllowedAddrs []string,
) (map[string]struct{}, error) {
	if len(jailAllowedAddrs) == 0 {
		return nil, nil
	}
	validatorsByOperator := make(map[string]stakingtypes.Validator, len(validators))
	for _, validator := range validators {
		validatorsByOperator[validator.GetOperator()] = validator
	}

	allowed := make(map[string]struct{}, len(jailAllowedAddrs))
	for _, address := range jailAllowedAddrs {
		addressBytes, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(address)
		if err != nil {
			return nil, fmt.Errorf("invalid jail-allowlist validator address %q: %w", address, err)
		}
		canonical, err := app.StakingKeeper.ValidatorAddressCodec().BytesToString(addressBytes)
		if err != nil {
			return nil, fmt.Errorf("encode jail-allowlist validator address %q: %w", address, err)
		}
		if _, duplicate := allowed[canonical]; duplicate {
			return nil, fmt.Errorf("duplicate jail-allowlist validator address %q", canonical)
		}
		validator, found := validatorsByOperator[canonical]
		if !found {
			return nil, fmt.Errorf(
				"jail-allowlist address %q does not identify an exported validator",
				canonical,
			)
		}
		consensusAddress, err := validator.GetConsAddr()
		if err != nil {
			return nil, fmt.Errorf("derive consensus address for validator %q: %w", canonical, err)
		}
		if app.SlashingKeeper.IsTombstoned(ctx, consensusAddress) {
			return nil, fmt.Errorf("jail-allowlist cannot unjail tombstoned validator %q", canonical)
		}
		allowed[canonical] = struct{}{}
	}
	return allowed, nil
}

// prepareZeroHeightStaking follows the staking section of Cosmos SDK v0.53.6
// simapp/export.go. Pending entries survive with zero creation heights; their
// amounts, completion times, IDs and hold references remain owned by upstream.
// Guru adds only its existing minimum-self-bond validator-set filter.
func (app *App) prepareZeroHeightStaking(
	ctx sdk.Context,
	validators []stakingtypes.Validator,
	allowed map[string]struct{},
	receipt *zeroHeightExportReceipt,
	witness *zeroHeightMutationWitness,
) error {
	var callbackErr error
	if err := app.StakingKeeper.IterateRedelegations(ctx, func(_ int64, red stakingtypes.Redelegation) bool {
		for i := range red.Entries {
			red.Entries[i].CreationHeight = 0
			receipt.RedelegationEntriesReset++
		}
		callbackErr = app.StakingKeeper.SetRedelegation(ctx, red)
		return callbackErr != nil
	}); err != nil {
		return fmt.Errorf("iterate redelegations: %w", err)
	}
	if callbackErr != nil {
		return fmt.Errorf("reset redelegation creation heights: %w", callbackErr)
	}
	if err := app.StakingKeeper.IterateUnbondingDelegations(ctx, func(_ int64, ubd stakingtypes.UnbondingDelegation) bool {
		for i := range ubd.Entries {
			ubd.Entries[i].CreationHeight = 0
			receipt.UnbondingEntriesReset++
		}
		callbackErr = app.StakingKeeper.SetUnbondingDelegation(ctx, ubd)
		return callbackErr != nil
	}); err != nil {
		return fmt.Errorf("iterate unbonding delegations: %w", err)
	}
	if callbackErr != nil {
		return fmt.Errorf("reset unbonding creation heights: %w", callbackErr)
	}

	for _, validator := range validators {
		if len(allowed) == 0 || validator.Jailed {
			continue
		}
		if _, included := allowed[validator.OperatorAddress]; included {
			continue
		}
		// The upstream allowlist only jails excluded validators. It does not
		// unjail included validators. Jail also maintains the power index.
		consensusAddress, err := validator.GetConsAddr()
		if err != nil {
			return fmt.Errorf("derive validator %s consensus address: %w", validator.OperatorAddress, err)
		}
		if err := app.StakingKeeper.Jail(ctx, consensusAddress); err != nil {
			return fmt.Errorf("jail excluded validator %s: %w", validator.OperatorAddress, err)
		}
	}

	// Retain the source time and queue keys while the SDK performs transitions,
	// including rebonding an unbonding validator or starting a new unbonding.
	if _, err := app.CustomStakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx); err != nil {
		return fmt.Errorf("recalculate Guru validator set: %w", err)
	}
	updated, err := app.StakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return fmt.Errorf("list recalculated validators: %w", err)
	}
	witness.StakingValidators = make(map[string]stakingtypes.Validator, len(updated))
	for _, validator := range updated {
		// Normalize after recalculation so newly unbonding validators also have
		// height zero. Use upstream queue APIs to keep the isolated cache
		// consistent with the height that InitGenesis will import.
		if validator.IsUnbonding() {
			if err := app.StakingKeeper.DeleteValidatorQueue(ctx, validator); err != nil {
				return fmt.Errorf("remove source-height validator queue: %w", err)
			}
		}
		validator.UnbondingHeight = 0
		if err := app.StakingKeeper.SetValidator(ctx, validator); err != nil {
			return fmt.Errorf("reset validator %s unbonding height: %w", validator.OperatorAddress, err)
		}
		if validator.IsUnbonding() {
			if err := app.StakingKeeper.InsertUnbondingValidatorQueue(ctx, validator); err != nil {
				return fmt.Errorf("insert zero-height validator queue: %w", err)
			}
		}
		witness.StakingValidators[validator.OperatorAddress] = validator
	}
	return nil
}

func (app *App) validateExportInvariants(genesis GenesisState) error {
	if err := app.validateStakingExportInvariants(genesis); err != nil {
		return fmt.Errorf("staking: %w", err)
	}
	if err := app.validateDistributionExportInvariants(genesis); err != nil {
		return fmt.Errorf("distribution: %w", err)
	}
	return nil
}

// validateStakingExportInvariants checks the read-only export assertions required
// by the zero-height specification. Pool accounting matches SDK v0.53.6
// staking/keeper.InitGenesis; this does not add staking lifecycle rules or
// reconstruct upstream store indexes. The SDK's lightweight ValidateGenesis
// does not compare bank and staking genesis documents.
func (app *App) validateStakingExportInvariants(genesis GenesisState) error {
	stakingGenesis := new(stakingtypes.GenesisState)
	raw, ok := genesis[stakingtypes.ModuleName]
	if !ok {
		return fmt.Errorf("staking genesis is missing")
	}
	if err := app.AppCodec().UnmarshalJSON(raw, stakingGenesis); err != nil {
		return fmt.Errorf("decode staking genesis: %w", err)
	}
	if !stakingGenesis.Exported {
		return fmt.Errorf("staking genesis is not marked as exported state")
	}

	bankGenesis := new(banktypes.GenesisState)
	raw, ok = genesis[banktypes.ModuleName]
	if !ok {
		return fmt.Errorf("bank genesis is missing")
	}
	if err := app.AppCodec().UnmarshalJSON(raw, bankGenesis); err != nil {
		return fmt.Errorf("decode bank genesis: %w", err)
	}

	validators := make(map[string]stakingtypes.Validator, len(stakingGenesis.Validators))
	delegationShares := make(
		map[string]sdkmath.LegacyDec,
		len(stakingGenesis.Validators),
	)
	bondedTokens := sdkmath.ZeroInt()
	notBondedTokens := sdkmath.ZeroInt()
	for _, validator := range stakingGenesis.Validators {
		operatorBytes, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			validator.OperatorAddress,
		)
		if err != nil {
			return fmt.Errorf(
				"invalid validator operator address %q: %w",
				validator.OperatorAddress,
				err,
			)
		}
		key := string(operatorBytes)
		if _, duplicate := validators[key]; duplicate {
			return fmt.Errorf("duplicate validator operator address %q", validator.OperatorAddress)
		}
		validators[key] = validator
		delegationShares[key] = sdkmath.LegacyZeroDec()
		if validator.IsBonded() {
			bondedTokens = bondedTokens.Add(validator.Tokens)
		} else {
			notBondedTokens = notBondedTokens.Add(validator.Tokens)
		}
	}
	for _, delegation := range stakingGenesis.Delegations {
		validatorBytes, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			delegation.ValidatorAddress,
		)
		if err != nil {
			return fmt.Errorf(
				"invalid delegation validator address %q: %w",
				delegation.ValidatorAddress,
				err,
			)
		}
		key := string(validatorBytes)
		if _, found := validators[key]; !found {
			return fmt.Errorf(
				"delegation references missing validator %q",
				delegation.ValidatorAddress,
			)
		}
		delegationShares[key] = delegationShares[key].Add(delegation.Shares)
	}
	// InitGenesis checks the not-bonded pool against both tokens owned by
	// unbonded/unbonding validators and balances that have already left a
	// validator but are still waiting in an unbonding-delegation entry.
	for _, unbonding := range stakingGenesis.UnbondingDelegations {
		_, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			unbonding.ValidatorAddress,
		)
		if err != nil {
			return fmt.Errorf(
				"invalid unbonding-delegation validator address %q: %w",
				unbonding.ValidatorAddress,
				err,
			)
		}
		// A fully removed validator may still have pending delegator withdrawals.
		// Upstream InitGenesis restores those entries without requiring the
		// validator record to survive.
		for _, entry := range unbonding.Entries {
			if entry.Balance.IsNegative() {
				return fmt.Errorf(
					"unbonding delegation for %s has negative balance %s",
					unbonding.ValidatorAddress,
					entry.Balance,
				)
			}
			notBondedTokens = notBondedTokens.Add(entry.Balance)
		}
	}
	validatorKeys := make([]string, 0, len(validators))
	for key := range validators {
		validatorKeys = append(validatorKeys, key)
	}
	sort.Strings(validatorKeys)
	for _, key := range validatorKeys {
		validator := validators[key]
		if !delegationShares[key].Equal(validator.DelegatorShares) {
			return fmt.Errorf(
				"validator %s delegation shares %s do not equal validator shares %s",
				validator.OperatorAddress,
				delegationShares[key],
				validator.DelegatorShares,
			)
		}
	}

	bondedPoolAddress, err := app.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(stakingtypes.BondedPoolName),
	)
	if err != nil {
		return fmt.Errorf("encode bonded pool address: %w", err)
	}
	notBondedPoolAddress, err := app.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(stakingtypes.NotBondedPoolName),
	)
	if err != nil {
		return fmt.Errorf("encode not-bonded pool address: %w", err)
	}
	balances := make(map[string]sdk.Coins, len(bankGenesis.Balances))
	for _, balance := range bankGenesis.Balances {
		balances[balance.Address] = balance.Coins
	}
	bondDenom := stakingGenesis.Params.BondDenom
	expectedBondedBalance := coinsForExportAmount(bondDenom, bondedTokens)
	if actual := balances[bondedPoolAddress]; !actual.Equal(expectedBondedBalance) {
		return fmt.Errorf(
			"bonded pool balance %s does not equal expected balance %s",
			actual,
			expectedBondedBalance,
		)
	}
	expectedNotBondedBalance := coinsForExportAmount(bondDenom, notBondedTokens)
	if actual := balances[notBondedPoolAddress]; !actual.Equal(expectedNotBondedBalance) {
		return fmt.Errorf(
			"not-bonded pool balance %s does not equal expected balance %s",
			actual,
			expectedNotBondedBalance,
		)
	}

	lastPowers := make(map[string]int64, len(stakingGenesis.LastValidatorPowers))
	totalPower := sdkmath.ZeroInt()
	for _, lastPower := range stakingGenesis.LastValidatorPowers {
		addressBytes, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(
			lastPower.Address,
		)
		if err != nil {
			return fmt.Errorf("invalid last validator address %q: %w", lastPower.Address, err)
		}
		key := string(addressBytes)
		if _, duplicate := lastPowers[key]; duplicate {
			return fmt.Errorf("duplicate last validator power for %q", lastPower.Address)
		}
		validator, found := validators[key]
		if !found {
			return fmt.Errorf("last validator power references missing validator %q", lastPower.Address)
		}
		if !validator.IsBonded() {
			return fmt.Errorf(
				"last validator power references non-bonded validator %q",
				lastPower.Address,
			)
		}
		expectedPower := validator.ConsensusPower(sdk.DefaultPowerReduction)
		if lastPower.Power != expectedPower {
			return fmt.Errorf(
				"validator %s last power %d does not equal token-derived power %d",
				lastPower.Address,
				lastPower.Power,
				expectedPower,
			)
		}
		lastPowers[key] = lastPower.Power
		totalPower = totalPower.AddRaw(lastPower.Power)
	}
	for _, key := range validatorKeys {
		validator := validators[key]
		_, active := lastPowers[key]
		if validator.IsBonded() != active {
			return fmt.Errorf(
				"validator %s bonded status does not match the exported active set",
				validator.OperatorAddress,
			)
		}
	}
	if !stakingGenesis.LastTotalPower.Equal(totalPower) {
		return fmt.Errorf(
			"last total power %s does not equal validator power sum %s",
			stakingGenesis.LastTotalPower,
			totalPower,
		)
	}

	return nil
}

func (app *App) validateDistributionExportInvariants(genesis GenesisState) error {
	raw, ok := genesis[distrtypes.ModuleName]
	if !ok {
		return fmt.Errorf("distribution genesis is missing")
	}
	distributionGenesis := new(distrtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, distributionGenesis); err != nil {
		return fmt.Errorf("decode distribution genesis: %w", err)
	}

	raw, ok = genesis[banktypes.ModuleName]
	if !ok {
		return fmt.Errorf("bank genesis is missing")
	}
	bankGenesis := new(banktypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, bankGenesis); err != nil {
		return fmt.Errorf("decode bank genesis: %w", err)
	}

	var moduleHoldings sdk.DecCoins
	for _, rewards := range distributionGenesis.OutstandingRewards {
		moduleHoldings = moduleHoldings.Add(rewards.OutstandingRewards...)
	}
	moduleHoldings = moduleHoldings.Add(distributionGenesis.FeePool.CommunityPool...)
	expectedBalance, _ := moduleHoldings.TruncateDecimal()

	distributionAddress, err := app.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(distrtypes.ModuleName),
	)
	if err != nil {
		return fmt.Errorf("encode distribution module address: %w", err)
	}
	actualBalance := sdk.NewCoins()
	for _, balance := range bankGenesis.Balances {
		if balance.Address == distributionAddress {
			actualBalance = balance.Coins
			break
		}
	}
	if !actualBalance.Equal(expectedBalance) {
		return fmt.Errorf(
			"module balance %s does not equal community pool plus outstanding rewards %s",
			actualBalance,
			expectedBalance,
		)
	}
	return nil
}

func coinsForExportAmount(denom string, amount sdkmath.Int) sdk.Coins {
	if amount.IsZero() {
		return sdk.NewCoins()
	}
	return sdk.NewCoins(sdk.NewCoin(denom, amount))
}

func (app *App) validateZeroHeightPreflight(ctx sdk.Context, genesis GenesisState) error {
	if err := app.validateZeroHeightModuleInventory(genesis); err != nil {
		return err
	}
	if err := app.validateNoActiveIBCLifecycle(ctx, genesis); err != nil {
		return err
	}
	if err := app.validateNoActiveGovernance(genesis); err != nil {
		return err
	}
	plan, err := app.UpgradeKeeper.GetUpgradePlan(ctx)
	switch {
	case err == nil:
		return fmt.Errorf(
			"zero-height export blocked by pending software upgrade %q at height %d",
			plan.Name,
			plan.Height,
		)
	case !errors.Is(err, upgradetypes.ErrNoUpgradePlanFound):
		return fmt.Errorf("read pending software upgrade: %w", err)
	}

	return app.validateEVMHistoryContract(genesis, false)
}

var zeroHeightHandledGenesisModules = map[string]struct{}{
	"07-tendermint": {},
	"auth":          {},
	"authz":         {},
	"bank":          {},
	"consensus":     {},
	"constitution":  {},
	"distribution":  {},
	"erc20":         {},
	"evidence":      {},
	"evm":           {},
	"feegrant":      {},
	"feemarket":     {},
	"genutil":       {},
	"gov":           {},
	"ibc":           {},
	"mint":          {},
	"oracle":        {},
	"slashing":      {},
	"staking":       {},
	"transfer":      {},
	"upgrade":       {},
	"vesting":       {},
}

// Every mounted persistent store must either round-trip through the named
// genesis document or have an explicit external owner. The consensus keeper's
// store is the sole exception: canonical CometBFT consensus params are carried
// in the top-level genesis document and validated separately.
var zeroHeightPersistentStoreGenesis = map[string]string{
	"acc":          "auth",
	"authz":        "authz",
	"bank":         "bank",
	"consensus":    "",
	"constitution": "constitution",
	"distribution": "distribution",
	"erc20":        "erc20",
	"evidence":     "evidence",
	"evm":          "evm",
	"feegrant":     "feegrant",
	"feemarket":    "feemarket",
	"gov":          "gov",
	"ibc":          "ibc",
	"mint":         "mint",
	"oracle":       "oracle",
	"slashing":     "slashing",
	"staking":      "staking",
	"transfer":     "transfer",
	"upgrade":      "upgrade",
}

// validateHandledZeroHeightModules makes future persistent modules fail closed
// until their height/time semantics are explicitly added to this inventory.
func validateHandledZeroHeightModules(genesis GenesisState) error {
	undefined := make([]string, 0)
	for moduleName := range genesis {
		if _, ok := zeroHeightHandledGenesisModules[moduleName]; !ok {
			undefined = append(undefined, moduleName)
		}
	}
	if len(undefined) != 0 {
		sort.Strings(undefined)
		return fmt.Errorf("zero-height policy is undefined for modules %q", undefined)
	}
	return nil
}

func (app *App) validateZeroHeightModuleInventory(genesis GenesisState) error {
	if err := validateHandledZeroHeightModules(genesis); err != nil {
		return err
	}

	unknownModules := make([]string, 0)
	for _, moduleName := range app.ModuleManager.ModuleNames() {
		if _, ok := zeroHeightHandledGenesisModules[moduleName]; !ok {
			unknownModules = append(unknownModules, moduleName)
		}
	}
	if len(unknownModules) != 0 {
		sort.Strings(unknownModules)
		return fmt.Errorf("zero-height policy is undefined for registered modules %q", unknownModules)
	}

	mountedStores := app.GetKVStoreKeys()
	unknownStores := make([]string, 0)
	for storeName := range mountedStores {
		if _, ok := zeroHeightPersistentStoreGenesis[storeName]; !ok {
			unknownStores = append(unknownStores, storeName)
		}
	}
	if len(unknownStores) != 0 {
		sort.Strings(unknownStores)
		return fmt.Errorf("zero-height policy is undefined for persistent stores %q", unknownStores)
	}

	policyOnlyStores := make([]string, 0)
	missingDocuments := make([]string, 0)
	for storeName, genesisName := range zeroHeightPersistentStoreGenesis {
		if _, mounted := mountedStores[storeName]; !mounted {
			policyOnlyStores = append(policyOnlyStores, storeName)
			continue
		}
		if genesisName == "" {
			continue
		}
		if _, exported := genesis[genesisName]; !exported {
			missingDocuments = append(missingDocuments, genesisName)
		}
	}
	if len(policyOnlyStores) != 0 {
		sort.Strings(policyOnlyStores)
		return fmt.Errorf("zero-height persistent-store policy is stale for stores %q", policyOnlyStores)
	}
	if len(missingDocuments) != 0 {
		sort.Strings(missingDocuments)
		return fmt.Errorf(
			"zero-height export is missing genesis documents for persistent stores %q",
			missingDocuments,
		)
	}
	return nil
}

func (app *App) validateNoActiveIBCLifecycle(ctx sdk.Context, genesis GenesisState) error {
	raw, ok := genesis["ibc"]
	if !ok {
		return fmt.Errorf("ibc genesis is missing")
	}
	state := new(ibccoretypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode ibc genesis: %w", err)
	}

	clients := state.ClientGenesis
	connections := state.ConnectionGenesis
	channels := state.ChannelGenesis
	clientsV2 := state.ClientV2Genesis
	channelsV2 := state.ChannelV2Genesis

	unsafeClients := make([]string, 0)
	for _, client := range clients.Clients {
		status := app.IBCKeeper.ClientKeeper.GetClientStatus(ctx, client.ClientId)
		switch status {
		case ibcexported.Frozen, ibcexported.Expired:
			// Terminal clients are historical state and are preserved unchanged.
		case ibcexported.Active:
			unsafeClients = append(unsafeClients, client.ClientId+"(Active)")
		default:
			// Unknown, unauthorized, and future statuses cannot be classified as
			// safely terminal, so the fail-closed policy applies.
			unsafeClients = append(unsafeClients, client.ClientId+"("+string(status)+")")
		}
	}

	unsafeConnections := make([]string, 0)
	localhostCounterparty := connectiontypes.NewCounterparty(
		ibcexported.LocalhostClientID,
		ibcexported.LocalhostConnectionID,
		commitmenttypes.NewMerklePrefix(
			app.IBCKeeper.ConnectionKeeper.GetCommitmentPrefix().Bytes(),
		),
	)
	canonicalLocalhost := connectiontypes.NewIdentifiedConnection(
		ibcexported.LocalhostConnectionID,
		connectiontypes.NewConnectionEnd(
			connectiontypes.OPEN,
			ibcexported.LocalhostClientID,
			localhostCounterparty,
			connectiontypes.GetCompatibleVersions(),
			0,
		),
	)
	for _, connection := range connections.Connections {
		// IBC-Go v10 always exports its stateless localhost connection. It is
		// not external continuity state and is recreated by normal InitGenesis.
		if connection.Id == ibcexported.LocalhostConnectionID {
			if !reflect.DeepEqual(connection, canonicalLocalhost) {
				unsafeConnections = append(
					unsafeConnections,
					ibcexported.LocalhostConnectionID+"(non-canonical)",
				)
			}
			continue
		}
		unsafeConnections = append(
			unsafeConnections,
			fmt.Sprintf("%s(%s)", connection.Id, connection.State.String()),
		)
	}

	unsafeChannels := make([]string, 0)
	for _, channel := range channels.Channels {
		if channel.State == channeltypes.CLOSED {
			continue
		}
		unsafeChannels = append(
			unsafeChannels,
			fmt.Sprintf("%s/%s(%s)", channel.PortId, channel.ChannelId, channel.State.String()),
		)
	}

	sort.Strings(unsafeClients)
	sort.Strings(unsafeConnections)
	sort.Strings(unsafeChannels)
	if len(unsafeClients) != 0 || len(unsafeConnections) != 0 || len(unsafeChannels) != 0 ||
		len(channels.Commitments) != 0 || len(clientsV2.CounterpartyInfos) != 0 ||
		len(channelsV2.Commitments) != 0 || len(channelsV2.AsyncPackets) != 0 {
		return fmt.Errorf(
			"zero-height export blocked by active or unclassified IBC lifecycle state: "+
				"clients=%v connections=%v channels=%v packet_commitments=%d "+
				"v2_counterparties=%d v2_commitments=%d v2_async_packets=%d",
			unsafeClients,
			unsafeConnections,
			unsafeChannels,
			len(channels.Commitments),
			len(clientsV2.CounterpartyInfos),
			len(channelsV2.Commitments),
			len(channelsV2.AsyncPackets),
		)
	}

	return nil
}

func (app *App) validateNoActiveGovernance(genesis GenesisState) error {
	raw, ok := genesis[govtypes.ModuleName]
	if !ok {
		return fmt.Errorf("governance genesis is missing")
	}
	state := new(govv1.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode governance genesis: %w", err)
	}
	unsafe := make([]string, 0)
	for _, proposal := range state.Proposals {
		if proposal == nil {
			return fmt.Errorf("governance genesis contains a nil proposal")
		}
		switch proposal.Status {
		case govv1.StatusPassed, govv1.StatusRejected, govv1.StatusFailed:
			// Terminal proposals are preserved unchanged.
		default:
			unsafe = append(unsafe, fmt.Sprintf("%d(%s)", proposal.Id, proposal.Status.String()))
		}
	}
	if len(unsafe) != 0 {
		sort.Strings(unsafe)
		return fmt.Errorf("zero-height export blocked by active or unclassified governance proposals %v", unsafe)
	}
	return nil
}

func (app *App) validateEVMHistoryContract(genesis GenesisState, requireEmpty bool) error {
	raw, ok := genesis[evmtypes.ModuleName]
	if !ok {
		return fmt.Errorf("evm genesis is missing")
	}
	state := new(evmtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode evm genesis: %w", err)
	}
	found := false
	for _, account := range state.Accounts {
		if common.HexToAddress(account.Address) != ethparams.HistoryStorageAddress {
			continue
		}
		if found {
			return fmt.Errorf("EIP-2935 history contract account is duplicated")
		}
		found = true
		if !bytes.Equal(common.FromHex(account.Code), ethparams.HistoryStorageCode) {
			return fmt.Errorf(
				"EIP-2935 history contract code does not match the configured implementation",
			)
		}
		if requireEmpty && len(account.Storage) != 0 {
			return fmt.Errorf(
				"zero-height restart contains %d EIP-2935 history slots",
				len(account.Storage),
			)
		}
	}
	if !found {
		return fmt.Errorf("EIP-2935 history contract account is missing")
	}
	return nil
}

func (app *App) transformZeroHeightGenesis(
	genesis GenesisState,
	receipt *zeroHeightExportReceipt,
) error {
	if receipt == nil {
		return fmt.Errorf("zero-height export receipt cannot be nil")
	}

	if err := app.transformZeroHeightOracle(genesis, receipt); err != nil {
		return err
	}
	if err := app.transformZeroHeightConstitution(genesis, receipt); err != nil {
		return err
	}
	return app.transformZeroHeightEVM(genesis, receipt)
}

// transformZeroHeightEVM clears only the canonical history contract's storage.
// Decoding into a fresh value keeps the source export and committed stores intact.
func (app *App) transformZeroHeightEVM(genesis GenesisState, receipt *zeroHeightExportReceipt) error {
	if err := app.validateEVMHistoryContract(genesis, false); err != nil {
		return err
	}
	state := new(evmtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(genesis[evmtypes.ModuleName], state); err != nil {
		return err
	}
	for i := range state.Accounts {
		account := &state.Accounts[i]
		if common.HexToAddress(account.Address) == ethparams.HistoryStorageAddress {
			receipt.EVMHistorySlotsRemoved = len(account.Storage)
			account.Storage = nil
		}
	}
	encoded, err := app.AppCodec().MarshalJSON(state)
	if err != nil {
		return err
	}
	genesis[evmtypes.ModuleName] = encoded
	return nil
}

func (app *App) validateZeroHeightEVMContinuity(source, target GenesisState) error {
	if err := app.validateEVMHistoryContract(target, true); err != nil {
		return err
	}
	expected := GenesisState{evmtypes.ModuleName: source[evmtypes.ModuleName]}
	if err := app.transformZeroHeightEVM(expected, new(zeroHeightExportReceipt)); err != nil {
		return err
	}
	var expectedJSON, targetJSON interface{}
	// Compare JSON structurally without converting numeric values to float64.
	decode := func(raw []byte, value *interface{}) error {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		return decoder.Decode(value)
	}
	if err := decode(expected[evmtypes.ModuleName], &expectedJSON); err != nil {
		return err
	}
	if err := decode(target[evmtypes.ModuleName], &targetJSON); err != nil {
		return err
	}
	if !reflect.DeepEqual(expectedJSON, targetJSON) {
		return fmt.Errorf("EVM genesis changed outside the approved EIP-2935 history storage reset")
	}
	return nil
}

func (app *App) transformZeroHeightOracle(
	genesis GenesisState,
	receipt *zeroHeightExportReceipt,
) error {
	raw, ok := genesis[oracletypes.ModuleName]
	if !ok {
		return fmt.Errorf("oracle genesis is missing")
	}
	state := new(oracletypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode oracle genesis: %w", err)
	}
	for _, task := range state.Tasks {
		if task == nil {
			return fmt.Errorf("oracle genesis contains a nil task")
		}
		if task.SubmissionInterval == 0 {
			return fmt.Errorf("oracle task %q has zero submission_interval", task.Symbol)
		}
	}
	receipt.OracleTasksPreserved = len(state.Tasks)
	receipt.OracleSchedulesRemoved = len(state.TaskSchedule)
	receipt.OracleLatestValuesRemoved = len(state.LatestValues)
	receipt.OracleHistoriesRemoved = len(state.History)
	state.TaskSchedule = nil
	state.LatestValues = nil
	state.History = nil

	encoded, err := app.AppCodec().MarshalJSON(state)
	if err != nil {
		return fmt.Errorf("encode oracle genesis: %w", err)
	}
	genesis[oracletypes.ModuleName] = encoded
	return nil
}

func (app *App) transformZeroHeightConstitution(
	genesis GenesisState,
	receipt *zeroHeightExportReceipt,
) error {
	raw, ok := genesis[constitutiontypes.ModuleName]
	if !ok {
		return fmt.Errorf("constitution genesis is missing")
	}
	state := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("decode constitution genesis: %w", err)
	}
	if state.PendingMinGasPrice != nil {
		receipt.ConstitutionPendingRemoved = 1
	}
	state.PendingMinGasPrice = nil
	encoded, err := app.AppCodec().MarshalJSON(state)
	if err != nil {
		return fmt.Errorf("encode constitution genesis: %w", err)
	}
	genesis[constitutiontypes.ModuleName] = encoded
	return nil
}

func (app *App) validateZeroHeightEconomicContinuity(
	source,
	target GenesisState,
	witness zeroHeightMutationWitness,
) error {
	moduleNames := make([]string, 0, len(source))
	for moduleName := range source {
		moduleNames = append(moduleNames, moduleName)
	}
	sort.Strings(moduleNames)
	for _, moduleName := range moduleNames {
		sourceState := source[moduleName]
		sourceDocument := bytes.TrimSpace(sourceState)
		if len(sourceDocument) == 0 {
			return fmt.Errorf("source module %q genesis is empty", moduleName)
		}
		if !json.Valid(sourceDocument) {
			return fmt.Errorf("source module %q genesis is invalid JSON", moduleName)
		}
		if sourceDocument[0] != '{' {
			return fmt.Errorf("source module %q genesis must be a JSON object", moduleName)
		}
		targetState, ok := target[moduleName]
		if !ok {
			return fmt.Errorf("module %q disappeared during zero-height export", moduleName)
		}
		targetDocument := bytes.TrimSpace(targetState)
		if len(targetDocument) == 0 {
			return fmt.Errorf("target module %q genesis is empty", moduleName)
		}
		if !json.Valid(targetDocument) {
			return fmt.Errorf("target module %q genesis is invalid JSON", moduleName)
		}
		if targetDocument[0] != '{' {
			return fmt.Errorf("target module %q genesis must be a JSON object", moduleName)
		}
	}
	targetModuleNames := make([]string, 0)
	for moduleName := range target {
		if _, ok := source[moduleName]; !ok {
			targetModuleNames = append(targetModuleNames, moduleName)
		}
	}
	if len(targetModuleNames) != 0 {
		sort.Strings(targetModuleNames)
		return fmt.Errorf(
			"zero-height export introduced modules %q",
			targetModuleNames,
		)
	}

	sourceBankRaw, ok := source[banktypes.ModuleName]
	if !ok {
		return fmt.Errorf("source bank genesis is missing")
	}
	targetBankRaw, ok := target[banktypes.ModuleName]
	if !ok {
		return fmt.Errorf("target bank genesis is missing")
	}
	sourceBank := new(banktypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(sourceBankRaw, sourceBank); err != nil {
		return fmt.Errorf("decode source bank genesis: %w", err)
	}
	targetBank := new(banktypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(targetBankRaw, targetBank); err != nil {
		return fmt.Errorf("decode target bank genesis: %w", err)
	}
	if !sourceBank.Supply.Equal(targetBank.Supply) {
		return fmt.Errorf("total supply changed from %s to %s", sourceBank.Supply, targetBank.Supply)
	}
	if err := validateZeroHeightBankContinuity(sourceBank, targetBank); err != nil {
		return err
	}
	if err := app.validateZeroHeightBankMovements(
		sourceBank,
		targetBank,
		witness.RewardPayouts,
	); err != nil {
		return err
	}
	if err := app.validateZeroHeightAuthContinuity(
		source,
		target,
		sourceBank,
		targetBank,
		witness,
	); err != nil {
		return err
	}
	if _, ok := source[evmtypes.ModuleName]; !ok {
		return fmt.Errorf("source EVM genesis is missing")
	}
	if _, ok := target[evmtypes.ModuleName]; !ok {
		return fmt.Errorf("target EVM genesis is missing")
	}
	if err := app.validateZeroHeightEVMContinuity(source, target); err != nil {
		return err
	}

	sourceFeeMarketRaw, ok := source[feemarkettypes.ModuleName]
	if !ok {
		return fmt.Errorf("source feemarket genesis is missing")
	}
	targetFeeMarketRaw, ok := target[feemarkettypes.ModuleName]
	if !ok {
		return fmt.Errorf("target feemarket genesis is missing")
	}
	sourceFeeMarket := new(feemarkettypes.GenesisState)
	targetFeeMarket := new(feemarkettypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(
		sourceFeeMarketRaw,
		sourceFeeMarket,
	); err != nil {
		return fmt.Errorf("decode source feemarket genesis: %w", err)
	}
	if err := app.AppCodec().UnmarshalJSON(
		targetFeeMarketRaw,
		targetFeeMarket,
	); err != nil {
		return fmt.Errorf("decode target feemarket genesis: %w", err)
	}
	if !sourceFeeMarket.Params.MinGasPrice.Equal(targetFeeMarket.Params.MinGasPrice) {
		return fmt.Errorf(
			"current minimum gas price changed from %s to %s",
			sourceFeeMarket.Params.MinGasPrice,
			targetFeeMarket.Params.MinGasPrice,
		)
	}
	if !bytes.Equal(source[feemarkettypes.ModuleName], target[feemarkettypes.ModuleName]) {
		return fmt.Errorf("feemarket genesis changed during zero-height export")
	}
	if err := app.validateZeroHeightStakingContinuity(source, target, witness); err != nil {
		return err
	}
	if err := app.validateZeroHeightDistributionContinuity(
		source,
		target,
		witness.RewardPayouts,
	); err != nil {
		return err
	}
	if err := app.validateZeroHeightSlashingContinuity(source, target); err != nil {
		return err
	}
	if err := app.validateZeroHeightOracleContinuity(source, target); err != nil {
		return err
	}
	if err := app.validateZeroHeightConstitutionContinuity(source, target); err != nil {
		return err
	}

	mutableModules := map[string]struct{}{
		evmtypes.ModuleName:          {}, // canonical EIP-2935 history storage only
		authtypes.ModuleName:         {}, // reward payout may create a plain recipient account
		banktypes.ModuleName:         {}, // reward settlement moves balances but not supply
		distrtypes.ModuleName:        {}, // upstream zero-height reward reset
		stakingtypes.ModuleName:      {}, // upstream height/validator-set reset
		slashingtypes.ModuleName:     {}, // upstream signing-window reset
		oracletypes.ModuleName:       {}, // Guru-approved schedule/value reset
		constitutiontypes.ModuleName: {}, // Guru-approved pending MGP removal
	}
	for _, moduleName := range moduleNames {
		if _, mutable := mutableModules[moduleName]; mutable {
			continue
		}
		targetState := target[moduleName]
		if !bytes.Equal(source[moduleName], targetState) {
			return fmt.Errorf(
				"module %q genesis changed without an approved zero-height transformation",
				moduleName,
			)
		}
	}
	return nil
}

func validateZeroHeightBankContinuity(
	source,
	target *banktypes.GenesisState,
) error {
	sourceConfiguration := *source
	targetConfiguration := *target
	sourceConfiguration.Balances = nil
	targetConfiguration.Balances = nil
	sourceConfiguration.Supply = nil
	targetConfiguration.Supply = nil
	if !reflect.DeepEqual(sourceConfiguration, targetConfiguration) {
		return fmt.Errorf("bank configuration changed during zero-height export")
	}
	return nil
}

func (app *App) validateZeroHeightBankMovements(
	sourceBank,
	targetBank *banktypes.GenesisState,
	rewardPayouts map[string]sdk.Coins,
) error {
	moduleAddress := func(moduleName string) (string, error) {
		address, err := app.AccountKeeper.AddressCodec().BytesToString(
			authtypes.NewModuleAddress(moduleName),
		)
		if err != nil {
			return "", fmt.Errorf("encode %s module address: %w", moduleName, err)
		}
		return address, nil
	}
	distributionAddress, err := moduleAddress(distrtypes.ModuleName)
	if err != nil {
		return err
	}
	bondedPoolAddress, err := moduleAddress(stakingtypes.BondedPoolName)
	if err != nil {
		return err
	}
	notBondedPoolAddress, err := moduleAddress(stakingtypes.NotBondedPoolName)
	if err != nil {
		return err
	}

	sourceBalances := zeroHeightBankBalances(sourceBank)
	targetBalances := zeroHeightBankBalances(targetBank)
	expectedBalances := make(map[string]sdk.Coins, len(sourceBalances)+len(rewardPayouts))
	for address, balance := range sourceBalances {
		expectedBalances[address] = balance
	}
	rewardPayoutTotal := sdk.NewCoins()
	payoutAddresses := make([]string, 0, len(rewardPayouts))
	for address := range rewardPayouts {
		payoutAddresses = append(payoutAddresses, address)
	}
	sort.Strings(payoutAddresses)
	for _, address := range payoutAddresses {
		payout := rewardPayouts[address]
		if !payout.IsValid() || !payout.IsAllPositive() {
			return fmt.Errorf("reward payout witness for %s is not positive and valid: %s", address, payout)
		}
		expectedBalances[address] = expectedBalances[address].Add(payout...)
		rewardPayoutTotal = rewardPayoutTotal.Add(payout...)
	}
	distributionAfterPayout, negative := expectedBalances[distributionAddress].SafeSub(
		rewardPayoutTotal...,
	)
	if negative {
		return fmt.Errorf(
			"reward payout witness %s exceeds source distribution balance %s",
			rewardPayoutTotal,
			expectedBalances[distributionAddress],
		)
	}
	expectedBalances[distributionAddress] = distributionAfterPayout

	seenAddresses := make(map[string]struct{}, len(expectedBalances)+len(targetBalances))
	for address := range expectedBalances {
		seenAddresses[address] = struct{}{}
	}
	for address := range sourceBalances {
		seenAddresses[address] = struct{}{}
	}
	for address := range targetBalances {
		seenAddresses[address] = struct{}{}
	}
	addresses := make([]string, 0, len(seenAddresses))
	for address := range seenAddresses {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if address == bondedPoolAddress || address == notBondedPoolAddress {
			continue
		}
		if !expectedBalances[address].Equal(targetBalances[address]) {
			return fmt.Errorf(
				"bank balance for %s is %s; expected %s from the recorded zero-height payouts",
				address,
				targetBalances[address],
				expectedBalances[address],
			)
		}
	}
	expectedStakingPools := expectedBalances[bondedPoolAddress].Add(
		expectedBalances[notBondedPoolAddress]...,
	)
	targetStakingPools := targetBalances[bondedPoolAddress].Add(
		targetBalances[notBondedPoolAddress]...,
	)
	if !expectedStakingPools.Equal(targetStakingPools) {
		return fmt.Errorf(
			"staking pool balance group is %s; expected %s",
			targetStakingPools,
			expectedStakingPools,
		)
	}
	return nil
}

func zeroHeightBankBalances(genesis *banktypes.GenesisState) map[string]sdk.Coins {
	result := make(map[string]sdk.Coins, len(genesis.Balances))
	for _, balance := range genesis.Balances {
		result[balance.Address] = balance.Coins
	}
	return result
}

type zeroHeightAuthAccount struct {
	account authtypes.GenesisAccount
	typeURL string
	value   []byte
}

func (app *App) validateZeroHeightAuthContinuity(
	source,
	target GenesisState,
	sourceBank,
	targetBank *banktypes.GenesisState,
	witness zeroHeightMutationWitness,
) error {
	sourceState := new(authtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[authtypes.ModuleName], sourceState); err != nil {
		return fmt.Errorf("decode source auth genesis: %w", err)
	}
	targetState := new(authtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(target[authtypes.ModuleName], targetState); err != nil {
		return fmt.Errorf("decode target auth genesis: %w", err)
	}
	if !reflect.DeepEqual(sourceState.Params, targetState.Params) {
		return fmt.Errorf("auth params changed during zero-height export")
	}

	indexAccounts := func(
		label string,
		state *authtypes.GenesisState,
	) (map[string]zeroHeightAuthAccount, error) {
		accounts, err := authtypes.UnpackAccounts(state.Accounts)
		if err != nil {
			return nil, fmt.Errorf("unpack %s auth accounts: %w", label, err)
		}
		indexed := make(map[string]zeroHeightAuthAccount, len(accounts))
		for i, account := range accounts {
			address, err := app.AccountKeeper.AddressCodec().BytesToString(account.GetAddress())
			if err != nil {
				return nil, fmt.Errorf("encode %s auth account address: %w", label, err)
			}
			if _, duplicate := indexed[address]; duplicate {
				return nil, fmt.Errorf("%s auth account %s is duplicated", label, address)
			}
			indexed[address] = zeroHeightAuthAccount{
				account: account,
				typeURL: state.Accounts[i].TypeUrl,
				value:   state.Accounts[i].Value,
			}
		}
		return indexed, nil
	}

	sourceAccounts, err := indexAccounts("source", sourceState)
	if err != nil {
		return err
	}
	targetAccounts, err := indexAccounts("target", targetState)
	if err != nil {
		return err
	}

	sourceAddresses := make([]string, 0, len(sourceAccounts))
	for address := range sourceAccounts {
		sourceAddresses = append(sourceAddresses, address)
	}
	sort.Strings(sourceAddresses)
	for _, address := range sourceAddresses {
		targetAccount, ok := targetAccounts[address]
		if !ok {
			return fmt.Errorf("auth account %s disappeared during zero-height export", address)
		}
		sourceAccount := sourceAccounts[address]
		if sourceAccount.typeURL != targetAccount.typeURL ||
			!bytes.Equal(sourceAccount.value, targetAccount.value) {
			return fmt.Errorf("auth account %s changed during zero-height export", address)
		}
	}

	expectedNew := make(map[string]struct{}, len(witness.NewRewardAccounts))
	for _, address := range witness.NewRewardAccounts {
		if _, existed := sourceAccounts[address]; existed {
			return fmt.Errorf("auth witness marks existing account %s as newly created", address)
		}
		if _, duplicate := expectedNew[address]; duplicate {
			return fmt.Errorf("auth witness repeats newly created account %s", address)
		}
		expectedNew[address] = struct{}{}
	}

	targetAddresses := make([]string, 0, len(targetAccounts))
	for address := range targetAccounts {
		targetAddresses = append(targetAddresses, address)
	}
	sort.Strings(targetAddresses)
	for _, address := range targetAddresses {
		if _, existed := sourceAccounts[address]; existed {
			continue
		}
		_, ok := expectedNew[address]
		if !ok {
			return fmt.Errorf("unapproved auth account %s was created", address)
		}
		baseAccount, ok := targetAccounts[address].account.(*authtypes.BaseAccount)
		if !ok {
			return fmt.Errorf("new reward account %s has type %s; expected BaseAccount", address, targetAccounts[address].typeURL)
		}
		if baseAccount.Address != address || baseAccount.PubKey != nil || baseAccount.Sequence != 0 {
			return fmt.Errorf("new reward account %s has invalid base-account state", address)
		}
		payout := witness.RewardPayouts[address]
		if payout.IsZero() {
			return fmt.Errorf("new reward account %s has no recorded positive payout", address)
		}
	}
	for _, address := range witness.NewRewardAccounts {
		if _, ok := targetAccounts[address]; !ok {
			return fmt.Errorf("recorded reward account %s was not created", address)
		}
	}

	// The bank comparator fixes the exact payout delta; repeat the source-missing
	// boundary here so auth additions cannot be justified by a detached witness.
	sourceBalances := zeroHeightBankBalances(sourceBank)
	targetBalances := zeroHeightBankBalances(targetBank)
	for _, address := range witness.NewRewardAccounts {
		expected := sourceBalances[address].Add(witness.RewardPayouts[address]...)
		if !expected.Equal(targetBalances[address]) {
			return fmt.Errorf("new reward account %s does not match its recorded bank payout", address)
		}
	}
	return nil
}

func (app *App) validateZeroHeightDistributionContinuity(
	source,
	target GenesisState,
	rewardPayouts map[string]sdk.Coins,
) error {
	sourceState := new(distrtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[distrtypes.ModuleName], sourceState); err != nil {
		return fmt.Errorf("decode source distribution genesis: %w", err)
	}
	targetState := new(distrtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(target[distrtypes.ModuleName], targetState); err != nil {
		return fmt.Errorf("decode target distribution genesis: %w", err)
	}
	targetStaking := new(stakingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], targetStaking); err != nil {
		return fmt.Errorf("decode target staking genesis for distribution continuity: %w", err)
	}

	sourceRewardLedger := sourceState.FeePool.CommunityPool
	for _, outstanding := range sourceState.OutstandingRewards {
		sourceRewardLedger = sourceRewardLedger.Add(outstanding.OutstandingRewards...)
	}
	targetRewardLedger := targetState.FeePool.CommunityPool
	for _, outstanding := range targetState.OutstandingRewards {
		targetRewardLedger = targetRewardLedger.Add(outstanding.OutstandingRewards...)
	}
	payoutTotal := sdk.NewCoins()
	for _, payout := range rewardPayouts {
		payoutTotal = payoutTotal.Add(payout...)
	}
	targetRewardLedger = targetRewardLedger.Add(sdk.NewDecCoinsFromCoins(payoutTotal...)...)
	if !sourceRewardLedger.Equal(targetRewardLedger) {
		return fmt.Errorf(
			"distribution decimal reward ledger changed from %s to %s including payouts %s",
			sourceRewardLedger,
			targetRewardLedger,
			payoutTotal,
		)
	}
	if err := validateZeroHeightDistributionTarget(targetState, targetStaking); err != nil {
		return err
	}

	// Reward settlement and period rebuilding may change every reward record
	// and the fee pool. Configuration, withdrawal routing, and proposer identity
	// are not part of the upstream zero-height rewrite.
	sourceState.FeePool = distrtypes.FeePool{}
	targetState.FeePool = distrtypes.FeePool{}
	sourceState.OutstandingRewards = nil
	targetState.OutstandingRewards = nil
	sourceState.ValidatorAccumulatedCommissions = nil
	targetState.ValidatorAccumulatedCommissions = nil
	sourceState.ValidatorHistoricalRewards = nil
	targetState.ValidatorHistoricalRewards = nil
	sourceState.ValidatorCurrentRewards = nil
	targetState.ValidatorCurrentRewards = nil
	sourceState.DelegatorStartingInfos = nil
	targetState.DelegatorStartingInfos = nil
	sourceState.ValidatorSlashEvents = nil
	targetState.ValidatorSlashEvents = nil
	if !reflect.DeepEqual(sourceState, targetState) {
		return fmt.Errorf("distribution configuration or withdrawal routing changed during zero-height export")
	}
	return nil
}

// validateZeroHeightDistributionTarget checks the reset contract, not the SDK's
// internal period allocation or reference-count algorithm. Those are exercised
// by export/import and subsequent reward operations in the runtime tests.
func validateZeroHeightDistributionTarget(
	state *distrtypes.GenesisState,
	_ *stakingtypes.GenesisState,
) error {
	for _, record := range state.OutstandingRewards {
		if !record.OutstandingRewards.IsZero() {
			return fmt.Errorf("target distribution outstanding rewards for %s are not reset", record.ValidatorAddress)
		}
	}
	for _, record := range state.ValidatorAccumulatedCommissions {
		if !record.Accumulated.Commission.IsZero() {
			return fmt.Errorf("target distribution commissions for %s are not reset", record.ValidatorAddress)
		}
	}
	for _, record := range state.ValidatorCurrentRewards {
		if !record.Rewards.Rewards.IsZero() {
			return fmt.Errorf("target distribution current rewards for %s are not reset", record.ValidatorAddress)
		}
	}
	for _, record := range state.ValidatorHistoricalRewards {
		if !record.Rewards.CumulativeRewardRatio.IsZero() {
			return fmt.Errorf("target distribution historical rewards for %s are not reset", record.ValidatorAddress)
		}
	}
	for _, record := range state.DelegatorStartingInfos {
		if record.StartingInfo.Height != 0 {
			return fmt.Errorf("target distribution starting height for %s is not reset", record.DelegatorAddress)
		}
	}
	if len(state.ValidatorSlashEvents) != 0 {
		return fmt.Errorf("target distribution contains stale validator slash events")
	}
	return nil
}

func (app *App) validateZeroHeightStakingContinuity(
	source,
	target GenesisState,
	witness zeroHeightMutationWitness,
) error {
	sourceState := new(stakingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[stakingtypes.ModuleName], sourceState); err != nil {
		return fmt.Errorf("decode source staking genesis: %w", err)
	}
	targetState := new(stakingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], targetState); err != nil {
		return fmt.Errorf("decode target staking genesis: %w", err)
	}
	if err := validateZeroHeightStakingTarget(targetState); err != nil {
		return err
	}

	targetValidators := make(map[string]stakingtypes.Validator, len(targetState.Validators))
	for _, validator := range targetState.Validators {
		if _, duplicate := targetValidators[validator.OperatorAddress]; duplicate {
			return fmt.Errorf("target staking validator %s is duplicated", validator.OperatorAddress)
		}
		targetValidators[validator.OperatorAddress] = validator
	}
	if len(sourceState.Validators) != len(targetValidators) {
		return fmt.Errorf(
			"staking validator count changed from %d to %d",
			len(sourceState.Validators),
			len(targetValidators),
		)
	}
	for _, validator := range sourceState.Validators {
		targetValidator, ok := targetValidators[validator.OperatorAddress]
		if !ok {
			return fmt.Errorf("staking validator %s disappeared during zero-height export", validator.OperatorAddress)
		}
		expectedJailed := validator.Jailed
		if witness.JailAllowlistApplied {
			_, allowed := witness.JailAllowedValidators[validator.OperatorAddress]
			expectedJailed = validator.Jailed || !allowed
		}
		if targetValidator.Jailed != expectedJailed {
			return fmt.Errorf(
				"staking validator %s jailed state is %t; expected %t",
				validator.OperatorAddress,
				targetValidator.Jailed,
				expectedJailed,
			)
		}
		// The SDK keeper owns status transitions, completion times and unbonding
		// IDs. Check its recorded result instead of reimplementing that state machine.
		if expected, recorded := witness.StakingValidators[validator.OperatorAddress]; recorded {
			// Compare serialized protobuf fields: JSON export/import can change
			// nil slices and cached Any values without changing staking state.
			if !bytes.Equal(app.AppCodec().MustMarshal(&targetValidator), app.AppCodec().MustMarshal(&expected)) {
				return fmt.Errorf("staking validator %s differs from the upstream keeper result", validator.OperatorAddress)
			}
		} else if targetValidator.Status != validator.Status ||
			!targetValidator.UnbondingTime.Equal(validator.UnbondingTime) ||
			!reflect.DeepEqual(targetValidator.UnbondingIds, validator.UnbondingIds) {
			return fmt.Errorf("staking validator %s lifecycle changed without an upstream keeper result", validator.OperatorAddress)
		}
	}

	// Preserve validator economics and every pending delegation entry. Only the
	// SDK zero-height fields and the recorded validator-set effects may differ.
	sourceState.LastTotalPower = targetState.LastTotalPower
	sourceState.LastValidatorPowers = targetState.LastValidatorPowers
	for i := range sourceState.Validators {
		validator := &sourceState.Validators[i]
		targetValidator := targetValidators[validator.OperatorAddress]
		validator.Jailed = targetValidator.Jailed
		validator.Status = targetValidator.Status
		validator.UnbondingTime = targetValidator.UnbondingTime
		validator.UnbondingIds = targetValidator.UnbondingIds
		validator.UnbondingHeight = 0
	}
	for i := range sourceState.UnbondingDelegations {
		for j := range sourceState.UnbondingDelegations[i].Entries {
			sourceState.UnbondingDelegations[i].Entries[j].CreationHeight = 0
		}
	}
	for i := range sourceState.Redelegations {
		for j := range sourceState.Redelegations[i].Entries {
			sourceState.Redelegations[i].Entries[j].CreationHeight = 0
		}
	}
	sort.Slice(sourceState.Validators, func(i, j int) bool {
		return sourceState.Validators[i].OperatorAddress < sourceState.Validators[j].OperatorAddress
	})
	sort.Slice(targetState.Validators, func(i, j int) bool {
		return targetState.Validators[i].OperatorAddress < targetState.Validators[j].OperatorAddress
	})
	if !reflect.DeepEqual(sourceState, targetState) {
		return fmt.Errorf("staking state changed outside approved zero-height lifecycle fields")
	}
	return nil
}

func validateZeroHeightStakingTarget(state *stakingtypes.GenesisState) error {
	for _, validator := range state.Validators {
		if validator.UnbondingHeight != 0 {
			return fmt.Errorf(
				"target validator %s has nonzero unbonding height %d",
				validator.OperatorAddress,
				validator.UnbondingHeight,
			)
		}
	}
	for _, unbonding := range state.UnbondingDelegations {
		for _, entry := range unbonding.Entries {
			if entry.CreationHeight != 0 {
				return fmt.Errorf("target unbonding delegation %s/%s has nonzero creation height %d", unbonding.DelegatorAddress, unbonding.ValidatorAddress, entry.CreationHeight)
			}
		}
	}
	for _, redelegation := range state.Redelegations {
		for _, entry := range redelegation.Entries {
			if entry.CreationHeight != 0 {
				return fmt.Errorf("target redelegation %s/%s/%s has nonzero creation height %d", redelegation.DelegatorAddress, redelegation.ValidatorSrcAddress, redelegation.ValidatorDstAddress, entry.CreationHeight)
			}
		}
	}
	return nil
}

func (app *App) validateZeroHeightSlashingContinuity(source, target GenesisState) error {
	sourceState, targetState := new(slashingtypes.GenesisState), new(slashingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[slashingtypes.ModuleName], sourceState); err != nil {
		return fmt.Errorf("decode source slashing genesis: %w", err)
	}
	if err := app.AppCodec().UnmarshalJSON(target[slashingtypes.ModuleName], targetState); err != nil {
		return fmt.Errorf("decode target slashing genesis: %w", err)
	}
	sourceStaking, targetStaking := new(stakingtypes.GenesisState), new(stakingtypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[stakingtypes.ModuleName], sourceStaking); err != nil {
		return err
	}
	if err := app.AppCodec().UnmarshalJSON(target[stakingtypes.ModuleName], targetStaking); err != nil {
		return err
	}
	if err := app.validateZeroHeightSlashingTarget(targetState, targetStaking); err != nil {
		return err
	}
	if !reflect.DeepEqual(sourceState.Params, targetState.Params) {
		return fmt.Errorf("slashing state changed outside approved signing-window fields")
	}
	// Preserve each source record's tombstone and jail deadline. New records
	// are permitted only for validators bonded by the upstream transition.
	previous := make(map[string]slashingtypes.ValidatorSigningInfo, len(sourceState.SigningInfos))
	for _, record := range sourceState.SigningInfos {
		info := record.ValidatorSigningInfo
		info.StartHeight, info.IndexOffset, info.MissedBlocksCounter = 0, 0, 0
		previous[record.Address] = info
	}
	oldStatus := make(map[string]stakingtypes.BondStatus, len(sourceStaking.Validators))
	for _, validator := range sourceStaking.Validators {
		oldStatus[validator.OperatorAddress] = validator.Status
	}
	newlyBonded := make(map[string]bool)
	for _, validator := range targetStaking.Validators {
		status, existed := oldStatus[validator.OperatorAddress]
		if !existed || status == stakingtypes.Bonded || !validator.IsBonded() {
			continue
		}
		address, err := validator.GetConsAddr()
		if err != nil {
			return err
		}
		encoded, err := app.StakingKeeper.ConsensusAddressCodec().BytesToString(address)
		if err != nil {
			return err
		}
		newlyBonded[encoded] = true
	}
	for _, record := range targetState.SigningInfos {
		info := record.ValidatorSigningInfo
		if expected, existed := previous[record.Address]; existed {
			if !reflect.DeepEqual(expected, info) {
				return fmt.Errorf("slashing state changed outside approved signing-window fields")
			}
			delete(previous, record.Address)
		} else if !newlyBonded[record.Address] || info.Tombstoned || !info.JailedUntil.Equal(time.Unix(0, 0)) {
			return fmt.Errorf("target slashing state introduced an unapproved signing info %s", record.Address)
		}
	}
	if len(previous) != 0 {
		return fmt.Errorf("slashing state changed outside approved signing-window fields: source signing infos disappeared")
	}
	return nil
}

func (app *App) validateZeroHeightSlashingTarget(state *slashingtypes.GenesisState, stakingState *stakingtypes.GenesisState) error {
	signingAddresses := make(map[string]bool, len(state.SigningInfos))
	for _, record := range state.SigningInfos {
		info := record.ValidatorSigningInfo
		if signingAddresses[record.Address] || info.Address != record.Address {
			return fmt.Errorf("target signing info %s is duplicated or has a mismatched address", record.Address)
		}
		if info.StartHeight != 0 || info.IndexOffset != 0 || info.MissedBlocksCounter != 0 {
			return fmt.Errorf("target signing info %s was not reset", record.Address)
		}
		signingAddresses[record.Address] = true
	}
	for _, record := range state.MissedBlocks {
		if len(record.MissedBlocks) != 0 {
			return fmt.Errorf("target slashing missed-block window for %s is not empty", record.Address)
		}
	}
	for _, validator := range stakingState.Validators {
		if !validator.IsBonded() {
			continue
		}
		address, err := validator.GetConsAddr()
		if err != nil {
			return err
		}
		encoded, err := app.StakingKeeper.ConsensusAddressCodec().BytesToString(address)
		if err != nil {
			return err
		}
		if !signingAddresses[encoded] {
			return fmt.Errorf("target slashing state is missing signing info %s for active validator %s", encoded, validator.OperatorAddress)
		}
	}
	return nil
}

func (app *App) validateZeroHeightOracleContinuity(source, target GenesisState) error {
	sourceState := new(oracletypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[oracletypes.ModuleName], sourceState); err != nil {
		return fmt.Errorf("decode source oracle genesis: %w", err)
	}
	targetState := new(oracletypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(target[oracletypes.ModuleName], targetState); err != nil {
		return fmt.Errorf("decode target oracle genesis: %w", err)
	}
	if err := validateZeroHeightOracleTarget(targetState); err != nil {
		return err
	}
	if len(targetState.TaskSchedule) != 0 {
		return fmt.Errorf("target Oracle source schedules were not removed")
	}
	sourceState.LatestValues = nil
	targetState.LatestValues = nil
	sourceState.History = nil
	targetState.History = nil
	sourceState.TaskSchedule = nil
	targetState.TaskSchedule = nil
	if !reflect.DeepEqual(sourceState, targetState) {
		return fmt.Errorf("oracle params or task definitions changed during zero-height export")
	}
	return nil
}

func validateZeroHeightOracleTarget(state *oracletypes.GenesisState) error {
	if len(state.LatestValues) != 0 || len(state.History) != 0 {
		return fmt.Errorf("target Oracle observations were not removed")
	}
	return nil
}

func (app *App) validateZeroHeightConstitutionContinuity(source, target GenesisState) error {
	sourceState := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(source[constitutiontypes.ModuleName], sourceState); err != nil {
		return fmt.Errorf("decode source constitution genesis: %w", err)
	}
	targetState := new(constitutiontypes.GenesisState)
	if err := app.AppCodec().UnmarshalJSON(target[constitutiontypes.ModuleName], targetState); err != nil {
		return fmt.Errorf("decode target constitution genesis: %w", err)
	}
	if err := validateZeroHeightConstitutionTarget(targetState); err != nil {
		return err
	}
	sourceState.PendingMinGasPrice = nil
	targetState.PendingMinGasPrice = nil
	if !reflect.DeepEqual(sourceState, targetState) {
		return fmt.Errorf("constitution state changed outside pending minimum gas price")
	}
	return nil
}

func validateZeroHeightConstitutionTarget(state *constitutiontypes.GenesisState) error {
	if state.PendingMinGasPrice != nil {
		return fmt.Errorf("target Constitution pending minimum gas price was not removed")
	}
	return nil
}
