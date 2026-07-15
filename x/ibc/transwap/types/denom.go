package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmttypes "github.com/cometbft/cometbft/types"

	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"

	errorsmod "cosmossdk.io/errors"
)

// NewDenom creates a new Denom instance given the base denomination and a variable number of hops.
func NewDenom(base string, trace ...*transwapv1.Hop) *transwapv1.Denom {
	return &transwapv1.Denom{
		Base:  base,
		Trace: trace,
	}
}

// ValidateDenom performs a basic validation of the Denom fields.
func ValidateDenom(d *transwapv1.Denom) error {
	if d == nil {
		return errorsmod.Wrap(ErrInvalidDenomForTransfer, "denomination cannot be nil")
	}

	// NOTE: base denom validation cannot be performed here as each chain may
	// define its own base denom validation. TokenToCoin validates the resolved
	// local bank denomination immediately before sdk.Coin materialization.
	if strings.TrimSpace(d.Base) == "" {
		return errorsmod.Wrap(ErrInvalidDenomForTransfer, "base denomination cannot be blank")
	}

	for _, hop := range d.Trace {
		if err := ValidateHop(hop); err != nil {
			return errorsmod.Wrap(err, "invalid trace")
		}
	}

	return nil
}

// DenomHash returns the hex bytes of the SHA256 hash of the Denom fields.
func DenomHash(d *transwapv1.Denom) cmtbytes.HexBytes {
	hash := sha256.Sum256([]byte(DenomPath(d)))
	return hash[:]
}

// DenomIBCDenom returns the ICS20 ibc/{hash} denom for traced denominations.
func DenomIBCDenom(d *transwapv1.Denom) string {
	if d == nil {
		return ""
	}
	if DenomIsNative(d) {
		return d.Base
	}

	return fmt.Sprintf("%s/%s", DenomPrefix, DenomHash(d))
}

// DenomPath returns the full denomination according to the ICS20 specification.
func DenomPath(d *transwapv1.Denom) string {
	if d == nil {
		return ""
	}
	if DenomIsNative(d) {
		return d.Base
	}

	var sb strings.Builder
	for _, t := range d.Trace {
		_, _ = sb.WriteString(HopPath(t))
		if err := sb.WriteByte('/'); err != nil {
			return ""
		}
	}
	_, err := sb.WriteString(d.Base)
	if err != nil {
		return ""
	}
	return sb.String()
}

// DenomIsNative returns true if the denomination is native, thus containing no trace history.
func DenomIsNative(d *transwapv1.Denom) bool {
	if d == nil {
		return true
	}
	return len(d.Trace) == 0
}

// DenomHasPrefix returns true if the first trace hop matches the provided port and channel.
func DenomHasPrefix(d *transwapv1.Denom, portID, channelID string) bool {
	if DenomIsNative(d) {
		return false
	}
	if d.Trace[0] == nil {
		return false
	}

	return d.Trace[0].PortId == portID && d.Trace[0].ChannelId == channelID
}

// Denoms defines a wrapper type for a slice of Denom.
type Denoms []*transwapv1.Denom

// Validate performs a basic validation of each denomination trace info.
func (d Denoms) Validate() error {
	seenDenoms := make(map[string]bool)
	for i, denom := range d {
		if denom == nil {
			return fmt.Errorf("denomination %d cannot be nil", i)
		}

		hash := DenomHash(denom).String()
		if seenDenoms[hash] {
			return fmt.Errorf("duplicated denomination with hash %s", DenomHash(denom))
		}

		if err := ValidateDenom(denom); err != nil {
			return errorsmod.Wrapf(err, "failed denom %d validation", i)
		}
		seenDenoms[hash] = true
	}
	return nil
}

var _ sort.Interface = (*Denoms)(nil)

func (d Denoms) Len() int { return len(d) }

func (d Denoms) Less(i, j int) bool {
	if d[i] == nil {
		return d[j] != nil
	}
	if d[j] == nil {
		return false
	}

	if d[i].Base != d[j].Base {
		return d[i].Base < d[j].Base
	}

	if len(d[i].Trace) != len(d[j].Trace) {
		return len(d[i].Trace) < len(d[j].Trace)
	}

	return DenomPath(d[i]) < DenomPath(d[j])
}

func (d Denoms) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

func (d Denoms) Sort() Denoms {
	sort.Sort(d)
	return d
}

// ExtractDenomFromPath returns the denom from the full path.
func ExtractDenomFromPath(fullPath string) *transwapv1.Denom {
	denomSplit := strings.Split(fullPath, "/")

	if denomSplit[0] == fullPath {
		return &transwapv1.Denom{Base: fullPath}
	}

	var (
		trace          []*transwapv1.Hop
		baseDenomSlice []string
	)

	length := len(denomSplit)
	for i := 0; i < length; i += 2 {
		if i < length-1 && length > 2 && (channeltypes.IsValidChannelID(denomSplit[i+1]) || clienttypes.IsValidClientID(denomSplit[i+1])) {
			trace = append(trace, NewHop(denomSplit[i], denomSplit[i+1]))
		} else {
			baseDenomSlice = denomSplit[i:]
			break
		}
	}

	base := strings.Join(baseDenomSlice, "/")

	return &transwapv1.Denom{
		Base:  base,
		Trace: trace,
	}
}

// CloneDenom returns a deep copy of denom so packet-local trace mutation does not alias state.
func CloneDenom(denom *transwapv1.Denom) *transwapv1.Denom {
	if denom == nil {
		return nil
	}

	trace := make([]*transwapv1.Hop, len(denom.Trace))
	for i, hop := range denom.Trace {
		if hop != nil {
			trace[i] = &transwapv1.Hop{PortId: hop.PortId, ChannelId: hop.ChannelId}
		}
	}

	return &transwapv1.Denom{Base: denom.Base, Trace: trace}
}

// CloneToken returns a token copy with a deep-copied denom trace.
func CloneToken(token *transwapv1.Token) *transwapv1.Token {
	if token == nil {
		return nil
	}
	return &transwapv1.Token{Denom: CloneDenom(token.Denom), Amount: token.Amount}
}

// ParseHexHash parses a hex hash in string format to bytes and validates its correctness.
func ParseHexHash(hexHash string) (cmtbytes.HexBytes, error) {
	hash, err := hex.DecodeString(hexHash)
	if err != nil {
		return nil, err
	}

	if err := cmttypes.ValidateHash(hash); err != nil {
		return nil, err
	}

	return hash, nil
}
