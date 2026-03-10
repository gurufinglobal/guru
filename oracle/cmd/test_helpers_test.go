package cmd

import (
	"sync"
	"testing"
)

var homeMu sync.Mutex

func withHomeBase(t *testing.T, v string, fn func()) {
	t.Helper()
	homeMu.Lock()
	defer homeMu.Unlock()

	prev := homeBase
	homeBase = v
	t.Cleanup(func() { homeBase = prev })

	fn()
}
