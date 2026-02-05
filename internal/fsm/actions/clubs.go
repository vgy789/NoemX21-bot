package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func init() {
	Register(NewBasicPlugin("clubs", func(registry *fsm.LogicRegistry, deps *Dependencies) {
		if deps.AliasRegistrar != nil {
			deps.AliasRegistrar("CLUBS_MENU", "clubs.yaml/CLUBS_MENU")
		}
	}))
}
