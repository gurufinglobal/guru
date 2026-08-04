package domain

import (
	"strings"
	"testing"
)

func TestValidateSourceID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "minimum", value: "a"},
		{name: "maximum", value: strings.Repeat("a", 128)},
		{name: "allowed alphabet", value: "AZaz09._-"},
		{name: "empty", wantErr: true},
		{name: "too long", value: strings.Repeat("a", 129), wantErr: true},
		{name: "space", value: "a b", wantErr: true},
		{name: "slash", value: "a/b", wantErr: true},
		{name: "non ascii", value: "가격", wantErr: true},
		{name: "other punctuation", value: "a:b", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSourceID(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateSourceID(%q) error = %v, wantErr %t", test.value, err, test.wantErr)
			}
		})
	}
}

func TestValidateSourceURL(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://example.com/value",
		"https://example.com:443/value",
		"https://[::1]:8443/value",
	} {
		if err := ValidateSourceURL(raw); err != nil {
			t.Errorf("ValidateSourceURL(%q) = %v", raw, err)
		}
	}
	for _, raw := range []string{
		"https://example.com/" + strings.Repeat("x", MaxURLBytes),
		"https://:443/value",
		"https://:/value",
		"https://example.com:0/value",
		"https://example.com:65536/value",
		"http://example.com/value",
		"https://user@example.com/value",
		"https://example.com/value#fragment",
		string([]byte{'h', 't', 't', 'p', 's', ':', '/', '/', 0xff}),
	} {
		if err := ValidateSourceURL(raw); err == nil {
			t.Errorf("ValidateSourceURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestValidateJSONPointer(t *testing.T) {
	t.Parallel()
	for _, pointer := range []string{"", "/", "/a~1b/~0", "/가격"} {
		if err := ValidateJSONPointer(pointer); err != nil {
			t.Errorf("ValidateJSONPointer(%q) = %v", pointer, err)
		}
	}
	for _, pointer := range []string{
		"value",
		"/~",
		"/~2",
		"/" + strings.Repeat("x", MaxPointerBytes),
		string([]byte{'/', 0xff}),
	} {
		if err := ValidateJSONPointer(pointer); err == nil {
			t.Errorf("ValidateJSONPointer(%q) unexpectedly succeeded", pointer)
		}
	}
}
