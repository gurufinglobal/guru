package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestExtractJSONNumericTextBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		pointer string
		want    string
		wantErr error
	}{
		{name: "root number", input: `-1.25e+2`, pointer: "", want: "-1.25e+2"},
		{name: "root numeric string", input: `"0.1"`, pointer: "", want: "0.1"},
		{name: "nested array object", input: `{"a":[{"v":1},{"v":"2"}]}`, pointer: "/a/1/v", want: "2"},
		{name: "unicode escaped key", input: `{"\uD83D\uDE80":"3"}`, pointer: "/🚀", want: "3"},
		{name: "escape-equivalent duplicate", input: `{"v":"1","\u0076":"2"}`, pointer: "/v", want: "2"},
		{name: "empty object key", input: `{"":{"v":4}}`, pointer: "//v", want: "4"},
		{name: "numeric object key", input: `{"01":5}`, pointer: "/01", want: "5"},
		{name: "last duplicate removes path", input: `{"a":{"v":1},"a":{}}`, pointer: "/a/v", wantErr: errJSONPointerUnresolved},
		{name: "last duplicate changes type", input: `{"v":1,"v":false}`, pointer: "/v", wantErr: errJSONPointerNotNumeric},
		{
			name:    "noncanonical array index",
			input:   `{"a":[1,2]}`,
			pointer: "/a/01",
			wantErr: errJSONPointerUnresolved,
		},
		{
			name:    "oversized numeric string",
			input:   `{"v":"` + strings.Repeat("1", 257) + `"}`,
			pointer: "/v",
			wantErr: errJSONNumericTokenTooLong,
		},
		{name: "trailing array comma", input: `[1,]`, pointer: "/0", wantErr: errInvalidSourceJSON},
		{name: "trailing object comma", input: `{"v":1,}`, pointer: "/v", wantErr: errInvalidSourceJSON},
		{name: "invalid number fraction", input: `{"v":1.}`, pointer: "/v", wantErr: errInvalidSourceJSON},
		{name: "invalid leading plus", input: `{"v":+1}`, pointer: "/v", wantErr: errInvalidSourceJSON},
		{name: "unpaired high surrogate", input: `{"v":"\uD800"}`, pointer: "/v", wantErr: errInvalidSourceJSON},
		{name: "unpaired low surrogate", input: `{"v":"\uDC00"}`, pointer: "/v", wantErr: errInvalidSourceJSON},
		{name: "mismatched surrogate pair", input: `{"v":"\uD800\u0041"}`, pointer: "/v", wantErr: errInvalidSourceJSON},
		{name: "invalid UTF-8", input: string([]byte{'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'}), pointer: "/v", wantErr: errInvalidSourceUTF8},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got, err := extractJSONNumericText(context.Background(), []byte(test.input), test.pointer)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractJSONNumericTextMatchesStandardReference(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		input   string
		pointer string
	}{
		{name: "root exponent", input: `-1.25e+2`},
		{name: "escaped numeric string", input: `"\u0031.25"`},
		{name: "nested array", input: `{"a":[{"v":1},{"v":"2"}]}`, pointer: "/a/1/v"},
		{name: "escaped pointer", input: `{"a/b":{"~v":"3"}}`, pointer: "/a~1b/~0v"},
		{name: "escape-equivalent duplicate", input: `{"v":"1","\u0076":"2"}`, pointer: "/v"},
		{name: "last duplicate unresolved", input: `{"a":{"v":1},"a":{}}`, pointer: "/a/v"},
		{name: "last duplicate nonnumeric", input: `{"v":1,"v":false}`, pointer: "/v"},
		{name: "noncanonical array index", input: `[1,2]`, pointer: "/01"},
		{name: "oversized numeric string", input: `"` + strings.Repeat("1", domain.MaxNumericToken+1) + `"`},
		{name: "empty object key", input: `{"":4}`, pointer: "/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := extractJSONNumericText(context.Background(), []byte(test.input), test.pointer)
			want, wantErr := referenceJSONNumericText([]byte(test.input), test.pointer)
			if got != want || !sameJSONExtractionError(gotErr, wantErr) {
				t.Fatalf("custom = (%q, %v), reference = (%q, %v)", got, gotErr, want, wantErr)
			}
		})
	}
}

func TestExtractJSONNumericTextMaximumNesting(t *testing.T) {
	input := nestedArrays(maxJSONNestingDepth)
	if _, err := extractJSONNumericText(context.Background(), input, ""); !errors.Is(err, errJSONPointerNotNumeric) {
		t.Fatalf("maximum nesting error = %v, want nonnumeric root", err)
	}
	input = nestedArrays(maxJSONNestingDepth + 1)
	if _, err := extractJSONNumericText(context.Background(), input, ""); !errors.Is(err, errInvalidSourceJSON) {
		t.Fatalf("excess nesting error = %v, want invalid JSON", err)
	}
}

func TestSourceJSONParserChecksContextWithinFourKiB(t *testing.T) {
	input := []byte(`"` + strings.Repeat("1", 20<<10) + `"`)
	recorder := &contextCheckRecorder{done: make(chan struct{})}
	parser := sourceJSONParser{input: input}
	recorder.position = func() int { return parser.position }
	parser.ctx = recorder
	if err := parser.checkContext(true); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.parseValue(true, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := parser.checkContext(true); err != nil {
		t.Fatal(err)
	}
	if len(recorder.positions) < 3 {
		t.Fatalf("context checked only at positions %v", recorder.positions)
	}
	for i := 1; i < len(recorder.positions); i++ {
		if distance := recorder.positions[i] - recorder.positions[i-1]; distance > 4<<10 {
			t.Fatalf("context check gap = %d bytes at positions %v", distance, recorder.positions)
		}
	}
}

func FuzzExtractJSONNumericText(f *testing.F) {
	f.Add([]byte(`{"v":"1.25"}`), "/v")
	f.Add([]byte(`{"a/b":[0,-2e3]}`), "/a~1b/1")
	f.Add([]byte(`null`), "")
	f.Fuzz(func(t *testing.T, input []byte, pointer string) {
		if len(input) > 64<<10 || len(pointer) > 2<<10 {
			t.Skip()
		}
		// encoding/json intentionally replaces malformed UTF-16 surrogates, while
		// the source contract rejects them. Compare only the shared valid domain.
		if !json.Valid(input) || !utf8.Valid(input) || containsSurrogateEscape(input) {
			return
		}
		got, gotErr := extractJSONNumericText(context.Background(), input, pointer)
		want, wantErr := referenceJSONNumericText(input, pointer)
		if got != want || !sameJSONExtractionError(gotErr, wantErr) {
			t.Fatalf("custom = (%q, %v), reference = (%q, %v)", got, gotErr, want, wantErr)
		}
	})
}

func BenchmarkExtractJSONNumericTextMaximumBody(b *testing.B) {
	input := maximumJSONBody()
	var (
		allocationValue string
		allocationErr   error
	)
	allocations := testing.AllocsPerRun(3, func() {
		allocationValue, allocationErr = extractJSONNumericText(context.Background(), input, "/value")
	})
	if allocationErr != nil || allocationValue != "1" {
		b.Fatalf("allocation probe value = %q, error = %v", allocationValue, allocationErr)
	}
	if allocations > 4 {
		b.Fatalf("allocations per maximum-body extraction = %.1f, want at most 4", allocations)
	}
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ReportMetric(allocations, "allocs/extract")
	b.ResetTimer()
	for b.Loop() {
		value, err := extractJSONNumericText(context.Background(), input, "/value")
		if err != nil || value != "1" {
			b.Fatalf("value = %q, error = %v", value, err)
		}
	}
}

func referenceJSONNumericText(input []byte, pointer string) (string, error) {
	if err := domain.ValidateJSONPointer(pointer); err != nil {
		return "", errJSONPointerUnresolved
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errInvalidSourceJSON
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errInvalidSourceJSON
	}
	if pointer != "" {
		for _, token := range strings.Split(pointer[1:], "/") {
			token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
			switch current := value.(type) {
			case map[string]any:
				var found bool
				value, found = current[token]
				if !found {
					return "", errJSONPointerUnresolved
				}
			case []any:
				index, ok := referenceArrayIndex(token)
				if !ok || index >= len(current) {
					return "", errJSONPointerUnresolved
				}
				value = current[index]
			default:
				return "", errJSONPointerUnresolved
			}
		}
	}
	switch value := value.(type) {
	case json.Number:
		if len(value) > domain.MaxNumericToken {
			return "", errJSONNumericTokenTooLong
		}
		return value.String(), nil
	case string:
		if len(value) > domain.MaxNumericToken {
			return "", errJSONNumericTokenTooLong
		}
		return value, nil
	default:
		return "", errJSONPointerNotNumeric
	}
}

func referenceArrayIndex(token string) (int, bool) {
	if token == "" || token == "-" || (len(token) > 1 && token[0] == '0') {
		return 0, false
	}
	value, err := strconv.ParseUint(token, 10, 31)
	return int(value), err == nil
}

func sameJSONExtractionError(first, second error) bool {
	for _, target := range []error{
		errInvalidSourceJSON,
		errInvalidSourceUTF8,
		errJSONPointerUnresolved,
		errJSONPointerNotNumeric,
		errJSONNumericTokenTooLong,
	} {
		if errors.Is(first, target) || errors.Is(second, target) {
			return errors.Is(first, target) && errors.Is(second, target)
		}
	}
	return first == nil && second == nil
}

func nestedArrays(depth int) []byte {
	return []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
}

func maximumJSONBody() []byte {
	const maximumBodyBytes = 16 << 20
	prefix := `{"value":"1","padding":"`
	suffix := `"}`
	return []byte(prefix + strings.Repeat("x", maximumBodyBytes-len(prefix)-len(suffix)) + suffix)
}

func containsSurrogateEscape(input []byte) bool {
	for i := 0; i+2 < len(input); i++ {
		if input[i] == '\\' && input[i+1] == 'u' && (input[i+2] == 'd' || input[i+2] == 'D') {
			return true
		}
	}
	return false
}

type contextCheckRecorder struct {
	position  func() int
	positions []int
	done      chan struct{}
}

func (*contextCheckRecorder) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *contextCheckRecorder) Done() <-chan struct{} {
	c.positions = append(c.positions, c.position())
	return c.done
}

func (*contextCheckRecorder) Err() error { return nil }

func (*contextCheckRecorder) Value(any) any { return nil }
