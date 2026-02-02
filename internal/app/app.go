package app

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/config"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
)

// App is the main application.
type App struct {
	tg telegram.TelegramService
}

// New creates a new application instance.
func New(cfg *config.Config, log *slog.Logger) *App {
	return &App{
		tg: telegram.NewTelegramService(&cfg.Telegram, log),
	}
}

// Run starts the application.
func (a *App) Run() {
	a.tg.Run()
}
