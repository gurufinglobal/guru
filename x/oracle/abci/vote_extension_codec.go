package abci

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
	oraclekeeper "github.com/gurufinglobal/guru/v2/x/oracle/keeper"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func DecodeVoteExtension(bz []byte) (*oraclev1.OracleVoteExtension, error) {
	extension := &oraclev1.OracleVoteExtension{}
	if err := extension.Unmarshal(bz); err != nil {
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
	if len(extension.GetResults()) > maxSidecarSymbols {
		return fmt.Errorf("oracle vote extension has too many results")
	}

	seen := map[string]struct{}{}
	for _, result := range extension.GetResults() {
		symbol := oraclekeeper.NormalizeSymbol(result.GetSymbol())
		if symbol == "" {
			return fmt.Errorf("oracle vote extension result symbol cannot be empty")
		}
		if result.GetSymbol() != symbol || len(symbol) > maxSidecarSymbolBytes {
			return fmt.Errorf("oracle vote extension result symbol is not canonical")
		}
		if _, ok := seen[symbol]; ok {
			return fmt.Errorf("duplicate oracle vote extension result for %q", symbol)
		}
		seen[symbol] = struct{}{}

		if result.GetValue() == "" {
			return fmt.Errorf("oracle vote extension result value cannot be empty")
		}
		if len(result.GetValue()) > maxSidecarValueBytes {
			return fmt.Errorf("oracle vote extension result value is too long")
		}
		if result.GetSourceCount() == 0 {
			return fmt.Errorf("oracle vote extension result source_count must be positive")
		}
		value, err := sdkmath.LegacyNewDecFromStr(result.GetValue())
		if err != nil {
			return fmt.Errorf("invalid oracle vote extension numeric value: %w", err)
		}
		if value.String() != result.GetValue() {
			return fmt.Errorf("oracle vote extension numeric value is not canonical")
		}
	}

	return nil
}
