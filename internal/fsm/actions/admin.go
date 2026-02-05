package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func init() {
	Register(NewBasicPlugin("admin", func(registry *fsm.LogicRegistry, deps *Dependencies) {
		if deps.AliasRegistrar != nil {
			deps.AliasRegistrar("ADMIN_MENU", "admin.yaml/ADMIN_MENU")
		}
	}))
}
