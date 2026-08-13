package abci

import (
	"fmt"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func signedVoteExtensionsFromExtendedCommit(extCommit abcitypes.ExtendedCommitInfo) *oraclev1.OracleSignedVoteExtensions {
	votes := make([]*oraclev1.OracleSignedVoteExtension, 0, len(extCommit.GetVotes()))
	for _, vote := range extCommit.GetVotes() {
		votes = append(votes, &oraclev1.OracleSignedVoteExtension{
			ValidatorAddress:   append([]byte(nil), vote.Validator.Address...),
			ValidatorPower:     vote.Validator.Power,
			BlockIdFlag:        int32(vote.BlockIdFlag),
			VoteExtension:      append([]byte(nil), vote.VoteExtension...),
			ExtensionSignature: append([]byte(nil), vote.ExtensionSignature...),
		})
	}

	return &oraclev1.OracleSignedVoteExtensions{
		Round: extCommit.Round,
		Votes: votes,
	}
}

func extendedCommitFromSignedVoteExtensions(voteExtensions *oraclev1.OracleSignedVoteExtensions) abcitypes.ExtendedCommitInfo {
	if voteExtensions == nil {
		return abcitypes.ExtendedCommitInfo{}
	}

	votes := make([]abcitypes.ExtendedVoteInfo, 0, len(voteExtensions.GetVotes()))
	for _, vote := range voteExtensions.GetVotes() {
		votes = append(votes, abcitypes.ExtendedVoteInfo{
			Validator: abcitypes.Validator{
				Address: append([]byte(nil), vote.GetValidatorAddress()...),
				Power:   vote.GetValidatorPower(),
			},
			BlockIdFlag:        cmtproto.BlockIDFlag(vote.GetBlockIdFlag()),
			VoteExtension:      append([]byte(nil), vote.GetVoteExtension()...),
			ExtensionSignature: append([]byte(nil), vote.GetExtensionSignature()...),
		})
	}

	return abcitypes.ExtendedCommitInfo{
		Round: voteExtensions.GetRound(),
		Votes: votes,
	}
}

func validateExtendedCommitBlockIDFlags(ctx sdk.Context, extCommit abcitypes.ExtendedCommitInfo) error {
	cometInfo := ctx.CometInfo()
	if cometInfo == nil {
		return fmt.Errorf("missing comet info for oracle vote extension validation")
	}
	lastCommit := cometInfo.GetLastCommit()
	if lastCommit == nil {
		return fmt.Errorf("missing last commit info for oracle vote extension validation")
	}
	lastVotes := lastCommit.Votes()
	if lastVotes == nil {
		return fmt.Errorf("missing last commit votes for oracle vote extension validation")
	}
	if len(extCommit.GetVotes()) != lastVotes.Len() {
		return fmt.Errorf("oracle vote extension count %d does not match last commit vote count %d", len(extCommit.GetVotes()), lastVotes.Len())
	}

	for i, vote := range extCommit.GetVotes() {
		expectedFlag := cmtproto.BlockIDFlag(lastVotes.Get(i).GetBlockIDFlag())
		if vote.BlockIdFlag != expectedFlag {
			return fmt.Errorf(
				"oracle vote extension %d block_id_flag %s does not match last commit block_id_flag %s",
				i,
				vote.BlockIdFlag,
				expectedFlag,
			)
		}
	}

	return nil
}
