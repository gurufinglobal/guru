package gurud

import (
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/gurufinglobal/guru/v2/gurud/eips"
)

const (
	// Use a high, chain-local range to avoid collisions with upstream/default EIP IDs.
	GuruEIPCreateMultiplier = 770001
	GuruEIPCallMultiplier   = 770002
	GuruEIPSstoreConstant   = 770003
)

// guruActivators defines a map of opcode modifiers associated
// with a key defining the corresponding EIP.
var guruActivators = map[int]func(*vm.JumpTable){
	GuruEIPCreateMultiplier: eips.Enable0000,
	GuruEIPCallMultiplier:   eips.Enable0001,
	GuruEIPSstoreConstant:   eips.Enable0002,
}

// registerGuruActivators injects chain-local activators into the global EVM activator registry.
// It is safe to call multiple times and across test resets.
func registerGuruActivators() {
	for eipNumber, activator := range guruActivators {
		if vm.ValidEip(eipNumber) {
			continue
		}

		if err := vm.ExtendActivators(map[int]func(*vm.JumpTable){eipNumber: activator}); err != nil {
			panic(fmt.Errorf("failed to register guru activators: %w", err))
		}
	}
}

func guruExtraEIPs() []int64 {
	keys := make([]int, 0, len(guruActivators))
	for key := range guruActivators {
		keys = append(keys, key)
	}

	sort.Ints(keys)
	extraEIPs := make([]int64, 0, len(keys))
	for _, key := range keys {
		extraEIPs = append(extraEIPs, int64(key))
	}

	return extraEIPs
}
