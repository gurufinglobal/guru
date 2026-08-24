package abci

import oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"

func oracleValuesEqual(a, b []*oraclev1.OracleValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GetSymbol() != b[i].GetSymbol() ||
			a[i].GetValueType() != b[i].GetValueType() ||
			a[i].GetValue() != b[i].GetValue() ||
			a[i].GetBlockHeight() != b[i].GetBlockHeight() ||
			a[i].GetBlockTimeUnix() != b[i].GetBlockTimeUnix() {
			return false
		}
	}

	return true
}

func containsPayloadAfterFirst(txs [][]byte) bool {
	for i := 1; i < len(txs); i++ {
		if isProposalTxCandidate(txs[i]) {
			return true
		}
	}

	return false
}

func stripPayloadTxs(txs [][]byte) [][]byte {
	stripped := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		if !isProposalTxCandidate(tx) {
			stripped = append(stripped, tx)
		}
	}

	return stripped
}
