package app

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm/setup"
	"github.com/vgy789/noemx21-bot/internal/service"
	transportHttp "github.com/vgy789/noemx21-bot/internal/transport/http"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
)

// HTTPServer defines the interface for HTTP server operations
type HTTPServer interface {
	Start()
}

// Starter is implemented by services that can be started (git sync, campus).
type Starter interface {
	Start() error
}

// App is the main application.
type App struct {
	tg         telegram.TelegramService
	httpServer HTTPServer
	gitSync    Starter
	campusSvc  Starter
}

// New creates a new application instance.
func New(cfg *config.Config, log *slog.Logger, repo *db.DBWrapper, rcClient *rocketchat.Client, s21Client *s21.Client, credService *service.CredentialService, gitSync Starter, campusSvc Starter) *App {
	userSvc := service.NewUserService(repo.Queries)
	engine := setup.NewFSM(cfg, log, repo.Queries, userSvc, rcClient, s21Client, credService, "docs/specs/flows")

	return &App{
		tg:         telegram.NewTelegramService(cfg, log, userSvc, engine),
		httpServer: transportHttp.NewServer(cfg, log, repo.Queries),
		gitSync:    gitSync,
		campusSvc:  campusSvc,
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
	go a.httpServer.Start()
	a.tg.Run()
}
