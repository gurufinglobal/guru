package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateWithParams performs validation on OracleRequestDoc with parameter-based limits
func (doc OracleRequestDoc) ValidateWithParams(params Params) error {
	// Check if oracle type is unspecified
	if doc.OracleType == OracleType_ORACLE_TYPE_UNSPECIFIED {
		return fmt.Errorf("oracle type cannot be unspecified")
	}
	// Check if oracle type is zero (empty)
	if doc.OracleType == 0 {
		return fmt.Errorf("oracle type cannot be empty")
	}
	// Check if name is empty
	if doc.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	// Check if endpoints is empty
	if doc.Endpoints == nil {
		return fmt.Errorf("endpoints cannot be empty")
	}
	// Check if endpoints is empty
	if len(doc.Endpoints) == 0 {
		return fmt.Errorf("endpoints cannot be empty")
	}
	// Check if aggregation rule is unspecified
	if doc.AggregationRule == AggregationRule_AGGREGATION_RULE_UNSPECIFIED {
		return fmt.Errorf("aggregation rule cannot be unspecified")
	}
	// Check if aggregation rule is empty
	if doc.AggregationRule == 0 {
		return fmt.Errorf("aggregation rule cannot be empty")
	}
	// Check if account list is nil
	if doc.AccountList == nil {
		return fmt.Errorf("account list cannot be empty")
	}
	// Check if account list is empty
	if len(doc.AccountList) == 0 {
		return fmt.Errorf("account list cannot be empty")
	}

	// Safety check: prevent DoS attacks with too many accounts (parameter-based)
	if uint64(len(doc.AccountList)) > params.MaxAccountListSize {
		return fmt.Errorf("account list size exceeds maximum allowed: %d, maximum: %d", len(doc.AccountList), params.MaxAccountListSize)
	}

	// Validate each account in the account list
	for _, account := range doc.AccountList {
		if _, err := sdk.AccAddressFromBech32(account); err != nil {
			return fmt.Errorf("account address is not valid bech32: %v", err)
		}
	}
	// Check if quorum is zero
	if doc.Quorum == 0 {
		return fmt.Errorf("quorum cannot be 0")
	}
	// Check if quorum is greater than the length of the account list
	if doc.Quorum > uint32(len(doc.AccountList)) {
		return fmt.Errorf("quorum cannot be greater than account list length")
	}

	// Safety check: prevent excessive quorum that could lead to performance issues (parameter-based)
	if uint64(doc.Quorum) > params.MaxAccountListSize {
		return fmt.Errorf("quorum exceeds maximum allowed: %d, maximum: %d", doc.Quorum, params.MaxAccountListSize)
	}

	// Check if status is unspecified
	if doc.Status == RequestStatus_REQUEST_STATUS_UNSPECIFIED {
		return fmt.Errorf("status cannot be unspecified")
	}
	// Check if status is empty
	if doc.Status == 0 {
		return fmt.Errorf("status cannot be empty")
	}

	return nil
}

// Validate performs basic validation on OracleRequestDoc with default limits
func (doc OracleRequestDoc) Validate() error {
	// Use default parameters for validation
	defaultParams := DefaultParams()
	return doc.ValidateWithParams(defaultParams)
}
