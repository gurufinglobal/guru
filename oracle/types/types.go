package types

type OracleTask struct {
	Id       uint64
	Category int32
	Symbol   string
	Nonce    uint64
}

type OracleTaskResult struct {
	Id        uint64
	Value     string
	Nonce     uint64
	Signature []byte
}
