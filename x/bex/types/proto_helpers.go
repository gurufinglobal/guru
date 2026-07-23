package types

import (
	"bytes"
	"reflect"

	gogoproto "github.com/cosmos/gogoproto/proto"
	gogotypes "github.com/cosmos/gogoproto/types"
)

// CloneMessage returns a deep copy while preserving the concrete internal
// gogo message type. It also preserves a typed nil input.
func CloneMessage[T gogoproto.Message](message T) T {
	value := reflect.ValueOf(message)
	if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return message
	}
	if value.Kind() != reflect.Pointer {
		panic("internal protobuf message must be a pointer")
	}

	cloned, ok := reflect.New(value.Type().Elem()).Interface().(T)
	if !ok {
		panic("internal protobuf clone has an unexpected concrete type")
	}
	bz, err := gogoproto.Marshal(message)
	if err != nil {
		panic(err)
	}
	if err := gogoproto.Unmarshal(bz, cloned); err != nil {
		panic(err)
	}
	return cloned
}

// EqualMessages reports protobuf wire-semantic equality for internal gogo
// messages. Comparing encoded bytes preserves proto3's nil-versus-empty
// repeated/map equivalence and avoids reflecting through sdkmath.Int internals.
func EqualMessages(left, right gogoproto.Message) bool {
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	leftNil := !leftValue.IsValid() || (leftValue.Kind() == reflect.Pointer && leftValue.IsNil())
	rightNil := !rightValue.IsValid() || (rightValue.Kind() == reflect.Pointer && rightValue.IsNil())
	if leftNil || rightNil {
		return leftNil && rightNil
	}
	if leftValue.Type() != rightValue.Type() {
		return false
	}

	leftBytes, err := gogoproto.Marshal(left)
	if err != nil {
		return false
	}
	rightBytes, err := gogoproto.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(leftBytes, rightBytes)
}

// NewStringValue constructs an internal gogo string wrapper.
func NewStringValue(value string) *gogotypes.StringValue {
	return &gogotypes.StringValue{Value: value}
}

// NewUInt32Value constructs an internal gogo uint32 wrapper.
func NewUInt32Value(value uint32) *gogotypes.UInt32Value {
	return &gogotypes.UInt32Value{Value: value}
}

// NewUInt64Value constructs an internal gogo uint64 wrapper.
func NewUInt64Value(value uint64) *gogotypes.UInt64Value {
	return &gogotypes.UInt64Value{Value: value}
}

// NewBoolValue constructs an internal gogo bool wrapper.
func NewBoolValue(value bool) *gogotypes.BoolValue {
	return &gogotypes.BoolValue{Value: value}
}
