package main

import (
	"context"
	"os"

	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/initialization"
)

func main() {
	cfg := config.MustLoad()
	log := initialization.SetupLogger(cfg.Production, cfg.LogLevel)

	ctx := context.Background()

	builder := initialization.NewBuilder().
		WithContext(ctx).
		WithConfig(cfg).
		WithLogger(log)

	if err := builder.Run(); err != nil {
		os.Exit(1)
	}
}
