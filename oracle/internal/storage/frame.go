package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

const (
	storageSchemaVersion = 1
	maxAggregatePayload  = 16 << 10
	maxCatalogPayload    = 256 << 10
)

var (
	aggregateMagic  = [4]byte{'O', 'R', 'A', 'G'}
	catalogMagic    = [4]byte{'O', 'R', 'C', 'T'}
	aggregateDomain = []byte("guru-oracled/aggregate/v1\x00")
	catalogDomain   = []byte("guru-oracled/catalog/v1\x00")
)

type aggregatePayload struct {
	Symbol                 string   `json:"symbol"`
	Value                  string   `json:"value"`
	Sequence               string   `json:"sequence"`
	ActivationGeneration   string   `json:"activation_generation"`
	CycleStartedAtUnixNano string   `json:"cycle_started_at_unix_nano"`
	CollectedAtUnixNano    string   `json:"collected_at_unix_nano"`
	ConfiguredSourceCount  uint32   `json:"configured_source_count"`
	SuccessfulSourceCount  uint32   `json:"successful_source_count"`
	ContributorIDs         []string `json:"contributor_ids"`
	FeedPlanFingerprint    string   `json:"feed_plan_fingerprint"`
}

type catalogPayload struct {
	ActivationGeneration string        `json:"activation_generation"`
	PlanDigest           string        `json:"plan_digest"`
	Feeds                []catalogFeed `json:"feeds"`
}

type catalogFeed struct {
	Symbol              string `json:"symbol"`
	FeedPlanFingerprint string `json:"feed_plan_fingerprint"`
}

type Catalog struct {
	ActivationGeneration uint64
	PlanDigest           [32]byte
	Feeds                []CatalogFeed
}

type CatalogFeed struct {
	Symbol      string
	Fingerprint [32]byte
}

func encodeAggregate(record domain.Aggregate) ([]byte, error) {
	if err := validateAggregate(record); err != nil {
		return nil, err
	}
	payload := aggregatePayload{
		Symbol:                 record.Symbol,
		Value:                  record.Value,
		Sequence:               strconv.FormatUint(record.Sequence, 10),
		ActivationGeneration:   strconv.FormatUint(record.ActivationGeneration, 10),
		CycleStartedAtUnixNano: strconv.FormatInt(record.CycleStartedAt.UnixNano(), 10),
		CollectedAtUnixNano:    strconv.FormatInt(record.CollectedAt.UnixNano(), 10),
		ConfiguredSourceCount:  record.ConfiguredSources,
		SuccessfulSourceCount:  record.SuccessfulSources,
		ContributorIDs:         append([]string{}, record.ContributorIDs...),
		FeedPlanFingerprint:    hex.EncodeToString(record.FeedPlanFingerprint[:]),
	}
	return encodeFrame(aggregateMagic, aggregateDomain, payload, maxAggregatePayload)
}

func decodeAggregate(frame []byte) (domain.Aggregate, error) {
	var payload aggregatePayload
	raw, err := decodeFrame(frame, aggregateMagic, aggregateDomain, maxAggregatePayload, &payload)
	if err != nil {
		return domain.Aggregate{}, err
	}
	sequence, err := parseCanonicalUint(payload.Sequence, false)
	if err != nil {
		return domain.Aggregate{}, fmt.Errorf("sequence: %w", err)
	}
	generation, err := parseCanonicalUint(payload.ActivationGeneration, true)
	if err != nil {
		return domain.Aggregate{}, fmt.Errorf("activation generation: %w", err)
	}
	started, err := parsePositiveInt64(payload.CycleStartedAtUnixNano)
	if err != nil {
		return domain.Aggregate{}, fmt.Errorf("cycle started: %w", err)
	}
	collected, err := parsePositiveInt64(payload.CollectedAtUnixNano)
	if err != nil {
		return domain.Aggregate{}, fmt.Errorf("collected: %w", err)
	}
	fingerprint, err := parseDigest(payload.FeedPlanFingerprint)
	if err != nil {
		return domain.Aggregate{}, fmt.Errorf("feed fingerprint: %w", err)
	}
	record := domain.Aggregate{
		Symbol:               payload.Symbol,
		Value:                payload.Value,
		Sequence:             sequence,
		ActivationGeneration: generation,
		CycleStartedAt:       time.Unix(0, started).UTC(),
		CollectedAt:          time.Unix(0, collected).UTC(),
		ConfiguredSources:    payload.ConfiguredSourceCount,
		SuccessfulSources:    payload.SuccessfulSourceCount,
		ContributorIDs:       append([]string{}, payload.ContributorIDs...),
		FeedPlanFingerprint:  fingerprint,
	}
	if err := validateAggregate(record); err != nil {
		return domain.Aggregate{}, err
	}
	reencoded, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(raw, reencoded) {
		return domain.Aggregate{}, errors.New("aggregate payload is not canonical JSON")
	}
	return record, nil
}

func encodeCatalog(catalog Catalog) ([]byte, error) {
	if err := validateCatalog(catalog); err != nil {
		return nil, err
	}
	feeds := make([]catalogFeed, len(catalog.Feeds))
	for i, feed := range catalog.Feeds {
		feeds[i] = catalogFeed{
			Symbol:              feed.Symbol,
			FeedPlanFingerprint: hex.EncodeToString(feed.Fingerprint[:]),
		}
	}
	payload := catalogPayload{
		ActivationGeneration: strconv.FormatUint(catalog.ActivationGeneration, 10),
		PlanDigest:           hex.EncodeToString(catalog.PlanDigest[:]),
		Feeds:                feeds,
	}
	return encodeFrame(catalogMagic, catalogDomain, payload, maxCatalogPayload)
}

func decodeCatalog(frame []byte) (Catalog, error) {
	var payload catalogPayload
	raw, err := decodeFrame(frame, catalogMagic, catalogDomain, maxCatalogPayload, &payload)
	if err != nil {
		return Catalog{}, err
	}
	if payload.Feeds == nil {
		return Catalog{}, errors.New("catalog feeds must be a JSON array")
	}
	generation, err := parseCanonicalUint(payload.ActivationGeneration, true)
	if err != nil {
		return Catalog{}, err
	}
	digest, err := parseDigest(payload.PlanDigest)
	if err != nil {
		return Catalog{}, err
	}
	catalog := Catalog{
		ActivationGeneration: generation,
		PlanDigest:           digest,
		Feeds:                make([]CatalogFeed, len(payload.Feeds)),
	}
	for i, feed := range payload.Feeds {
		fingerprint, err := parseDigest(feed.FeedPlanFingerprint)
		if err != nil {
			return Catalog{}, err
		}
		catalog.Feeds[i] = CatalogFeed{Symbol: feed.Symbol, Fingerprint: fingerprint}
	}
	if err := validateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	reencoded, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(raw, reencoded) {
		return Catalog{}, errors.New("catalog payload is not canonical JSON")
	}
	return catalog, nil
}

func encodeFrame(magic [4]byte, domainSeparator []byte, payload any, maximum int) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(raw) > maximum {
		return nil, fmt.Errorf("payload exceeds %d bytes", maximum)
	}
	frame := make([]byte, 10+len(raw)+sha256.Size)
	copy(frame[:4], magic[:])
	binary.BigEndian.PutUint16(frame[4:6], storageSchemaVersion)
	binary.BigEndian.PutUint32(frame[6:10], uint32(len(raw)))
	copy(frame[10:], raw)
	hashInput := make([]byte, 0, len(domainSeparator)+10+len(raw))
	hashInput = append(hashInput, domainSeparator...)
	hashInput = append(hashInput, frame[:10+len(raw)]...)
	sum := sha256.Sum256(hashInput)
	copy(frame[10+len(raw):], sum[:])
	return frame, nil
}

func decodeFrame(frame []byte, magic [4]byte, domainSeparator []byte, maximum int, target any) ([]byte, error) {
	if len(frame) < 10+sha256.Size {
		return nil, errors.New("frame is truncated")
	}
	if !bytes.Equal(frame[:4], magic[:]) {
		return nil, errors.New("frame magic mismatch")
	}
	if binary.BigEndian.Uint16(frame[4:6]) != storageSchemaVersion {
		return nil, errors.New("unsupported frame schema")
	}
	length := binary.BigEndian.Uint32(frame[6:10])
	if length > uint32(maximum) {
		return nil, errors.New("frame payload exceeds limit")
	}
	expected := 10 + int(length) + sha256.Size
	if len(frame) != expected {
		return nil, errors.New("frame length mismatch")
	}
	hashInput := make([]byte, 0, len(domainSeparator)+10+int(length))
	hashInput = append(hashInput, domainSeparator...)
	hashInput = append(hashInput, frame[:10+int(length)]...)
	sum := sha256.Sum256(hashInput)
	if !bytes.Equal(sum[:], frame[10+int(length):]) {
		return nil, errors.New("frame checksum mismatch")
	}
	raw := frame[10 : 10+int(length)]
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("decode frame payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("frame payload has trailing JSON")
	}
	return raw, nil
}

func validateAggregate(record domain.Aggregate) error {
	normalized, err := domain.NormalizeSymbol(record.Symbol)
	if err != nil || normalized != record.Symbol {
		return errors.New("aggregate symbol is not canonical")
	}
	if _, err := domain.ParseCanonicalDecimal(record.Value); err != nil {
		return fmt.Errorf("aggregate value: %w", err)
	}
	if record.Sequence == 0 {
		return errors.New("aggregate sequence is zero")
	}
	if record.ActivationGeneration == 0 {
		return errors.New("aggregate activation generation is zero")
	}
	if record.CycleStartedAt.UnixNano() <= 0 || record.CollectedAt.UnixNano() <= 0 {
		return errors.New("aggregate timestamps are invalid")
	}
	if record.ConfiguredSources < domain.MinSourcesPerFeed || record.ConfiguredSources > domain.MaxSourcesPerFeed ||
		record.SuccessfulSources == 0 || record.SuccessfulSources > record.ConfiguredSources ||
		uint32(len(record.ContributorIDs)) != record.SuccessfulSources {
		return errors.New("aggregate source counts are invalid")
	}
	if record.SuccessfulSources < record.ConfiguredSources/2+1 {
		return errors.New("aggregate is below strict-majority quorum")
	}
	for i, id := range record.ContributorIDs {
		if err := domain.ValidateSourceID(id); err != nil {
			return errors.New("aggregate contributor id is invalid")
		}
		if i > 0 && record.ContributorIDs[i-1] >= id {
			return errors.New("aggregate contributors are not sorted and unique")
		}
	}
	return nil
}

func validateCatalog(catalog Catalog) error {
	if catalog.ActivationGeneration == 0 && len(catalog.Feeds) != 0 {
		return errors.New("generation-zero catalog must be empty")
	}
	for i, feed := range catalog.Feeds {
		normalized, err := domain.NormalizeSymbol(feed.Symbol)
		if err != nil || normalized != feed.Symbol {
			return errors.New("catalog symbol is not canonical")
		}
		if i > 0 && catalog.Feeds[i-1].Symbol >= feed.Symbol {
			return errors.New("catalog feeds are not sorted and unique")
		}
	}
	if len(catalog.Feeds) > domain.MaxFeeds {
		return errors.New("catalog has too many feeds")
	}
	return nil
}

func parseCanonicalUint(value string, allowZero bool) (uint64, error) {
	if value == "" || (len(value) > 1 && value[0] == '0') || strings.HasPrefix(value, "+") {
		return 0, errors.New("integer string is not canonical")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || (!allowZero && parsed == 0) {
		return 0, errors.New("integer string is invalid")
	}
	return parsed, nil
}

func parsePositiveInt64(value string) (int64, error) {
	if value == "" || value[0] == '+' || value[0] == '-' || (len(value) > 1 && value[0] == '0') {
		return 0, errors.New("timestamp is not canonical")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("timestamp is invalid")
	}
	return parsed, nil
}

func parseDigest(value string) ([32]byte, error) {
	var digest [32]byte
	if len(value) != 64 || strings.ToLower(value) != value {
		return digest, errors.New("digest is not canonical lowercase hex")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return digest, errors.New("digest is invalid")
	}
	copy(digest[:], decoded)
	return digest, nil
}

func canonicalCatalog(feeds []CatalogFeed) []CatalogFeed {
	result := append([]CatalogFeed(nil), feeds...)
	sort.Slice(result, func(i, j int) bool { return result[i].Symbol < result[j].Symbol })
	return result
}
