package constitution

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
)

// RegisterMigrations registers in-place store migrations for x/constitution.
func RegisterMigrations(configurator module.Configurator) error {
	return configurator.RegisterMigration(constitutiontypes.ModuleName, 1, migrate1To2)
}

func migrate1To2(_ sdk.Context) error {
	// The version 1-to-2 schema change only added optional state under the new
	// 0x05 prefix; existing version 1 keys require no rewriting.
	return nil
}
