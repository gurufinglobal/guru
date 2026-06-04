package abci

import (
	"bytes"
	"errors"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"google.golang.org/protobuf/proto"
)

var MagicPrefix = []byte("GURU_ORACLE_V1:")

func EncodeProposalTx(payload *oraclev1.OracleProposalPayload) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("oracle proposal payload cannot be nil")
	}

	bz, err := proto.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tx := make([]byte, 0, len(MagicPrefix)+len(bz))
	tx = append(tx, MagicPrefix...)
	tx = append(tx, bz...)
	return tx, nil
}

func DecodeProposalTx(tx []byte) (*oraclev1.OracleProposalPayload, bool, error) {
	if !bytes.HasPrefix(tx, MagicPrefix) {
		return nil, false, nil
	}

	payload := &oraclev1.OracleProposalPayload{}
	if err := proto.Unmarshal(tx[len(MagicPrefix):], payload); err != nil {
		return nil, true, err
	}

	return payload, true, nil
}

func IsProposalTx(tx []byte) bool {
	return bytes.HasPrefix(tx, MagicPrefix)
}
