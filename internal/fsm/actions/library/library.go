package library

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers library-related actions.
func Register(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("LIBRARY_MENU", "library.yaml/LIBRARY_MENU")
	}
}
