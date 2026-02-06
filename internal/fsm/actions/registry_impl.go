package actions

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type registrar struct {
	deps *Dependencies
}

// NewRegistrar creates a new action registrar.
func NewRegistrar(
	cfg *config.Config,
	log *slog.Logger,
	studentSvc service.StudentService,
	queries db.Querier,
	rcClient *rocketchat.Client,
	s21Client *s21.Client,
) ActionRegistrar {
	return &registrar{
		deps: &Dependencies{
			Config:     cfg,
			Log:        log,
			StudentSvc: studentSvc,
			Queries:    queries,
			RCClient:   rcClient,
			S21Client:  s21Client,
		},
	}
}

func (r *registrar) RegisterAll(registry *fsm.LogicRegistry, aliasRegistrar func(alias, target string)) {
	r.deps.AliasRegistrar = aliasRegistrar
	for _, p := range GetPlugins() {
		r.deps.Log.Info("registering plugin", "plugin_id", p.ID())
		p.Register(registry, r.deps)
	}
}
