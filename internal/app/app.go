package app

import (
	"log/slog"
	"net/http"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm/setup"
	"github.com/vgy789/noemx21-bot/internal/pkg/imgcache"
	"github.com/vgy789/noemx21-bot/internal/service"
	"github.com/vgy789/noemx21-bot/internal/service/schedule_generator"
	transportHttp "github.com/vgy789/noemx21-bot/internal/transport/http"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
)

// HTTPServer defines the interface for HTTP server operations
type HTTPServer interface {
	Start()
	AddHandler(path string, handler http.Handler)
}

// Starter is implemented by services that can be started (git sync, campus).
type Starter interface {
	Start() error
}

// ScheduleRegenerator is implemented by the schedule generator service.
type ScheduleRegenerator interface {
	ForceRegenerate()
}

// App is the main application.
type App struct {
	tg            telegram.TelegramService
	httpServer    HTTPServer
	gitSync       Starter
	campusSvc     Starter
	scheduleGen   Starter
	scheduleRegen ScheduleRegenerator
	cfg           *config.Config

	log *slog.Logger
}

// New creates a new application instance.
func New(cfg *config.Config, log *slog.Logger, repo *db.DBWrapper, rcClient *rocketchat.Client, s21Client *s21.Client, credService *service.CredentialService, gitSync Starter, campusSvc Starter, scheduleGen Starter, scheduleRegen ScheduleRegenerator, cache *imgcache.Store) *App {

	userSvc := service.NewUserService(repo.Queries)
	engine := setup.NewFSM(cfg, log, repo.Queries, userSvc, rcClient, s21Client, credService, "docs/specs/flows", scheduleRegen)

	tgService := telegram.NewTelegramService(cfg, log, userSvc, engine, cache)
	
	// Set invalidator for schedule generator to clear cached file_ids on regeneration
	scheduleGen.(*schedule_generator.Service).SetInvalidator(tgService)

	return &App{
		tg:            tgService,
		httpServer:    transportHttp.NewServer(cfg, log, repo.Queries),
		gitSync:       gitSync,
		campusSvc:     campusSvc,
		scheduleGen:   scheduleGen,
		scheduleRegen: scheduleRegen,
		cfg:           cfg,
		log:           log,
	}

}

// Run starts the application.
func (a *App) Run() {
	if err := a.gitSync.Start(); err != nil {
		slog.Error("failed to start gitsync", "error", err)
	}
	if err := a.campusSvc.Start(); err != nil {
		slog.Error("failed to start campus service", "error", err)
	}
	if err := a.scheduleGen.Start(); err != nil {
		slog.Error("failed to start schedule generator", "error", err)
	}

	go a.httpServer.Start()

	// Check if webhook mode is enabled
	if a.cfg.Telegram.Webhook.Enabled {
		a.log.Info("starting bot in webhook mode", "path", a.cfg.Telegram.Webhook.ListenPath, "port", a.cfg.Telegram.Webhook.ListenPort)
		// Register webhook handler with HTTP server
		a.httpServer.AddHandler(a.cfg.Telegram.Webhook.ListenPath, a.tg.GetWebhookHandler())
		// Start webhook (this will set webhook URL with Telegram and start listening)
		if err := a.tg.RunWebhook(); err != nil {
			a.log.Error("failed to start webhook", "error", err)
		}
	} else {
		a.log.Info("starting bot in polling mode")
		a.tg.Run()
	}
}
