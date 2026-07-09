package abci

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	"google.golang.org/protobuf/proto"
)

func DecodeVoteExtension(bz []byte) (*oraclev1.OracleVoteExtension, error) {
	extension := &oraclev1.OracleVoteExtension{}
	if err := proto.Unmarshal(bz, extension); err != nil {
		return nil, err
	}
	if err := ValidateVoteExtension(extension); err != nil {
		return nil, err
	}

	return extension, nil
}

func ValidateVoteExtension(extension *oraclev1.OracleVoteExtension) error {
	if extension == nil {
		return fmt.Errorf("oracle vote extension cannot be nil")
	}

	seen := map[string]struct{}{}
	for _, result := range extension.GetResults() {
		symbol := oraclekeeper.NormalizeSymbol(result.GetSymbol())
		if symbol == "" {
			return fmt.Errorf("oracle vote extension result symbol cannot be empty")
		}
		if _, ok := seen[symbol]; ok {
			return fmt.Errorf("duplicate oracle vote extension result for %q", symbol)
		}
		seen[symbol] = struct{}{}

		if result.GetValueType() == oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED {
			return fmt.Errorf("oracle vote extension result value_type cannot be unspecified")
		}
		if result.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
			return fmt.Errorf("oracle vote extension result non-numeric value_type is not supported")
		}
		if result.GetValue() == "" {
			return fmt.Errorf("oracle vote extension result value cannot be empty")
		}
		if result.GetSourceCount() == 0 {
			return fmt.Errorf("oracle vote extension result source_count must be positive")
		}
		if _, err := sdkmath.LegacyNewDecFromStr(result.GetValue()); err != nil {
			return fmt.Errorf("invalid oracle vote extension numeric value: %w", err)
		}
	}

	return nil
}
