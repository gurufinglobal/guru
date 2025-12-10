package types

import (
	"fmt"

	"cosmossdk.io/math"
)

var (
	// DefaultQuorumRatio is the default quorum ratio (2/3).
	DefaultQuorumRatio = math.LegacyNewDecWithPrec(666666666666666667, 18)
	// DefaultReportRetentionBlocks is the default number of blocks to retain reports (1000).
	DefaultReportRetentionBlocks uint64 = 1000
)

// DefaultParams returns default oracle parameters.
func DefaultParams() Params {
	return Params{
		Enable:                true,
		QuorumRatio:           DefaultQuorumRatio,
		ReportRetentionBlocks: DefaultReportRetentionBlocks,
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.QuorumRatio.IsNegative() {
		return fmt.Errorf("quorum_ratio cannot be negative: %s", p.QuorumRatio)
	}
	if p.QuorumRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("quorum_ratio cannot exceed 1: %s", p.QuorumRatio)
	}
	if p.QuorumRatio.IsZero() {
		return fmt.Errorf("quorum_ratio cannot be zero")
	}
	return nil
}

// ValidateBasic performs basic validation on using the struct methods.
func (p Params) ValidateBasic() error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}
