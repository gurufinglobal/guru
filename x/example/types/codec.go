package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec:
// 이 체인은 신규 체인 + proto-only 방향으로 설계되어 legacy amino 경로를 사용하지 않는다.
// 다만 AppModuleBasic 인터페이스 호환을 위해 함수 시그니처는 유지한다.
func RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces는 protobuf Msg를 interface registry에 등록한다.
// CollInterfaceValue[sdk.Msg] 사용 시에도 registry 기반 직렬화가 필요하므로
// Msg 구현체 등록은 반드시 선행되어야 한다.
func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreateProject{},
		&MsgUpdateProject{},
		&MsgAddProjectMember{},
		&MsgRemoveProjectMember{},
		&MsgCreateTask{},
		&MsgAssignTask{},
		&MsgUpdateTaskStatus{},
		&MsgAppendTaskEvent{},
		&MsgDeleteTask{},
		&MsgMigrateLegacyTaskValues{},
		&MsgUpdateParams{},
	)

	// protobuf service descriptor를 registry에 연결하면
	// SDK의 메시지 라우팅/권한 체인이 서비스 정의를 기준으로 동작한다.
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
