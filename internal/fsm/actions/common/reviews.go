package common

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// RegisterReviews registers reviews-related actions.
func RegisterReviews(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	log *slog.Logger,
	aliasRegistrar func(alias, target string),
) {
	if aliasRegistrar != nil {
		aliasRegistrar("REVIEWS_MENU", "reviews.yaml/REVIEWS_MENU")
	}

	registerReviewActions(registry, cfg, queries, s21Client, credService, log)
	registerTeamActions(registry, cfg, queries, s21Client, credService, log)
}
