package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func init() {
	Register(NewBasicPlugin("library", func(registry *fsm.LogicRegistry, deps *Dependencies) {
		if deps.AliasRegistrar != nil {
			deps.AliasRegistrar("LIBRARY_MENU", "library.yaml/LIBRARY_MENU")
		}
	}))
}
