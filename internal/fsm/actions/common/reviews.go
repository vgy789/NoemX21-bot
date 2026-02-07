package common

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// RegisterReviews registers reviews-related actions.
func RegisterReviews(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("REVIEWS_MENU", "reviews.yaml/REVIEW_MENU")
	}
}
