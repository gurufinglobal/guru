package types

import (
	"fmt"
	"math"

	collcodec "cosmossdk.io/collections/codec"
	"github.com/cosmos/gogoproto/types"

	"cosmossdk.io/collections"
)

// LegacyTaskPriorityValueCodec는 "신규 canonical 포맷(uint32)"과
// "구 포맷(gogotypes.UInt64Value)"을 동시에 읽기 위한 AltValueCodec 예시다.
//
// 이 패턴의 핵심은 다음과 같다:
// 1) 읽기 경로: 새/구 포맷 모두 수용한다.
// 2) 쓰기 경로: 항상 새 포맷으로 기록한다.
// 3) 결과적으로 트래픽이 흐르는 동안 점진적으로 상태가 canonical 포맷으로 수렴한다.
var LegacyTaskPriorityValueCodec = collcodec.NewAltValueCodec(
	collections.Uint32Value,
	decodeLegacyTaskPriority,
)

func decodeLegacyTaskPriority(bz []byte) (uint32, error) {
	legacy := new(types.UInt64Value)
	if err := legacy.Unmarshal(bz); err != nil {
		return 0, err
	}

	if legacy.Value > math.MaxUint32 {
		return 0, fmt.Errorf("%w: legacy priority overflow (%d)", ErrLegacyMigrationFailure, legacy.Value)
	}

	return uint32(legacy.Value), nil
}
