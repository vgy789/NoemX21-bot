package main

import (
	"context"
	"os"
	"strings"

	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/app"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

func main() {
	cfg := config.MustLoad()
	log := setupLogger(cfg.Production, cfg.LogLevel)
	log.Info("Bot started", "version", "0.0.1", "production", cfg.Production, "log_level", cfg.LogLevel)

	// Initialize Database
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DBURL.Expose())
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := db.NewDBWrapper(pool)

	a := app.New(cfg, log, repo)
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
