package oracle

import (
	"fmt"
	"strconv"
	"strings"
)

func ExtractJSONPath(value any, path string) (any, error) {
	current := value
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		if part == "" {
			return nil, fmt.Errorf("empty path segment")
		}
		next, err := applyPathPart(current, part)
		if err != nil {
			return nil, err
		}
		current = next
	}

	return current, nil
}

func applyPathPart(value any, part string) (any, error) {
	current := value
	remaining := part
	for remaining != "" {
		bracket := strings.IndexByte(remaining, '[')
		if bracket < 0 {
			if remaining == "" {
				return current, nil
			}
			return objectField(current, remaining)
		}

		if bracket > 0 {
			field, err := objectField(current, remaining[:bracket])
			if err != nil {
				return nil, err
			}
			current = field
		}

		closeBracket := strings.IndexByte(remaining[bracket:], ']')
		if closeBracket < 0 {
			return nil, fmt.Errorf("missing closing bracket in %q", part)
		}
		indexText := remaining[bracket+1 : bracket+closeBracket]
		index, err := strconv.Atoi(indexText)
		if err != nil {
			return nil, fmt.Errorf("invalid array index %q", indexText)
		}
		current, err = arrayIndex(current, index)
		if err != nil {
			return nil, err
		}
		remaining = remaining[bracket+closeBracket+1:]
	}

	return current, nil
}

func objectField(value any, field string) (any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object before %q", field)
	}
	next, ok := object[field]
	if !ok {
		return nil, fmt.Errorf("missing field %q", field)
	}

	return next, nil
}

func arrayIndex(value any, index int) (any, error) {
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array before index %d", index)
	}
	if index < 0 || index >= len(array) {
		return nil, fmt.Errorf("array index %d out of range", index)
	}

	return array[index], nil
}
