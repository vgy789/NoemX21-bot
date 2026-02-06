package app

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service"
	transportHttp "github.com/vgy789/noemx21-bot/internal/transport/http"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
)

// HTTPServer defines the interface for HTTP server operations
type HTTPServer interface {
	Start()
}

// App is the main application.
type App struct {
	tg         telegram.TelegramService
	httpServer HTTPServer
}

// New creates a new application instance.
func New(cfg *config.Config, log *slog.Logger, repo *db.DBWrapper, rcClient *rocketchat.Client, s21Client *s21.Client) *App {
	studentSvc := service.NewStudentService(repo.Queries)
	return &App{
		tg:         telegram.NewTelegramService(cfg, log, studentSvc, repo.Queries, rcClient, s21Client),
		httpServer: transportHttp.NewServer(cfg, log, repo.Queries),
	}
}

// Run starts the application.
func (a *App) Run() {
	go a.httpServer.Start()
	a.tg.Run()
}
