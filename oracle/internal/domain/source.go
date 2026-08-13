package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"unicode/utf8"
)

const (
	MaxSourceIDBytes = 128
	MaxURLBytes      = 2048
	MaxPointerBytes  = 1024
)

func ValidateSourceID(id string) error {
	if len(id) < 1 || len(id) > MaxSourceIDBytes {
		return fmt.Errorf("source ID must be 1-%d safe ASCII bytes", MaxSourceIDBytes)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return errors.New("source ID contains an unsafe character")
	}
	return nil
}

func ValidateSourceURL(raw string) error {
	if len(raw) < 1 || len(raw) > MaxURLBytes || !utf8.ValidString(raw) {
		return fmt.Errorf("source URL must be valid UTF-8 and at most %d bytes", MaxURLBytes)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("source URL must use HTTPS and include a host")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return errors.New("source URL port must be between 1 and 65535")
		}
	}
	if parsed.User != nil {
		return errors.New("source URL must not include credentials")
	}
	if parsed.Fragment != "" {
		return errors.New("source URL must not include a fragment")
	}
	return nil
}

func ValidateJSONPointer(pointer string) error {
	if len(pointer) > MaxPointerBytes || !utf8.ValidString(pointer) {
		return errors.New("JSON Pointer is too long or invalid UTF-8")
	}
	if pointer == "" {
		return nil
	}
	if pointer[0] != '/' {
		return errors.New("JSON Pointer must be empty or begin with /")
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] != '~' {
			continue
		}
		if i+1 >= len(pointer) || (pointer[i+1] != '0' && pointer[i+1] != '1') {
			return errors.New("JSON Pointer contains an invalid escape")
		}
		i++
	}
	return nil
}
