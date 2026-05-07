package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// 데모 모듈이지만 팀에 "운영 가능한 입력 검증 패턴"을 보여주기 위해
	// 문자열 길이 상한을 명시한다.
	MaxProjectNameLength        = 80
	MaxProjectDescriptionLength = 512
	MaxTaskTitleLength          = 120
	MaxTaskDescriptionLength    = 2048
	MaxMetadataJSONLength       = 4096
	MaxEventTypeLength          = 64
	MaxEventPayloadLength       = 8192
	MaxLabelLength              = 32
	MaxLabelCount               = 32
)

// ValidateAddress는 bech32 주소 문자열의 유효성을 검증한다.
func ValidateAddress(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return fmt.Errorf("%w: address is empty", ErrInvalidRequest)
	}
	if _, err := sdk.AccAddressFromBech32(addr); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidRequest, err)
	}
	return nil
}

// ValidateOptionalAddress는 선택 필드 주소를 검증한다.
// 빈 문자열은 "미지정"으로 취급해 허용하고, 값이 있을 때만 bech32 검증을 수행한다.
func ValidateOptionalAddress(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	return ValidateAddress(addr)
}

// ValidateMaxLen은 사용자 입력 문자열 길이를 검사한다.
func ValidateMaxLen(field, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%w: %s too long (max=%d)", ErrInvalidRequest, field, max)
	}
	return nil
}
