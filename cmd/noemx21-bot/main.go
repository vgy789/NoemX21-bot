package main

import (
	"os"
	"strings"

	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/app"
	"github.com/vgy789/noemx21-bot/internal/config"
)

func main() {
	cfg := config.MustLoad()
	log := setupLogger(cfg.Production, cfg.LogLevel)
	log.Info("Bot started", "version", "0.0.1", "production", cfg.Production, "log_level", cfg.LogLevel)
	a := app.New(cfg, log)
	a.Run()
}

// setupLogger sets up the logger.
func setupLogger(production bool, levelStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	if production {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
