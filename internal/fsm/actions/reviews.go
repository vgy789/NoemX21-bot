package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func init() {
	Register(NewBasicPlugin("reviews", func(registry *fsm.LogicRegistry, deps *Dependencies) {
		if deps.AliasRegistrar != nil {
			deps.AliasRegistrar("REVIEWS_MENU", "reviews.yaml/REVIEW_MENU")
		}
	}))
}
