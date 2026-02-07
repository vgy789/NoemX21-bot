package booking

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers booking-related actions.
func Register(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("BOOKING_MENU", "booking.yaml/BOOKING_MENU")
	}
}
