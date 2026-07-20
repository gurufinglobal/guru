package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
)

const SwapProtectionMemoKey = "transwap"

// SwapProtection contains optional execution constraints supplied in the
// transwap memo namespace. Presence is tracked independently so callers may
// enforce either constraint without imposing the other.
type SwapProtection struct {
	MinAmountOut                sdkmath.Int
	ExpectedExchangeRevision    uint64
	HasMinAmountOut             bool
	HasExpectedExchangeRevision bool
}

// ParseSwapProtection parses the optional memo form:
//
//	{"transwap":{"min_amount_out":"1000","expected_exchange_revision":"12"}}
//
// Non-JSON memos and JSON objects without the transwap namespace do not enable
// protection. Once the namespace is present, its schema is intentionally
// strict so malformed protection cannot be mistaken for an unprotected swap.
func ParseSwapProtection(memo string) (SwapProtection, error) {
	var protection SwapProtection
	trimmed := strings.TrimSpace(memo)
	if trimmed == "" || trimmed[0] != '{' {
		return protection, nil
	}

	outer, duplicateOuter, err := decodeJSONObject([]byte(trimmed))
	if err != nil {
		return protection, errorsmod.Wrapf(ErrInvalidMemo, "invalid JSON memo: %v", err)
	}
	for _, field := range duplicateOuter {
		if field == SwapProtectionMemoKey {
			return protection, errorsmod.Wrap(ErrInvalidSwapProtection, "duplicate transwap memo namespace")
		}
	}
	rawNamespace, found := outer[SwapProtectionMemoKey]
	if !found {
		return protection, nil
	}

	fields, duplicateFields, err := decodeJSONObject(rawNamespace)
	if err != nil {
		return protection, errorsmod.Wrap(ErrInvalidSwapProtection, "transwap memo value must be an object")
	}
	if len(duplicateFields) > 0 {
		return protection, errorsmod.Wrapf(ErrInvalidSwapProtection, "duplicate transwap protection field %q", duplicateFields[0])
	}
	unknownFields := make([]string, 0)
	for field := range fields {
		if field != "min_amount_out" && field != "expected_exchange_revision" {
			unknownFields = append(unknownFields, field)
		}
	}
	if len(unknownFields) > 0 {
		sort.Strings(unknownFields)
		return protection, errorsmod.Wrapf(ErrInvalidSwapProtection, "unknown transwap protection field %q", unknownFields[0])
	}

	if rawValue, found := fields["min_amount_out"]; found {
		value, err := parseProtectionString("min_amount_out", rawValue)
		if err != nil {
			return SwapProtection{}, err
		}
		amount, err := uint256decimal.ParseCanonicalPositive(value)
		if err != nil {
			return SwapProtection{}, errorsmod.Wrapf(ErrInvalidSwapProtection, "min_amount_out must be a canonical positive uint256 string: %v", err)
		}
		protection.MinAmountOut = amount
		protection.HasMinAmountOut = true
	}

	if rawValue, found := fields["expected_exchange_revision"]; found {
		value, err := parseProtectionString("expected_exchange_revision", rawValue)
		if err != nil {
			return SwapProtection{}, err
		}
		revision, err := strconv.ParseUint(value, 10, 64)
		if err != nil || revision == 0 || strconv.FormatUint(revision, 10) != value {
			return SwapProtection{}, errorsmod.Wrap(ErrInvalidSwapProtection, "expected_exchange_revision must be a canonical positive uint64 string")
		}
		protection.ExpectedExchangeRevision = revision
		protection.HasExpectedExchangeRevision = true
	}

	return protection, nil
}

func parseProtectionString(field string, raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value string
	if err := decoder.Decode(&value); err != nil {
		return "", errorsmod.Wrapf(ErrInvalidSwapProtection, "%s must be a JSON string", field)
	}
	return value, nil
}

// decodeJSONObject preserves duplicate-key information that encoding/json's
// map unmarshalling would otherwise discard using last-value-wins semantics.
func decodeJSONObject(raw []byte) (map[string]json.RawMessage, []string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	delim, ok := opening.(json.Delim)
	if !ok || delim != '{' {
		return nil, nil, fmt.Errorf("expected JSON object")
	}

	fields := make(map[string]json.RawMessage)
	duplicateSet := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected JSON object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, err
		}
		if _, found := fields[key]; found {
			duplicateSet[key] = struct{}{}
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	if closingDelim, ok := closing.(json.Delim); !ok || closingDelim != '}' {
		return nil, nil, fmt.Errorf("expected end of JSON object")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, nil, err
	}

	duplicates := make([]string, 0, len(duplicateSet))
	for key := range duplicateSet {
		duplicates = append(duplicates, key)
	}
	sort.Strings(duplicates)
	return fields, duplicates, nil
}
