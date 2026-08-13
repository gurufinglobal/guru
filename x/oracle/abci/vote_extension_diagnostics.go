package abci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type voteExtensionDiagnostic struct {
	VoteIndex                int
	ValidatorAddress         string
	ValidatorPower           int64
	BlockIDFlag              cmtproto.BlockIDFlag
	ExtensionLength          int
	ExtensionHash            string
	SignatureLength          int
	SignatureHash            string
	CanonicalSignBytesHash   string
	PublicKeyHash            string
	ExpectedSignatureValid   bool
	PublicKeyResolutionError string
}

func (a Aggregator) validateVoteExtensions(ctx sdk.Context, extCommit abcitypes.ExtendedCommitInfo) error {
	err := baseapp.ValidateVoteExtensions(ctx, a.validatorStore, 0, "", extCommit)
	if err == nil {
		return nil
	}

	header := ctx.HeaderInfo()
	lastCommitRound := int64(-1)
	if cometInfo := ctx.CometInfo(); cometInfo != nil {
		if lastCommit := cometInfo.GetLastCommit(); lastCommit != nil {
			lastCommitRound = int64(lastCommit.Round())
		}
	}

	logger := ctx.Logger()
	if logger == nil {
		return err
	}
	logger = logger.With("module", "x/oracle")
	logger.Error(
		"oracle vote extension validation failed",
		"error", err,
		"block_height", ctx.BlockHeight(),
		"header_height", header.Height,
		"chain_id", header.ChainID,
		"extended_commit_round", extCommit.Round,
		"last_commit_round", lastCommitRound,
		"vote_count", len(extCommit.Votes),
	)
	for _, diagnostic := range a.voteExtensionDiagnostics(ctx, extCommit) {
		logger.Error(
			"oracle vote extension validation tuple",
			"vote_index", diagnostic.VoteIndex,
			"validator_address", diagnostic.ValidatorAddress,
			"validator_power", diagnostic.ValidatorPower,
			"block_id_flag", diagnostic.BlockIDFlag.String(),
			"extension_length", diagnostic.ExtensionLength,
			"extension_sha256", diagnostic.ExtensionHash,
			"signature_length", diagnostic.SignatureLength,
			"signature_sha256", diagnostic.SignatureHash,
			"canonical_sign_bytes_sha256", diagnostic.CanonicalSignBytesHash,
			"public_key_sha256", diagnostic.PublicKeyHash,
			"expected_signature_valid", diagnostic.ExpectedSignatureValid,
			"public_key_error", diagnostic.PublicKeyResolutionError,
		)
	}

	return err
}

func (a Aggregator) voteExtensionDiagnostics(
	ctx sdk.Context,
	extCommit abcitypes.ExtendedCommitInfo,
) []voteExtensionDiagnostic {
	header := ctx.HeaderInfo()
	diagnostics := make([]voteExtensionDiagnostic, 0, len(extCommit.Votes))
	for i, vote := range extCommit.Votes {
		canonicalSignBytes := cmttypes.VoteExtensionSignBytes(header.ChainID, &cmtproto.Vote{
			Extension: vote.VoteExtension,
			Height:    header.Height - 1,
			Round:     extCommit.Round,
		})
		diagnostic := voteExtensionDiagnostic{
			VoteIndex:              i,
			ValidatorAddress:       fmt.Sprintf("%X", vote.Validator.Address),
			ValidatorPower:         vote.Validator.Power,
			BlockIDFlag:            vote.BlockIdFlag,
			ExtensionLength:        len(vote.VoteExtension),
			ExtensionHash:          sha256Hex(vote.VoteExtension),
			SignatureLength:        len(vote.ExtensionSignature),
			SignatureHash:          sha256Hex(vote.ExtensionSignature),
			CanonicalSignBytesHash: sha256Hex(canonicalSignBytes),
		}

		pubKeyProto, err := a.validatorStore.GetPubKeyByConsAddr(ctx, sdk.ConsAddress(vote.Validator.Address))
		if err != nil {
			diagnostic.PublicKeyResolutionError = err.Error()
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		pubKey, err := cryptoenc.PubKeyFromProto(pubKeyProto)
		if err != nil {
			diagnostic.PublicKeyResolutionError = err.Error()
			diagnostics = append(diagnostics, diagnostic)
			continue
		}

		diagnostic.PublicKeyHash = sha256Hex(pubKey.Bytes())
		diagnostic.ExpectedSignatureValid = pubKey.VerifySignature(canonicalSignBytes, vote.ExtensionSignature)
		diagnostics = append(diagnostics, diagnostic)
	}

	return diagnostics
}

func sha256Hex(bz []byte) string {
	hash := sha256.Sum256(bz)
	return hex.EncodeToString(hash[:])
}
