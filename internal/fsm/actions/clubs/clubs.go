package clubs

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers clubs-related actions.
func Register(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("CLUBS_MENU", "clubs.yaml/CLUBS_MENU")
	}
}
