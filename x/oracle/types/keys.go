package types

import (
	"encoding/binary"
	"fmt"
)

const (
	// ModuleName defines the module name
	ModuleName = "oracle"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_oracle"
)

// KV Store key prefix bytes
const (
	preficParams = iota + 1
	prefixModeratorAddress
	prefixOracleRequestDoc
	prefixOracleRequestDocCount
	prefixOracleData
	prefixOracleDataSet
)

// KV Store key prefixes
var (
	KeyParams                = []byte{preficParams}
	KeyModeratorAddress      = []byte{prefixModeratorAddress}
	KeyOracleRequestDoc      = []byte{prefixOracleRequestDoc}
	KeyOracleRequestDocCount = []byte{prefixOracleRequestDocCount}
	KeyOracleData            = []byte{prefixOracleData}
	KeyOracleDataSet         = []byte{prefixOracleDataSet}
)

// GetOracleRequestDocKey returns the key for storing OracleRequsetDoc
func GetOracleRequestDocKey(id uint64) []byte {
	return append(KeyOracleRequestDoc, IDToBytes(id)...)
}

// GetOracleDataKey returns the key for storing oracle data
func GetOracleDataKey(id uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, id)
	return append(KeyOracleData, bz...)
}

// ParseOracleDataKey parses the oracle data key and returns the ID
func ParseOracleDataKey(key []byte) (uint64, error) {
	if len(key) != 9 {
		return 0, fmt.Errorf("invalid oracle data key length: %d", len(key))
	}
	return binary.BigEndian.Uint64(key[1:]), nil
}

func GetSubmitDataKey(request_id uint64, nonce uint64) []byte {
	KeyOracleDataForRequest := append(KeyOracleData, IDToBytes(request_id)...)
	if nonce == 0 {
		return KeyOracleDataForRequest
	}
	return append(KeyOracleDataForRequest, IDToBytes(nonce)...)
}

func GetSubmitDataKeyByProvider(request_id uint64, nonce uint64, provider string) []byte {
	return append(GetSubmitDataKey(request_id, nonce), StringToBytes(provider)...)
}

// GetDataSetKey returns the key for storing a DataSet
func GetDataSetKey(request_id uint64, nonce uint64) []byte {
	return append(KeyOracleDataSet, IDToBytes(request_id)...)
}

func IDToBytes(id uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, id)
	return bz
}

func IDToBytes32(id uint32) []byte {
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, id)
	return bz
}

func StringToBytes(str string) []byte {
	return []byte(str)
}
