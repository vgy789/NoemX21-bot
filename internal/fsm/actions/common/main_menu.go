package common

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// RegisterMainMenu registers main menu-related actions.
func RegisterMainMenu(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("MAIN_MENU", "main_menu.yaml/MAIN_MENU")
	}
}
