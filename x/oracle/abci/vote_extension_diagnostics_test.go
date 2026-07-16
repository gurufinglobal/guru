package abci

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestVoteExtensionDiagnosticsReportCanonicalTuple(t *testing.T) {
	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := NewAggregator(fakeKeeper{}, oracleValidatorStoreFor(validator))

	diagnostics := aggregator.voteExtensionDiagnostics(ctx, extCommit)
	require.Len(t, diagnostics, 1)
	diagnostic := diagnostics[0]
	require.Equal(t, 0, diagnostic.VoteIndex)
	require.Equal(t, fmt.Sprintf("%X", validator.consAddr.Bytes()), diagnostic.ValidatorAddress)
	require.Equal(t, int64(1), diagnostic.ValidatorPower)
	require.NotEmpty(t, diagnostic.ExtensionHash)
	require.NotEmpty(t, diagnostic.SignatureHash)
	require.NotEmpty(t, diagnostic.CanonicalSignBytesHash)
	require.NotEmpty(t, diagnostic.PublicKeyHash)
	require.True(t, diagnostic.ExpectedSignatureValid)
	require.Empty(t, diagnostic.PublicKeyResolutionError)
}

func TestVoteExtensionDiagnosticsDoNotTreatMutatedSignatureAsValid(t *testing.T) {
	validator := newOracleTestValidator()
	extCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	extCommit.Votes[0].ExtensionSignature[0] ^= 0xff
	ctx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), extCommit)
	aggregator := NewAggregator(fakeKeeper{}, oracleValidatorStoreFor(validator))

	diagnostics := aggregator.voteExtensionDiagnostics(ctx, extCommit)
	require.Len(t, diagnostics, 1)
	require.False(t, diagnostics[0].ExpectedSignatureValid)
	require.Empty(t, diagnostics[0].PublicKeyResolutionError)

	err := aggregator.validateVoteExtensions(ctx, extCommit)
	require.ErrorContains(t, err, "failed to verify validator")
}

func TestVoteExtensionDiagnosticsLogOnlyOnFailureWithoutRawExtension(t *testing.T) {
	validator := newOracleTestValidator()
	validCommit := signedOracleExtCommit(t, 3, validator, "1.0")
	aggregator := NewAggregator(fakeKeeper{}, oracleValidatorStoreFor(validator))

	var successLogs bytes.Buffer
	validCtx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), validCommit).
		WithLogger(log.NewLogger(&successLogs))
	require.NoError(t, aggregator.validateVoteExtensions(validCtx, validCommit))
	require.Empty(t, successLogs.String())

	invalidCommit := cloneExtendedCommit(validCommit)
	const rawExtension = "oracle-raw-extension-must-not-be-logged"
	invalidCommit.Votes[0].VoteExtension = []byte(rawExtension)
	var failureLogs bytes.Buffer
	invalidCtx := withOracleProposalContext(sdk.Context{}, 3, time.Unix(30, 0), invalidCommit).
		WithLogger(log.NewLogger(&failureLogs))

	err := aggregator.validateVoteExtensions(invalidCtx, invalidCommit)
	require.ErrorContains(t, err, "failed to verify validator")
	output := failureLogs.String()
	require.Contains(t, output, "oracle vote extension validation failed")
	require.Contains(t, output, "oracle vote extension validation tuple")
	require.Contains(t, output, "canonical_sign_bytes_sha256")
	require.Contains(t, output, "expected_signature_valid")
	require.NotContains(t, output, rawExtension)
	require.Less(t, len(output), 4_096)
}
