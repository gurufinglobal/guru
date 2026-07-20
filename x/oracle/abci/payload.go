package abci

import (
	"bytes"
	"errors"

	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

const magicPrefix = "GURU_ORACLE_V1:"

func EncodeProposalTx(payload *oraclev1.OracleProposalPayload) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("oracle proposal payload cannot be nil")
	}

	bz, err := payload.Marshal()
	if err != nil {
		return nil, err
	}

	tx := make([]byte, 0, len(magicPrefix)+len(bz))
	tx = append(tx, magicPrefix...)
	tx = append(tx, bz...)
	return tx, nil
}

func DecodeProposalTx(tx []byte) (*oraclev1.OracleProposalPayload, bool, error) {
	prefix := []byte(magicPrefix)
	if !bytes.HasPrefix(tx, prefix) {
		return nil, false, nil
	}

	payload := &oraclev1.OracleProposalPayload{}
	if err := payload.Unmarshal(tx[len(prefix):]); err != nil {
		return nil, true, err
	}

	return payload, true, nil
}

func IsProposalTx(tx []byte) bool {
	return bytes.HasPrefix(tx, []byte(magicPrefix))
}
