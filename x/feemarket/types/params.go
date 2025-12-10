package types

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/params"
)

var (
	// DefaultBaseFee for the Cosmos EVM chain
	DefaultBaseFee = math.LegacyNewDec(1_000_000_000)
	// DefaultMinGasMultiplier is 0.5 or 50%
	DefaultMinGasMultiplier = math.LegacyNewDecWithPrec(50, 2)
	// DefaultMinGasPrice is 0 (i.e disabled)
	DefaultMinGasPrice = math.LegacyZeroDec()
	// DefaultMinGasPriceRate is 1.0 (i.e no adjustment)
	DefaultMinGasPriceRate = math.LegacyOneDec()
	// DefaultEnableHeight is 0 (i.e disabled)
	DefaultEnableHeight = int64(0)
	// DefaultNoBaseFee is false
	DefaultNoBaseFee = false

	ParamsKey = []byte("Params")
)

// NewParams creates a new Params instance
func NewParams(
	noBaseFee bool,
	baseFeeChangeDenom,
	elasticityMultiplier uint32,
	baseFee math.LegacyDec,
	enableHeight int64,
	minGasPrice math.LegacyDec,
	minGasPriceMultiplier math.LegacyDec,
	gasPriceAdjustmentFactor math.LegacyDec,
	maxChangeRate math.LegacyDec,
) Params {
	return Params{
		NoBaseFee:                noBaseFee,
		BaseFeeChangeDenominator: baseFeeChangeDenom,
		ElasticityMultiplier:     elasticityMultiplier,
		BaseFee:                  baseFee,
		EnableHeight:             enableHeight,
		MinGasPrice:              minGasPrice,
		MinGasMultiplier:         minGasPriceMultiplier,
		GasPriceAdjustmentFactor: gasPriceAdjustmentFactor,
		MaxChangeRate:            maxChangeRate,
	}
}

// DefaultParams returns default evm parameters
func DefaultParams() Params {
	return Params{
		NoBaseFee:                DefaultNoBaseFee,
		BaseFeeChangeDenominator: params.DefaultBaseFeeChangeDenominator,
		ElasticityMultiplier:     params.DefaultElasticityMultiplier,
		BaseFee:                  DefaultBaseFee,
		EnableHeight:             DefaultEnableHeight,
		MinGasPrice:              DefaultMinGasPrice,
		MinGasMultiplier:         DefaultMinGasMultiplier,
		GasPriceAdjustmentFactor: DefaultMinGasPriceRate,
		MaxChangeRate:            DefaultMinGasPriceRate,
	}
}

// Validate performs basic validation on fee market parameters.
func (p Params) Validate() error {
	if p.BaseFeeChangeDenominator == 0 {
		return fmt.Errorf("base fee change denominator cannot be 0")
	}

	if p.BaseFee.IsNegative() {
		return fmt.Errorf("initial base fee cannot be negative: %s", p.BaseFee)
	}

	if p.EnableHeight < 0 {
		return fmt.Errorf("enable height cannot be negative: %d", p.EnableHeight)
	}

	if p.ElasticityMultiplier == 0 {
		return fmt.Errorf("elasticity multiplier cannot be zero: %d", p.ElasticityMultiplier)
	}

	if err := validateMinGasMultiplier(p.MinGasMultiplier); err != nil {
		return err
	}

	if err := validateMinGasPrice(p.MinGasPrice); err != nil {
		return err
	}

	return validateGasPriceAdjustmentFactor(p.GasPriceAdjustmentFactor)
}

func (p *Params) IsBaseFeeEnabled(height int64) bool {
	return !p.NoBaseFee && height >= p.EnableHeight
}

func validateMinGasPrice(gasPrice math.LegacyDec) error {
	if gasPrice.IsNil() {
		return fmt.Errorf("invalid parameter: nil")
	}

	if gasPrice.IsNegative() {
		return fmt.Errorf("value cannot be negative: %s", gasPrice)
	}

	return nil
}

func validateMinGasMultiplier(multiplier math.LegacyDec) error {
	if multiplier.IsNil() {
		return fmt.Errorf("invalid parameter: nil")
	}

	if multiplier.IsNegative() {
		return fmt.Errorf("value cannot be negative: %s", multiplier)
	}

	if multiplier.GT(math.LegacyOneDec()) {
		return fmt.Errorf("value cannot be greater than 1: %s", multiplier)
	}

	return nil
}

func validateGasPriceAdjustmentFactor(rate math.LegacyDec) error {
	if rate.IsNil() {
		return fmt.Errorf("invalid parameter: nil")
	}

	if rate.IsNegative() {
		return fmt.Errorf("value cannot be negative: %s", rate)
	}

	return nil
}
