package app

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
)

// App is the main application.
type App struct {
	tg telegram.TelegramService
}

// New creates a new application instance.
func New(cfg *config.Config, log *slog.Logger, repo *db.DBWrapper) *App {
	studentSvc := service.NewStudentService(repo)
	return &App{
		tg: telegram.NewTelegramService(&cfg.Telegram, log, studentSvc),
	}
}

// Run starts the application.
func (a *App) Run() {
	a.tg.Run()
}
