package types

import (
	fmt "fmt"

	storetypes "cosmossdk.io/store/types"
)

// fixedGasMeter is a deterministic gas meter that always reports a fixed gas
// usage regardless of ConsumeGas/RefundGas calls.
type fixedGasMeter struct {
	fixed storetypes.Gas
}

// NewFixedGasMeter returns a gas meter that always reports `fixed` for both
// gas consumed and gas limit.
func NewFixedGasMeter(fixed storetypes.Gas) storetypes.GasMeter {
	return &fixedGasMeter{fixed: fixed}
}

func (g *fixedGasMeter) GasConsumed() storetypes.Gas {
	return g.fixed
}

func (g *fixedGasMeter) GasConsumedToLimit() storetypes.Gas {
	return g.fixed
}

func (g *fixedGasMeter) Limit() storetypes.Gas {
	return g.fixed
}

// ConsumeGas is intentionally a no-op to keep gas usage fixed.
func (g *fixedGasMeter) ConsumeGas(_ storetypes.Gas, _ string) {}

// RefundGas is intentionally a no-op to keep gas usage fixed.
func (g *fixedGasMeter) RefundGas(_ storetypes.Gas, _ string) {}

func (g *fixedGasMeter) IsPastLimit() bool {
	return false
}

func (g *fixedGasMeter) IsOutOfGas() bool {
	return false
}

func (g *fixedGasMeter) String() string {
	return fmt.Sprintf("FixedGasMeter:\n  fixed: %d", g.fixed)
}

func (g *fixedGasMeter) GasRemaining() storetypes.Gas {
	return 0
}
