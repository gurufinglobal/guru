package keeper

import (
	"context"

	corestore "cosmossdk.io/core/store"
)

func bytesOf(b byte) []byte {
	out := make([]byte, 20)
	for i := range out {
		out[i] = b
	}
	return out
}

type storeFault struct {
	op     string
	prefix byte
	skip   int
	err    error
}

type faultStoreService struct {
	base  corestore.KVStoreService
	fault *storeFault
}

func (s faultStoreService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return faultKVStore{KVStore: s.base.OpenKVStore(ctx), fault: s.fault}
}

type faultKVStore struct {
	corestore.KVStore
	fault *storeFault
}

func (s faultKVStore) fail(op string, key []byte) error {
	if s.fault == nil || s.fault.op != op {
		return nil
	}
	if s.fault.prefix != 0 && (len(key) == 0 || key[0] != s.fault.prefix) {
		return nil
	}
	if s.fault.skip > 0 {
		s.fault.skip--
		return nil
	}
	return s.fault.err
}

func (s faultKVStore) Get(key []byte) ([]byte, error) {
	if err := s.fail("get", key); err != nil {
		return nil, err
	}
	return s.KVStore.Get(key)
}

func (s faultKVStore) Has(key []byte) (bool, error) {
	if err := s.fail("has", key); err != nil {
		return false, err
	}
	return s.KVStore.Has(key)
}

func (s faultKVStore) Set(key, value []byte) error {
	if err := s.fail("set", key); err != nil {
		return err
	}
	return s.KVStore.Set(key, value)
}

func (s faultKVStore) Delete(key []byte) error {
	if err := s.fail("delete", key); err != nil {
		return err
	}
	return s.KVStore.Delete(key)
}

func (s faultKVStore) Iterator(start, end []byte) (corestore.Iterator, error) {
	if err := s.fail("iterator", start); err != nil {
		return nil, err
	}
	return s.KVStore.Iterator(start, end)
}

func (s faultKVStore) ReverseIterator(start, end []byte) (corestore.Iterator, error) {
	if err := s.fail("reverse_iterator", start); err != nil {
		return nil, err
	}
	return s.KVStore.ReverseIterator(start, end)
}
