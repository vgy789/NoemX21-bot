package main

import (
	"os"

	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/app"
	"github.com/vgy789/noemx21-bot/internal/config"
)

const (
	EnvLocal = "local"
	EnvProd  = "prod"
)

func main() {
	log := setupLogger(EnvLocal)
	cfg := config.MustLoad()
	log.Info("Bot started", "version", "0.0.1")
	a := app.New(cfg, log)
	a.Run()
}

// setupLogger sets up the logger.
func setupLogger(env string) *slog.Logger {
	switch env {
	case EnvLocal:
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case EnvProd:
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.Default()
}
