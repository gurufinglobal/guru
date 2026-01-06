package types

import (
	"encoding/binary"
)

const (
	ModuleName  = "oracle"
	StoreKey    = ModuleName
	RouterKey   = ModuleName
	MemStoreKey = "mem_oracle"
)

// key prefixes
const (
	prefixParams byte = iota + 1
	prefixModerator
	prefixRequest
	prefixRequestCount
	prefixReport
	prefixResult
	prefixCategory
	prefixWhitelist
	prefixLatestResult
	prefixTaskSchedule
	prefixResultExpiry
	prefixReportCount
)

var (
	// KeyParams is the key for the oracle module parameters.
	KeyParams = []byte{prefixParams}
	// KeyModeratorAddress is the key for the moderator address.
	KeyModeratorAddress = []byte{prefixModerator}
	// KeyRequestCount is the key for the request count.
	KeyRequestCount = []byte{prefixRequestCount}
	// KeyWhitelistCount is the key for the whitelist count.
	KeyWhitelistCount = []byte{prefixWhitelist}
)

// RequestKey returns the key for a specific oracle request ID.
func RequestKey(id uint64) []byte {
	return append([]byte{prefixRequest}, IDToBytes(id)...)
}

// ReportKey returns the key for a specific oracle report.
func ReportKey(requestID, nonce uint64, provider string) []byte {
	key := make([]byte, 1, 1+16+len(provider))
	key[0] = prefixReport
	key = append(key, IDToBytes(requestID)...)
	key = append(key, IDToBytes(nonce)...)
	return append(key, []byte(provider)...)
}

// ReportPrefix returns the key prefix for reports of a specific request and nonce.
func ReportPrefix(requestID, nonce uint64) []byte {
	key := make([]byte, 1, 1+16)
	key[0] = prefixReport
	key = append(key, IDToBytes(requestID)...)
	return append(key, IDToBytes(nonce)...)
}

// ReportRequestPrefix returns the key prefix for reports of a specific request.
func ReportRequestPrefix(requestID uint64) []byte {
	key := make([]byte, 1, 1+8)
	key[0] = prefixReport
	return append(key, IDToBytes(requestID)...)
}

// ResultKey returns the key for a specific oracle result.
func ResultKey(requestID, nonce uint64) []byte {
	key := make([]byte, 1, 1+16)
	key[0] = prefixResult
	key = append(key, IDToBytes(requestID)...)
	return append(key, IDToBytes(nonce)...)
}

// ResultPrefix returns the key prefix for results of a specific request.
func ResultPrefix(requestID uint64) []byte {
	key := make([]byte, 1, 1+8)
	key[0] = prefixResult
	return append(key, IDToBytes(requestID)...)
}

// CategoryKey returns the key for a specific category.
func CategoryKey(cat Category) []byte {
	return []byte{prefixCategory, byte(cat)}
}

const (
	// WhitelistKeyPrefix is the prefix for whitelist keys.
	WhitelistKeyPrefix = "whitelist/value/"
)

// GetWhitelistKey returns the key for a specific whitelist address.
func GetWhitelistKey(address string) []byte {
	return append([]byte(WhitelistKeyPrefix), []byte(address)...)
}

// LatestResultKey returns the key for storing latest result nonce per request.
// Key: prefixLatestResult || requestID (8 bytes)
func LatestResultKey(requestID uint64) []byte {
	return append([]byte{prefixLatestResult}, IDToBytes(requestID)...)
}

// TaskScheduleKey returns the key for scheduling oracle task events.
// Key: prefixTaskSchedule || blockHeight (8 bytes) || requestID (8 bytes)
func TaskScheduleKey(blockHeight, requestID uint64) []byte {
	key := make([]byte, 1+16)
	key[0] = prefixTaskSchedule
	copy(key[1:9], IDToBytes(blockHeight))
	copy(key[9:17], IDToBytes(requestID))
	return key
}

// TaskSchedulePrefix returns the prefix for a specific block height.
func TaskSchedulePrefix(blockHeight uint64) []byte {
	return append([]byte{prefixTaskSchedule}, IDToBytes(blockHeight)...)
}

// ResultExpiryKey returns the key for indexing result expiry.
// Key: prefixResultExpiry || expireHeight (8) || requestID (8) || nonce (8)
func ResultExpiryKey(expireHeight, requestID, nonce uint64) []byte {
	key := make([]byte, 1+24)
	key[0] = prefixResultExpiry
	copy(key[1:9], IDToBytes(expireHeight))
	copy(key[9:17], IDToBytes(requestID))
	copy(key[17:25], IDToBytes(nonce))
	return key
}

// ResultExpiryPrefix returns the prefix for a specific expiry height.
func ResultExpiryPrefix(expireHeight uint64) []byte {
	return append([]byte{prefixResultExpiry}, IDToBytes(expireHeight)...)
}

// ReportCountKey returns the key for tracking report counts.
// Key: prefixReportCount || requestID (8) || nonce (8)
func ReportCountKey(requestID, nonce uint64) []byte {
	key := make([]byte, 1+16)
	key[0] = prefixReportCount
	copy(key[1:9], IDToBytes(requestID))
	copy(key[9:17], IDToBytes(nonce))
	return key
}

// IDToBytes converts a uint64 ID to an 8-byte big-endian slice.
func IDToBytes(id uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, id)
	return bz
}

// BytesToID converts an 8-byte big-endian slice to a uint64 ID.
func BytesToID(bz []byte) uint64 {
	return binary.BigEndian.Uint64(bz)
}
