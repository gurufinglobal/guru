package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
)

const (
	// SwapProtectionMemoStem reserves all versioned TransSwap protection memos.
	SwapProtectionMemoStem = "guru.transwap.protection:"
	// SwapProtectionMemoMarkerV1 identifies the strict v1 protection envelope.
	SwapProtectionMemoMarkerV1 = SwapProtectionMemoStem + "v1:"

	// SwapProtectionMemoKey is the deprecated top-level JSON namespace. It is
	// retained only so the parser can reject the legacy, downgrade-prone form.
	SwapProtectionMemoKey = "transwap"
)

// SwapProtection contains optional execution constraints supplied in the
// versioned TransSwap protection memo. Presence is tracked independently so
// callers may enforce either constraint without imposing the other.
type SwapProtection struct {
	MinAmountOut                sdkmath.Int
	ExpectedExchangeRevision    uint64
	HasMinAmountOut             bool
	HasExpectedExchangeRevision bool
}

// SwapProtectionMemoState distinguishes an ordinary memo, a valid protection
// intent, and a malformed protection intent that must fail closed.
type SwapProtectionMemoState uint8

const (
	SwapProtectionNoIntent SwapProtectionMemoState = iota
	SwapProtectionValidIntent
	SwapProtectionMalformedIntent
)

// SwapProtectionMemoResult is the explicit three-state parse result.
type SwapProtectionMemoResult struct {
	State      SwapProtectionMemoState
	Protection SwapProtection
}

// ParseSwapProtection preserves the existing caller API while delegating to
// the explicit three-state parser. A malformed intent always returns an error.
func ParseSwapProtection(memo string) (SwapProtection, error) {
	result, err := ParseSwapProtectionMemo(memo)
	return result.Protection, err
}

// ParseSwapProtectionMemo parses the optional, domain-separated memo form:
//
//	guru.transwap.protection:v1:{"min_amount_out":"1000","expected_exchange_revision":"12"}
//
// Memos without the reserved stem have no protection intent and are otherwise
// opaque. Once the stem is present, the marker must start at byte offset zero,
// use a supported version, and contain exactly one strict JSON object. The
// deprecated top-level {"transwap": ...} form is rejected rather than silently
// downgraded to an unprotected exchange.
func ParseSwapProtectionMemo(memo string) (SwapProtectionMemoResult, error) {
	result := SwapProtectionMemoResult{State: SwapProtectionNoIntent}

	if !strings.Contains(memo, SwapProtectionMemoStem) {
		if isDeprecatedSwapProtectionMemo(memo) {
			result.State = SwapProtectionMalformedIntent
			return result, errorsmod.Wrap(ErrInvalidSwapProtection, "deprecated transwap memo namespace")
		}
		return result, nil
	}

	result.State = SwapProtectionMalformedIntent
	if !strings.HasPrefix(memo, SwapProtectionMemoMarkerV1) {
		return result, errorsmod.Wrap(ErrInvalidSwapProtection, "transwap protection marker must use supported v1 marker at byte offset zero")
	}

	body := memo[len(SwapProtectionMemoMarkerV1):]
	if len(body) == 0 || body[0] != '{' {
		return result, errorsmod.Wrap(ErrInvalidSwapProtection, "transwap protection v1 body must immediately begin with a JSON object")
	}
	if !utf8.ValidString(body) {
		return result, errorsmod.Wrap(ErrInvalidSwapProtection, "transwap protection v1 body must be valid UTF-8")
	}

	protection, err := parseSwapProtectionV1([]byte(body))
	if err != nil {
		return result, err
	}
	result.State = SwapProtectionValidIntent
	result.Protection = protection
	return result, nil
}

func parseSwapProtectionV1(raw []byte) (SwapProtection, error) {
	var protection SwapProtection
	fields, duplicateFields, err := decodeJSONObject(raw)
	if err != nil {
		return protection, errorsmod.Wrapf(ErrInvalidSwapProtection, "invalid transwap protection v1 JSON object: %v", err)
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
	if len(fields) == 0 {
		return protection, errorsmod.Wrap(ErrInvalidSwapProtection, "transwap protection v1 requires at least one protection field")
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

// isDeprecatedSwapProtectionMemo narrowly detects a valid top-level legacy
// namespace. Invalid or unrelated JSON remains an opaque memo when no reserved
// stem is present, avoiding false positives for other memo formats.
func isDeprecatedSwapProtectionMemo(memo string) bool {
	trimmed := strings.TrimSpace(memo)
	if trimmed == "" || trimmed[0] != '{' {
		return false
	}
	fields, duplicateFields, err := decodeJSONObject([]byte(trimmed))
	if err != nil {
		return false
	}
	if _, found := fields[SwapProtectionMemoKey]; found {
		return true
	}
	for _, field := range duplicateFields {
		if field == SwapProtectionMemoKey {
			return true
		}
	}
	return false
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
