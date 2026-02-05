package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// ActionRegistrar defines the interface for registering FSM actions.
type ActionRegistrar interface {
	RegisterAll(registry *fsm.LogicRegistry)
}
