package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// ParseCategory parses a Category from either enum name (e.g. CATEGORY_CRYPTO)
// or numeric value (e.g. 2). Empty string returns CATEGORY_UNSPECIFIED.
func ParseCategory(v string) (types.Category, error) {
	if strings.TrimSpace(v) == "" {
		return types.Category_CATEGORY_UNSPECIFIED, nil
	}

	// Numeric form
	if n, err := strconv.ParseInt(v, 10, 32); err == nil {
		if _, ok := types.Category_name[int32(n)]; !ok {
			return 0, fmt.Errorf("unknown category value %d", n)
		}
		return types.Category(n), nil
	}

	// Enum name form (as-is, then upper)
	if n, ok := types.Category_value[v]; ok {
		return types.Category(n), nil
	}
	if n, ok := types.Category_value[strings.ToUpper(v)]; ok {
		return types.Category(n), nil
	}

	return 0, fmt.Errorf("unknown category %q (expected enum name or numeric value)", v)
}

// ParseStatus parses a Status from either enum name (e.g. STATUS_ACTIVE)
// or numeric value (e.g. 1). Empty string returns STATUS_UNSPECIFIED.
func ParseStatus(v string) (types.Status, error) {
	if strings.TrimSpace(v) == "" {
		return types.Status_STATUS_UNSPECIFIED, nil
	}

	// Numeric form
	if n, err := strconv.ParseInt(v, 10, 32); err == nil {
		if _, ok := types.Status_name[int32(n)]; !ok {
			return 0, fmt.Errorf("unknown status value %d", n)
		}
		return types.Status(n), nil
	}

	// Enum name form (as-is, then upper)
	if n, ok := types.Status_value[v]; ok {
		return types.Status(n), nil
	}
	if n, ok := types.Status_value[strings.ToUpper(v)]; ok {
		return types.Status(n), nil
	}

	return 0, fmt.Errorf("unknown status %q (expected enum name or numeric value)", v)
}
