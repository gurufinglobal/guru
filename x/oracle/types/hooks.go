package types

import (
	"context"
)

type OracleHooks interface {
	// AfterOracleValueApplied runs after an oracle aggregate is written to
	// oracle state. Hook implementers may derive their own module state from
	// the value, but oracle remains the owner of aggregation and task cadence.
	AfterOracleValueApplied(ctx context.Context, value *OracleValue, sourceSubmissionInterval uint32) error
}

type MultiOracleHooks []OracleHooks

func NewMultiOracleHooks(hooks ...OracleHooks) MultiOracleHooks {
	return hooks
}

func (h MultiOracleHooks) AfterOracleValueApplied(ctx context.Context, value *OracleValue, sourceSubmissionInterval uint32) error {
	for _, hook := range h {
		if hook == nil {
			continue
		}
		if err := hook.AfterOracleValueApplied(ctx, value, sourceSubmissionInterval); err != nil {
			return err
		}
	}

	return nil
}
