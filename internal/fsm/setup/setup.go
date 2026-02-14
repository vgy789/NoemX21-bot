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
	userSvc service.UserService,
	rcClient *rocketchat.Client,
	s21Client *s21.Client,
	credService *service.CredentialService,
	flowsPath string,
) *fsm.Engine {
	// Initialize FSM components
	parser := fsm.NewFlowParser(flowsPath, log)
	repoFSM := fsm.NewPostgreSQLStateRepository(queries)

	// Create registry and register actions
	registry := fsm.NewLogicRegistry()
	registrar := actions.NewRegistrar(cfg, log, userSvc, queries, rcClient, s21Client, credService, repoFSM)

	// Initialize Engine with sanitizer: escape Markdown specials in values from context/DB
	// so that e.g. campus "24_04_NSK" is not interpreted as italic in Telegram.
	sanitizer := escapeMarkdownForTelegram

	engine := fsm.NewEngine(parser, repoFSM, log, registry, sanitizer)

	// Register all actions and aliases from plugins
	registrar.RegisterAll(registry, engine.AddAlias)

	return engine
}

// escapeMarkdownForTelegram escapes characters that Telegram treats as Markdown
// when ParseMode is Markdown, so values from DB (e.g. campus "24_04_NSK") display literally.
func escapeMarkdownForTelegram(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '_', '*', '`', '[':
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
