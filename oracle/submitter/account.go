package submitter

type AccountInfo struct {
	accountNumber  uint64
	sequenceNumber uint64
}

func NewAccountInfo(accountNumber uint64, sequenceNumber uint64) *AccountInfo {
	return &AccountInfo{
		accountNumber:  accountNumber,
		sequenceNumber: sequenceNumber,
	}
}

func (a *AccountInfo) AccountNumber() uint64 {
	return a.accountNumber
}

func (a *AccountInfo) CurrentSequenceNumber() uint64 {
	return a.sequenceNumber
}

func (a *AccountInfo) IncrementSequenceNumber() {
	a.sequenceNumber++
}

func (a *AccountInfo) DecrementSequenceNumber() {
	a.sequenceNumber--
}

func (a *AccountInfo) UpdateSequenceNumber(sequenceNumber uint64) {
	a.sequenceNumber = sequenceNumber
}
