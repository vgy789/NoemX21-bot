package app

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/config"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
)

// Run starts the application.
func Run(cfg *config.Config, log *slog.Logger) {
	tg := telegram.NewTelegramService(&cfg.Telegram, log)
	tg.Run()
}
