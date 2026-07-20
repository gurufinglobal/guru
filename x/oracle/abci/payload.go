package abci

import (
	"bytes"
	"errors"
	"fmt"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

func EncodeProposalTx(payload *oraclev1.OracleProposalPayload) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("oracle proposal payload cannot be nil")
	}

	option, err := codectypes.NewAnyWithValue(payload)
	if err != nil {
		return nil, err
	}
	if option.TypeUrl != oraclev1.ProposalPayloadTypeURL {
		return nil, fmt.Errorf("unexpected oracle proposal type URL %q", option.TypeUrl)
	}

	bodyBytes, err := (&txtypes.TxBody{
		ExtensionOptions: []*codectypes.Any{option},
	}).Marshal()
	if err != nil {
		return nil, err
	}
	authInfoBytes, err := (&txtypes.AuthInfo{Fee: &txtypes.Fee{}}).Marshal()
	if err != nil {
		return nil, err
	}

	return (&txtypes.TxRaw{
		BodyBytes:     bodyBytes,
		AuthInfoBytes: authInfoBytes,
	}).Marshal()
}

func DecodeProposalTx(tx []byte) (*oraclev1.OracleProposalPayload, bool, error) {
	var raw txtypes.TxRaw
	if err := raw.Unmarshal(tx); err != nil {
		return nil, false, nil
	}

	var body txtypes.TxBody
	if err := body.Unmarshal(raw.BodyBytes); err != nil {
		return nil, false, nil
	}

	isCandidate := false
	for _, options := range [][]*codectypes.Any{body.ExtensionOptions, body.NonCriticalExtensionOptions} {
		for _, option := range options {
			if option != nil && option.TypeUrl == oraclev1.ProposalPayloadTypeURL {
				isCandidate = true
				break
			}
		}
		if isCandidate {
			break
		}
	}
	if !isCandidate {
		return nil, false, nil
	}
	if len(body.ExtensionOptions) != 1 || body.ExtensionOptions[0] == nil || body.ExtensionOptions[0].TypeUrl != oraclev1.ProposalPayloadTypeURL {
		return nil, true, errors.New("oracle proposal transaction must contain exactly one critical OracleProposalPayload option")
	}

	payload := &oraclev1.OracleProposalPayload{}
	if err := payload.Unmarshal(body.ExtensionOptions[0].Value); err != nil {
		return nil, true, err
	}
	canonical, err := EncodeProposalTx(payload)
	if err != nil {
		return nil, true, err
	}
	if !bytes.Equal(tx, canonical) {
		return nil, true, errors.New("oracle proposal transaction is not canonically encoded")
	}

	return payload, true, nil
}

func IsProposalTx(tx []byte) bool {
	_, isProposal, err := DecodeProposalTx(tx)
	return isProposal && err == nil
}

func isProposalTxCandidate(tx []byte) bool {
	_, isCandidate, _ := DecodeProposalTx(tx)
	return isCandidate
}
