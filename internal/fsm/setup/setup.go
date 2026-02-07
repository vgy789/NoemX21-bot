package setup

import (
	"log/slog"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// NewFSM creates and initializes the FSM engine.
func NewFSM(
	cfg *config.Config,
	log *slog.Logger,
	queries db.Querier,
	studentSvc service.StudentService,
	rcClient *rocketchat.Client,
	s21Client *s21.Client,
	flowsPath string,
) *fsm.Engine {
	// Initialize FSM components
	parser := fsm.NewFlowParser(flowsPath, log)
	repoFSM := fsm.NewPostgreSQLStateRepository(queries)

	// Create registry and register actions
	registry := fsm.NewLogicRegistry()
	registrar := actions.NewRegistrar(cfg, log, studentSvc, queries, rcClient, s21Client)

	// Initialize Engine with sanitizer
	sanitizer := func(text string) string {
		return strings.ReplaceAll(text, "_", "\\_")
	}

	engine := fsm.NewEngine(parser, repoFSM, log, registry, sanitizer)

	// Register all actions and aliases from plugins
	registrar.RegisterAll(registry, engine.AddAlias)

	return engine
}
