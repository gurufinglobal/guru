package collector

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

var (
	errInvalidSourceJSON       = errors.New("invalid JSON response")
	errInvalidSourceUTF8       = errors.New("response body is not valid UTF-8")
	errJSONPointerUnresolved   = errors.New("JSON Pointer did not resolve")
	errJSONPointerNotNumeric   = errors.New("JSON Pointer value is not numeric text")
	errJSONNumericTokenTooLong = errors.New("numeric value rejected")
)

const maxJSONNestingDepth = 10_000

type extractedJSONValue struct {
	found   bool
	numeric bool
	tooLong bool
	text    string
}

type sourceJSONParser struct {
	ctx       context.Context
	input     []byte
	tokens    []string
	position  int
	lastCheck int
}

func extractJSONNumericText(ctx context.Context, input []byte, pointer string) (string, error) {
	if err := validateSourceUTF8(ctx, input); err != nil {
		return "", err
	}
	if err := domain.ValidateJSONPointer(pointer); err != nil {
		return "", errJSONPointerUnresolved
	}
	var tokens []string
	if pointer != "" {
		rawTokens := strings.Split(pointer[1:], "/")
		tokens = make([]string, len(rawTokens))
		for i, token := range rawTokens {
			tokens[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		}
	}
	parser := sourceJSONParser{ctx: ctx, input: input, tokens: tokens}
	if err := parser.checkContext(true); err != nil {
		return "", err
	}
	value, err := parser.parseValue(true, 0, 0)
	if err != nil {
		return "", err
	}
	if err := parser.skipWhitespace(); err != nil {
		return "", err
	}
	if parser.position != len(parser.input) {
		return "", errInvalidSourceJSON
	}
	if err := parser.checkContext(true); err != nil {
		return "", err
	}
	switch {
	case !value.found:
		return "", errJSONPointerUnresolved
	case !value.numeric:
		return "", errJSONPointerNotNumeric
	case value.tooLong:
		return "", errJSONNumericTokenTooLong
	default:
		return value.text, nil
	}
}

func validateSourceUTF8(ctx context.Context, input []byte) error {
	lastCheck := 0
	for position := 0; position < len(input); {
		if position-lastCheck >= 4<<10 {
			lastCheck = position
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		if input[position] < utf8.RuneSelf {
			position++
			continue
		}
		r, size := utf8.DecodeRune(input[position:])
		if r == utf8.RuneError && size == 1 {
			return errInvalidSourceUTF8
		}
		position += size
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (p *sourceJSONParser) parseValue(
	seeking bool,
	tokenIndex,
	depth int,
) (extractedJSONValue, error) {
	if depth > maxJSONNestingDepth {
		return extractedJSONValue{}, errInvalidSourceJSON
	}
	if err := p.checkContext(false); err != nil {
		return extractedJSONValue{}, err
	}
	if err := p.skipWhitespace(); err != nil {
		return extractedJSONValue{}, err
	}
	if p.position >= len(p.input) {
		return extractedJSONValue{}, errInvalidSourceJSON
	}
	if seeking && tokenIndex == len(p.tokens) {
		switch current := p.input[p.position]; {
		case current == '"':
			text, _, tooLong, err := p.parseString(nil, domain.MaxNumericToken)
			if err != nil {
				return extractedJSONValue{}, err
			}
			return extractedJSONValue{found: true, numeric: true, tooLong: tooLong, text: text}, nil
		case current == '-' || isJSONDigit(current):
			start, err := p.parseNumber()
			if err != nil {
				return extractedJSONValue{}, err
			}
			length := p.position - start
			if length > domain.MaxNumericToken {
				return extractedJSONValue{found: true, numeric: true, tooLong: true}, nil
			}
			return extractedJSONValue{
				found:   true,
				numeric: true,
				text:    string(p.input[start:p.position]),
			}, nil
		default:
			if _, err := p.parseValue(false, tokenIndex, depth); err != nil {
				return extractedJSONValue{}, err
			}
			return extractedJSONValue{found: true}, nil
		}
	}

	switch current := p.input[p.position]; {
	case current == '{':
		return p.parseObject(seeking, tokenIndex, depth+1)
	case current == '[':
		return p.parseArray(seeking, tokenIndex, depth+1)
	case current == '"':
		_, _, _, err := p.parseString(nil, 0)
		return extractedJSONValue{}, err
	case current == '-' || isJSONDigit(current):
		_, err := p.parseNumber()
		return extractedJSONValue{}, err
	case current == 't':
		return extractedJSONValue{}, p.parseLiteral("true")
	case current == 'f':
		return extractedJSONValue{}, p.parseLiteral("false")
	case current == 'n':
		return extractedJSONValue{}, p.parseLiteral("null")
	default:
		return extractedJSONValue{}, errInvalidSourceJSON
	}
}

func (p *sourceJSONParser) parseObject(
	seeking bool,
	tokenIndex,
	depth int,
) (extractedJSONValue, error) {
	p.position++
	if err := p.skipWhitespace(); err != nil {
		return extractedJSONValue{}, err
	}
	if p.consume('}') {
		return extractedJSONValue{}, nil
	}
	var result extractedJSONValue
	for {
		if p.position >= len(p.input) || p.input[p.position] != '"' {
			return extractedJSONValue{}, errInvalidSourceJSON
		}
		var target *string
		if seeking {
			target = &p.tokens[tokenIndex]
		}
		_, matches, _, err := p.parseString(target, 0)
		if err != nil {
			return extractedJSONValue{}, err
		}
		if err := p.skipWhitespace(); err != nil {
			return extractedJSONValue{}, err
		}
		if !p.consume(':') {
			return extractedJSONValue{}, errInvalidSourceJSON
		}
		childSeeking := seeking && matches
		childToken := tokenIndex
		if childSeeking {
			childToken++
		}
		child, err := p.parseValue(childSeeking, childToken, depth)
		if err != nil {
			return extractedJSONValue{}, err
		}
		if childSeeking {
			result = child
		}
		if err := p.skipWhitespace(); err != nil {
			return extractedJSONValue{}, err
		}
		if p.consume('}') {
			return result, nil
		}
		if !p.consume(',') {
			return extractedJSONValue{}, errInvalidSourceJSON
		}
		if err := p.skipWhitespace(); err != nil {
			return extractedJSONValue{}, err
		}
	}
}

func (p *sourceJSONParser) parseArray(
	seeking bool,
	tokenIndex,
	depth int,
) (extractedJSONValue, error) {
	p.position++
	if err := p.skipWhitespace(); err != nil {
		return extractedJSONValue{}, err
	}
	if p.consume(']') {
		return extractedJSONValue{}, nil
	}
	targetIndex := -1
	if seeking {
		targetIndex = canonicalArrayIndex(p.tokens[tokenIndex])
	}
	var result extractedJSONValue
	for index := 0; ; index++ {
		childSeeking := seeking && index == targetIndex
		childToken := tokenIndex
		if childSeeking {
			childToken++
		}
		child, err := p.parseValue(childSeeking, childToken, depth)
		if err != nil {
			return extractedJSONValue{}, err
		}
		if childSeeking {
			result = child
		}
		if err := p.skipWhitespace(); err != nil {
			return extractedJSONValue{}, err
		}
		if p.consume(']') {
			return result, nil
		}
		if !p.consume(',') {
			return extractedJSONValue{}, errInvalidSourceJSON
		}
		if err := p.skipWhitespace(); err != nil {
			return extractedJSONValue{}, err
		}
	}
}

func (p *sourceJSONParser) parseString(
	target *string,
	captureLimit int,
) (text string, matches, tooLong bool, err error) {
	if !p.consume('"') {
		return "", false, false, errInvalidSourceJSON
	}
	var decoded []byte
	if captureLimit > 0 {
		decoded = make([]byte, 0, min(captureLimit, 64))
	}
	matches = target != nil
	matchedBytes := 0
	emit := func(content []byte) {
		if target != nil && matches {
			if matchedBytes+len(content) > len(*target) {
				matches = false
			} else {
				for index, value := range content {
					if value != (*target)[matchedBytes+index] {
						matches = false
						break
					}
				}
			}
		}
		matchedBytes += len(content)
		if captureLimit > 0 {
			if len(decoded)+len(content) > captureLimit {
				tooLong = true
			} else if !tooLong {
				decoded = append(decoded, content...)
			}
		}
	}

	for p.position < len(p.input) {
		if err := p.checkContext(false); err != nil {
			return "", false, false, err
		}
		current := p.input[p.position]
		p.position++
		switch {
		case current == '"':
			if target != nil {
				matches = matches && matchedBytes == len(*target)
			}
			return string(decoded), matches, tooLong, nil
		case current == '\\':
			if p.position >= len(p.input) {
				return "", false, false, errInvalidSourceJSON
			}
			escaped := p.input[p.position]
			p.position++
			switch escaped {
			case '"', '\\', '/':
				emit([]byte{escaped})
			case 'b':
				emit([]byte{'\b'})
			case 'f':
				emit([]byte{'\f'})
			case 'n':
				emit([]byte{'\n'})
			case 'r':
				emit([]byte{'\r'})
			case 't':
				emit([]byte{'\t'})
			case 'u':
				r, parseErr := p.parseEscapedRune()
				if parseErr != nil {
					return "", false, false, parseErr
				}
				var encoded [utf8.UTFMax]byte
				size := utf8.EncodeRune(encoded[:], r)
				emit(encoded[:size])
			default:
				return "", false, false, errInvalidSourceJSON
			}
		case current < 0x20:
			return "", false, false, errInvalidSourceJSON
		case current < utf8.RuneSelf:
			emit([]byte{current})
		default:
			p.position--
			r, size := utf8.DecodeRune(p.input[p.position:])
			if r == utf8.RuneError && size == 1 {
				return "", false, false, errInvalidSourceJSON
			}
			emit(p.input[p.position : p.position+size])
			p.position += size
		}
	}
	return "", false, false, errInvalidSourceJSON
}

func (p *sourceJSONParser) parseEscapedRune() (rune, error) {
	first, err := p.parseHexQuad()
	if err != nil {
		return 0, err
	}
	switch {
	case first >= 0xD800 && first <= 0xDBFF:
		if p.position+2 > len(p.input) ||
			p.input[p.position] != '\\' ||
			p.input[p.position+1] != 'u' {
			return 0, errInvalidSourceJSON
		}
		p.position += 2
		second, err := p.parseHexQuad()
		if err != nil || second < 0xDC00 || second > 0xDFFF {
			return 0, errInvalidSourceJSON
		}
		return utf16.DecodeRune(rune(first), rune(second)), nil
	case first >= 0xDC00 && first <= 0xDFFF:
		return 0, errInvalidSourceJSON
	default:
		return rune(first), nil
	}
}

func (p *sourceJSONParser) parseHexQuad() (uint16, error) {
	if p.position+4 > len(p.input) {
		return 0, errInvalidSourceJSON
	}
	var value uint16
	for range 4 {
		current := p.input[p.position]
		p.position++
		value <<= 4
		switch {
		case current >= '0' && current <= '9':
			value |= uint16(current - '0')
		case current >= 'a' && current <= 'f':
			value |= uint16(current-'a') + 10
		case current >= 'A' && current <= 'F':
			value |= uint16(current-'A') + 10
		default:
			return 0, errInvalidSourceJSON
		}
	}
	return value, nil
}

func (p *sourceJSONParser) parseNumber() (int, error) {
	start := p.position
	if p.consume('-') && p.position >= len(p.input) {
		return 0, errInvalidSourceJSON
	}
	if p.consume('0') {
		if p.position < len(p.input) && isJSONDigit(p.input[p.position]) {
			return 0, errInvalidSourceJSON
		}
	} else {
		if p.position >= len(p.input) || p.input[p.position] < '1' || p.input[p.position] > '9' {
			return 0, errInvalidSourceJSON
		}
		for p.position < len(p.input) && isJSONDigit(p.input[p.position]) {
			p.position++
			if err := p.checkContext(false); err != nil {
				return 0, err
			}
		}
	}
	if p.consume('.') {
		digits := p.position
		for p.position < len(p.input) && isJSONDigit(p.input[p.position]) {
			p.position++
			if err := p.checkContext(false); err != nil {
				return 0, err
			}
		}
		if p.position == digits {
			return 0, errInvalidSourceJSON
		}
	}
	if p.position < len(p.input) && (p.input[p.position] == 'e' || p.input[p.position] == 'E') {
		p.position++
		if p.position < len(p.input) && (p.input[p.position] == '+' || p.input[p.position] == '-') {
			p.position++
		}
		digits := p.position
		for p.position < len(p.input) && isJSONDigit(p.input[p.position]) {
			p.position++
			if err := p.checkContext(false); err != nil {
				return 0, err
			}
		}
		if p.position == digits {
			return 0, errInvalidSourceJSON
		}
	}
	return start, nil
}

func (p *sourceJSONParser) parseLiteral(literal string) error {
	if len(p.input)-p.position < len(literal) ||
		string(p.input[p.position:p.position+len(literal)]) != literal {
		return errInvalidSourceJSON
	}
	p.position += len(literal)
	return nil
}

func (p *sourceJSONParser) skipWhitespace() error {
	for p.position < len(p.input) {
		switch p.input[p.position] {
		case ' ', '\t', '\n', '\r':
			p.position++
			if err := p.checkContext(false); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (p *sourceJSONParser) consume(expected byte) bool {
	if p.position >= len(p.input) || p.input[p.position] != expected {
		return false
	}
	p.position++
	return true
}

func (p *sourceJSONParser) checkContext(force bool) error {
	if !force && p.position-p.lastCheck < 4<<10 {
		return nil
	}
	p.lastCheck = p.position
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		return nil
	}
}

func canonicalArrayIndex(token string) int {
	if token == "" || token == "-" || (len(token) > 1 && token[0] == '0') {
		return -1
	}
	value, err := strconv.ParseUint(token, 10, 31)
	if err != nil {
		return -1
	}
	return int(value)
}

func isJSONDigit(value byte) bool {
	return value >= '0' && value <= '9'
}
