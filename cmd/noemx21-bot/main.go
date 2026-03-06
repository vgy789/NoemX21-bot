package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database"
	"github.com/vgy789/noemx21-bot/internal/initialization"
)

func main() {
	// Parse command-line flags
	migrate := flag.Bool("migrate", false, "Apply database migrations and exit")
	migrateRollback := flag.Bool("migrate-rollback", false, "Rollback the last migration and exit")
	migrateStatus := flag.Bool("migrate-status", false, "Show migration status and exit")
	flag.Parse()

	cfg := config.MustLoad()
	log := initialization.SetupLogger(cfg.Production, cfg.LogLevel)

	ctx := context.Background()

	builder := initialization.NewBuilder().
		WithContext(ctx).
		WithConfig(cfg).
		WithLogger(log)

	if *migrate || *migrateRollback || *migrateStatus {
		repo, err := builder.BuildDatabase()
		if err != nil {
			log.Error("failed to connect to database", "error", err)
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
		defer repo.Pool.Close()
		if err := database.Run(ctx, repo.Pool, log, *migrate, *migrateRollback, *migrateStatus); err != nil {
			log.Error("migration failed", "error", err)
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := builder.Run(); err != nil {
		log.Error("application runtime failed", "error", err)
		fmt.Fprintf(os.Stderr, "Application failed: %v\n", err)
		os.Exit(1)
	}
}
