package types

import (
	"context"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
)

type OracleHooks interface {
	// AfterOracleValueApplied runs after an oracle aggregate is written to
	// oracle state. Hook implementers may derive their own module state from
	// the value, but oracle remains the owner of aggregation and task cadence.
	AfterOracleValueApplied(ctx context.Context, value *oraclev1.OracleValue, sourceSubmissionInterval uint32) error
}

type MultiOracleHooks []OracleHooks

func NewMultiOracleHooks(hooks ...OracleHooks) MultiOracleHooks {
	return hooks
}

func (h MultiOracleHooks) AfterOracleValueApplied(ctx context.Context, value *oraclev1.OracleValue, sourceSubmissionInterval uint32) error {
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
