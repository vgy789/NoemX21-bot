package admin

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers admin-related actions.
func Register(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("ADMIN_MENU", "admin.yaml/ADMIN_MENU")
	}
}
