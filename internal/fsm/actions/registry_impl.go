package actions

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/admin"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/booking"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/clubs"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/common"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/library"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/registration"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/settings"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/statistics"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type registrar struct {
	cfg         *config.Config
	log         *slog.Logger
	userSvc     service.UserService
	queries     db.Querier
	rcClient    *rocketchat.Client
	s21Client   *s21.Client
	credService *service.CredentialService
	otpProvider service.OTPProvider
	repo        fsm.StateRepository
}

// NewRegistrar creates a new action registrar.
func NewRegistrar(
	cfg *config.Config,
	log *slog.Logger,
	userSvc service.UserService,
	queries db.Querier,
	rcClient *rocketchat.Client,
	s21Client *s21.Client,
	credService *service.CredentialService,
	otpProvider service.OTPProvider,
	repo fsm.StateRepository,
) ActionRegistrar {
	return &registrar{
		cfg:         cfg,
		log:         log,
		userSvc:     userSvc,
		queries:     queries,
		rcClient:    rcClient,
		s21Client:   s21Client,
		credService: credService,
		otpProvider: otpProvider,
		repo:        repo,
	}
}

func (r *registrar) RegisterAll(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	common.RegisterBase(registry)
	common.RegisterMainMenu(registry, aliasRegistrar)
	common.RegisterReviews(registry, aliasRegistrar)

	admin.Register(registry, aliasRegistrar)
	booking.Register(registry, r.queries, r.cfg, aliasRegistrar)

	clubs.Register(registry, r.queries, aliasRegistrar)
	library.Register(registry, r.queries, aliasRegistrar)

	registration.Register(registry, r.cfg, r.log, r.queries, r.userSvc, r.rcClient, r.s21Client, r.credService, r.otpProvider, aliasRegistrar)
	settings.Register(registry, r.log, r.queries, aliasRegistrar)
	statistics.Register(registry, r.cfg, r.log, r.queries, r.s21Client, r.credService, r.repo, aliasRegistrar)
}
