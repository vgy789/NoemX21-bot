package setup

import (
	"log/slog"
	"os"
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

	// Safety check: refuse to start if test mode is enabled in production
	if cfg.TestModeNoOTP && cfg.Production {
		log.Error("TEST_MODE_NO_OTP is enabled in production - refusing to start")
		os.Exit(1)
	}

	// Create OTP provider based on configuration
	otpService := service.NewOTPService(queries, rcClient, cfg, log)
	var otpProvider service.OTPProvider
	if cfg.TestModeNoOTP {
		log.Info("using mock OTP provider (test mode)")
		otpProvider = service.NewMockOTPProvider(log)
	} else {
		log.Info("using real OTP provider (production mode)")
		otpProvider = service.NewRealOTPProvider(otpService)
	}

	// Create registry and register actions
	registry := fsm.NewLogicRegistry()
	registrar := actions.NewRegistrar(cfg, log, userSvc, queries, rcClient, s21Client, credService, otpProvider, repoFSM)

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
