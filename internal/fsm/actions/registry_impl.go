package actions

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type registrar struct {
	cfg        *config.Config
	log        *slog.Logger
	studentSvc service.StudentService
	queries    db.Querier
	rcClient   *rocketchat.Client
}

// NewRegistrar creates a new action registrar.
func NewRegistrar(
	cfg *config.Config,
	log *slog.Logger,
	studentSvc service.StudentService,
	queries db.Querier,
	rcClient *rocketchat.Client,
) ActionRegistrar {
	return &registrar{
		cfg:        cfg,
		log:        log,
		studentSvc: studentSvc,
		queries:    queries,
		rcClient:   rcClient,
	}
}

func (r *registrar) RegisterAll(registry *fsm.LogicRegistry) {
	// Base actions
	RegisterBaseActions(registry, r.log)

	// Settings actions
	RegisterSettingsActions(registry, r.log, r.queries)

	// Registration actions
	RegisterRegistrationActions(registry, r.cfg, r.log, r.studentSvc, r.queries, r.rcClient)
}
