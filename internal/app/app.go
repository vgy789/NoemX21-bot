package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm/setup"
	"github.com/vgy789/noemx21-bot/internal/pkg/imgcache"
	"github.com/vgy789/noemx21-bot/internal/service"
	"github.com/vgy789/noemx21-bot/internal/service/schedule_generator"
	transportHttp "github.com/vgy789/noemx21-bot/internal/transport/http"
	telegram "github.com/vgy789/noemx21-bot/internal/transport/telegram"
	"golang.org/x/sync/errgroup"
)

// HTTPServer defines the interface for HTTP server operations
type HTTPServer interface {
	Start(ctx context.Context) error
	AddHandler(path string, handler http.Handler)
}

// Starter is implemented by services that can be started (git sync, campus).
type Starter interface {
	Start() error
}

// Stopper is implemented by services that can be stopped (cron-based workers).
type Stopper interface {
	Stop()
}

// ScheduleRegenerator is implemented by the schedule generator service.
type ScheduleRegenerator interface {
	ForceRegenerate()
}

// App is the main application.
type App struct {
	tg            telegram.TelegramService
	httpServer    HTTPServer
	gitSync       Starter
	campusSvc     Starter
	scheduleGen   Starter
	scheduleRegen ScheduleRegenerator
	cfg           *config.Config

	log *slog.Logger
}

const telegramRestartDelay = 3 * time.Second

// New creates a new application instance.
func New(cfg *config.Config, log *slog.Logger, repo *db.DBWrapper, rcClient *rocketchat.Client, s21Client *s21.Client, credService *service.CredentialService, gitSync Starter, campusSvc Starter, scheduleGen Starter, scheduleRegen ScheduleRegenerator, cache *imgcache.Store) *App {

	userSvc := service.NewUserService(repo.Queries)
	engine := setup.NewFSM(cfg, log, repo.Queries, userSvc, rcClient, s21Client, credService, "docs/specs/flows", scheduleRegen)

	tgService := telegram.NewTelegramService(cfg, log, userSvc, repo.Queries, engine, cache)

	if setter, ok := campusSvc.(interface {
		SetPRRStatusBroadcaster(service.PRRStatusBroadcaster)
	}); ok {
		setter.SetPRRStatusBroadcaster(tgService)
	}

	// Set invalidator for schedule generator to clear cached file_ids on regeneration.
	if setter, ok := scheduleGen.(interface {
		SetInvalidator(schedule_generator.ScheduleInvalidator)
	}); ok {
		setter.SetInvalidator(tgService)
	}

	return &App{
		tg:            tgService,
		httpServer:    transportHttp.NewServer(cfg, log, repo.Queries),
		gitSync:       gitSync,
		campusSvc:     campusSvc,
		scheduleGen:   scheduleGen,
		scheduleRegen: scheduleRegen,
		cfg:           cfg,
		log:           log,
	}
}

// Run starts the application and blocks until the provided context is cancelled
// or any component returns an error.
func (a *App) Run(ctx context.Context) error {
	a.log.Info("starting bot runtime", "production", a.cfg.Production, "log_level", a.cfg.LogLevel)

	group, ctx := errgroup.WithContext(ctx)

	startBackground := func(name string, svc Starter) {
		group.Go(func() error {
			if err := svc.Start(); err != nil {
				a.log.Error("failed to start component", "component", name, "error", err)
				return err
			}

			<-ctx.Done()
			if stopper, ok := svc.(Stopper); ok {
				stopper.Stop()
			}
			return nil
		})
	}

	startBackground("gitsync", a.gitSync)
	startBackground("campus", a.campusSvc)
	startBackground("schedule_generator", a.scheduleGen)

	group.Go(func() error {
		err := a.httpServer.Start(ctx)
		if err != nil {
			a.log.Error("http server exited with error", "error", err)
		}
		return err
	})

	if a.cfg.Telegram.Webhook.Enabled {
		a.log.Info("starting bot in webhook mode", "path", a.cfg.Telegram.Webhook.ListenPath, "port", a.cfg.Telegram.Webhook.ListenPort)
		a.httpServer.AddHandler(a.cfg.Telegram.Webhook.ListenPath, a.tg.GetWebhookHandler())
		group.Go(func() error {
			return a.runTelegramWithRestart(ctx, true)
		})
	} else {
		a.log.Info("starting bot in polling mode")
		group.Go(func() error {
			return a.runTelegramWithRestart(ctx, false)
		})
	}

	return group.Wait()
}

func (a *App) runTelegramWithRestart(ctx context.Context, webhook bool) error {
	for {
		var err error
		if webhook {
			err = a.tg.RunWebhook(ctx)
		} else {
			err = a.tg.Run(ctx)
		}

		if err == nil {
			if ctx.Err() != nil {
				return nil
			}
			a.log.Warn("telegram runner exited without error; restarting", "mode", telegramMode(webhook), "retry_in", telegramRestartDelay)
		} else {
			a.log.Error(
				fmt.Sprintf("telegram %s exited with error", telegramMode(webhook)),
				"error", err,
				"retry_in", telegramRestartDelay,
			)
		}

		if waitErr := waitForRetry(ctx, telegramRestartDelay); waitErr != nil {
			return nil
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func telegramMode(webhook bool) string {
	if webhook {
		return "webhook"
	}
	return "polling"
}
