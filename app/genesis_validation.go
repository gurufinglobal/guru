package app

import (
	"bytes"
	"encoding/json"
	"fmt"

	sdkmath "cosmossdk.io/math"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

// ValidateChainGenesis validates chain-level and cross-module invariants only.
// Module-level schema/default validation is owned by each module's ValidateGenesis.
func (app *App) ValidateChainGenesis(genesis map[string]json.RawMessage) error {
	if err := app.BasicModuleManager.ValidateGenesis(app.appCodec, app.txConfig, genesis); err != nil {
		return fmt.Errorf("module validation failed: %w", err)
	}

	bankGenesis := banktypes.DefaultGenesisState()
	if bz, ok := genesis[banktypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, bankGenesis); err != nil {
			return fmt.Errorf("failed to decode bank genesis: %w", err)
		}
	}
	if err := validateBaseDenomMetadata(bankGenesis.DenomMetadata); err != nil {
		return fmt.Errorf("invalid bank genesis metadata: %w", err)
	}
	for _, balance := range bankGenesis.Balances {
		if err := validateCoinsDenom(balance.Coins, "", false); err != nil {
			return fmt.Errorf("invalid bank balance for %s: %w", balance.Address, err)
		}
	}
	if err := validateCoinsDenom(bankGenesis.Supply, "", false); err != nil {
		return fmt.Errorf("invalid bank supply: %w", err)
	}

	stakingGenesis := stakingtypes.DefaultGenesisState()
	if bz, ok := genesis[stakingtypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, stakingGenesis); err != nil {
			return fmt.Errorf("failed to decode staking genesis: %w", err)
		}
	}
	if stakingGenesis.Params.BondDenom != appparams.BaseDenom {
		return fmt.Errorf("staking bond denom must be %q, got %q", appparams.BaseDenom, stakingGenesis.Params.BondDenom)
	}

	constitutionGenesis := &constitutionv1.GenesisState{}
	if bz, ok := genesis[constitutiontypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, constitutionGenesis); err != nil {
			return fmt.Errorf("failed to decode constitution genesis: %w", err)
		}
	}
	if err := app.validateGenesisValidatorSelfBonds(stakingGenesis, constitutionGenesis); err != nil {
		return err
	}

	mintGenesis := minttypes.DefaultGenesisState()
	if bz, ok := genesis[minttypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, mintGenesis); err != nil {
			return fmt.Errorf("failed to decode mint genesis: %w", err)
		}
	}
	if mintGenesis.Params.MintDenom != appparams.BaseDenom {
		return fmt.Errorf("mint denom must be %q, got %q", appparams.BaseDenom, mintGenesis.Params.MintDenom)
	}

	govGenesis := govv1.DefaultGenesisState()
	if bz, ok := genesis[govtypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, govGenesis); err != nil {
			return fmt.Errorf("failed to decode gov genesis: %w", err)
		}
	}
	if govGenesis.Params == nil {
		return fmt.Errorf("gov params cannot be nil")
	}
	if err := validateCoinsDenom(govGenesis.Params.MinDeposit, appparams.BaseDenom, true); err != nil {
		return fmt.Errorf("invalid gov min_deposit: %w", err)
	}
	if err := validateCoinsDenom(govGenesis.Params.ExpeditedMinDeposit, appparams.BaseDenom, true); err != nil {
		return fmt.Errorf("invalid gov expedited_min_deposit: %w", err)
	}
	minDepositAmt := amountOfDenom(govGenesis.Params.MinDeposit, appparams.BaseDenom)
	expeditedDepositAmt := amountOfDenom(govGenesis.Params.ExpeditedMinDeposit, appparams.BaseDenom)
	if !expeditedDepositAmt.GT(minDepositAmt) {
		return fmt.Errorf(
			"gov expedited_min_deposit must be greater than min_deposit (got %s <= %s)",
			expeditedDepositAmt.String(),
			minDepositAmt.String(),
		)
	}

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	if bz, ok := genesis[feemarkettypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, feeMarketGenesis); err != nil {
			return fmt.Errorf("failed to decode feemarket genesis: %w", err)
		}
	}
	if !feeMarketGenesis.Params.NoBaseFee {
		return fmt.Errorf("feemarket no_base_fee must be true")
	}
	if !feeMarketGenesis.Params.BaseFee.IsZero() {
		return fmt.Errorf("feemarket base_fee must be zero, got %s", feeMarketGenesis.Params.BaseFee.String())
	}
	if !feeMarketGenesis.Params.MinGasPrice.IsPositive() {
		return fmt.Errorf("feemarket min_gas_price must be positive, got %s", feeMarketGenesis.Params.MinGasPrice.String())
	}

	evmGenesis := evmtypes.DefaultGenesisState()
	if bz, ok := genesis[evmtypes.ModuleName]; ok {
		if err := app.appCodec.UnmarshalJSON(bz, evmGenesis); err != nil {
			return fmt.Errorf("failed to decode evm genesis: %w", err)
		}
	}
	if evmGenesis.Params.EvmDenom != appparams.BaseDenom {
		return fmt.Errorf("evm denom must be %q, got %q", appparams.BaseDenom, evmGenesis.Params.EvmDenom)
	}
	if evmGenesis.Params.ExtendedDenomOptions == nil {
		return fmt.Errorf("evm extended denom options cannot be nil")
	}
	if evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom != appparams.BaseDenom {
		return fmt.Errorf(
			"evm extended denom must be %q, got %q",
			appparams.BaseDenom,
			evmGenesis.Params.ExtendedDenomOptions.ExtendedDenom,
		)
	}

	return nil
}

func (app *App) validateGenesisValidatorSelfBonds(
	stakingGenesis *stakingtypes.GenesisState,
	constitutionGenesis *constitutionv1.GenesisState,
) error {
	if stakingGenesis == nil || constitutionGenesis == nil {
		return nil
	}

	params := constitutionGenesis.GetParams()
	if params == nil {
		defaultConstitutionGenesis, err := app.defaultConstitutionGenesis()
		if err != nil {
			return err
		}
		params = defaultConstitutionGenesis.GetParams()
	}
	minBondCoin := params.GetMinValidatorBondAmount()
	if minBondCoin == nil {
		return fmt.Errorf("constitution min_validator_bond_amount cannot be nil")
	}
	if minBondCoin.Denom != appparams.BaseDenom {
		return fmt.Errorf(
			"constitution min_validator_bond_amount denom must be %q, got %q",
			appparams.BaseDenom,
			minBondCoin.Denom,
		)
	}
	minBond, ok := sdkmath.NewIntFromString(minBondCoin.Amount)
	if !ok {
		return fmt.Errorf("constitution min_validator_bond_amount amount must be an integer string")
	}

	activeExportedValidators, err := app.activeExportedGenesisValidators(stakingGenesis)
	if err != nil {
		return err
	}
	for _, validator := range stakingGenesis.Validators {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
		if err != nil {
			return fmt.Errorf("invalid genesis validator address %s: %w", validator.GetOperator(), err)
		}
		if activeExportedValidators != nil {
			if _, ok := activeExportedValidators[string(validatorAddr)]; !ok {
				continue
			}
		}

		selfBond, err := app.genesisValidatorSelfBond(stakingGenesis.Delegations, validator, validatorAddr)
		if err != nil {
			return err
		}
		if selfBond.LT(minBond) {
			return fmt.Errorf(
				"validator %s genesis self-bond %s below constitution minimum %s",
				validator.GetOperator(),
				selfBond.String(),
				minBond.String(),
			)
		}
	}

	return nil
}

func (app *App) defaultConstitutionGenesis() (*constitutionv1.GenesisState, error) {
	defaultGenesis := app.BasicModuleManager.DefaultGenesis(app.appCodec)
	bz, ok := defaultGenesis[constitutiontypes.ModuleName]
	if !ok {
		return nil, fmt.Errorf("constitution default genesis missing")
	}

	constitutionGenesis := &constitutionv1.GenesisState{}
	if err := app.appCodec.UnmarshalJSON(bz, constitutionGenesis); err != nil {
		return nil, fmt.Errorf("failed to decode constitution default genesis: %w", err)
	}

	return constitutionGenesis, nil
}

func (app *App) activeExportedGenesisValidators(stakingGenesis *stakingtypes.GenesisState) (map[string]struct{}, error) {
	if !stakingGenesis.GetExported() {
		return nil, nil
	}

	activeValidators := make(map[string]struct{}, len(stakingGenesis.GetLastValidatorPowers()))
	for _, lastValidatorPower := range stakingGenesis.GetLastValidatorPowers() {
		validatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(lastValidatorPower.Address)
		if err != nil {
			return nil, fmt.Errorf("invalid exported last validator address %s: %w", lastValidatorPower.Address, err)
		}
		activeValidators[string(validatorAddr)] = struct{}{}
	}

	return activeValidators, nil
}

func (app *App) genesisValidatorSelfBond(
	delegations []stakingtypes.Delegation,
	validator stakingtypes.Validator,
	validatorAddr []byte,
) (sdkmath.Int, error) {
	selfBond := sdkmath.ZeroInt()
	selfDelegationFound := false
	for _, delegation := range delegations {
		delegationValidatorAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(delegation.ValidatorAddress)
		if err != nil {
			return sdkmath.Int{}, fmt.Errorf("invalid genesis delegation validator address %s: %w", delegation.ValidatorAddress, err)
		}
		if !bytes.Equal(delegationValidatorAddr, validatorAddr) {
			continue
		}

		delegatorAddr, err := app.AccountKeeper.AddressCodec().StringToBytes(delegation.DelegatorAddress)
		if err != nil {
			return sdkmath.Int{}, fmt.Errorf("invalid genesis delegation delegator address %s: %w", delegation.DelegatorAddress, err)
		}
		if !bytes.Equal(delegatorAddr, validatorAddr) {
			continue
		}

		if selfDelegationFound {
			return sdkmath.Int{}, fmt.Errorf("duplicate genesis self-delegation for validator %s", validator.GetOperator())
		}
		selfDelegationFound = true
		selfBond = selfBond.Add(validator.TokensFromSharesTruncated(delegation.GetShares()).TruncateInt())
	}

	return selfBond, nil
}
