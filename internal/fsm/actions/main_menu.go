package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

type mainMenuPlugin struct{}

func (p *mainMenuPlugin) ID() string { return "main_menu" }

func (p *mainMenuPlugin) Register(registry *fsm.LogicRegistry, deps *Dependencies) {
	if deps.AliasRegistrar != nil {
		deps.AliasRegistrar("MAIN_MENU", "main_menu.yaml/MAIN_MENU")
	}
}

func init() {
	Register(&mainMenuPlugin{})
}
