package telegram

import (
	"log/slog"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/clients/telegram"
	"github.com/vgy789/noemx21-bot/internal/config"
)

//go:generate mockgen -source=$GOFILE -destination=mock/runner_mock.go -package=mock
type TelegramService interface {
	Run()
}

// NewTelegramService creates new telegram service.
func NewTelegramService(cfg *config.TelegramBot, log *slog.Logger) TelegramService {
	return &telegramService{
		cfg: cfg,
		log: log,
	}
}

// telegramService is a service that handles telegram updates.
type telegramService struct {
	cfg *config.TelegramBot
	log *slog.Logger
}

// Run starts the telegram bot.
func (s *telegramService) Run() {
	tgBot := telegram.MustNew(s.cfg)

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			s.log.Error("an error occurred while handling update",
				"error", err,
				"user_id", ctx.EffectiveUser.Id,
				"chat_id", ctx.EffectiveChat.Id,
			)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: s.cfg.Polling.MaxRoutines,
	})

	updater := ext.NewUpdater(dispatcher, nil)
	s.registerHandlers(dispatcher)

	err := updater.StartPolling(tgBot, &ext.PollingOpts{
		DropPendingUpdates: s.cfg.Polling.DropPendingUpdates,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: s.cfg.Polling.Timeout,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: s.cfg.Polling.RequestTimeout,
			},
		},
	})
	if err != nil {
		panic("failed to start polling: " + err.Error())
	}
	s.log.Info("start polling")
	updater.Idle()
}
