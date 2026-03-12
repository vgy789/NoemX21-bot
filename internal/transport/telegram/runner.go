package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/clients/telegram"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/pkg/imgcache"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type TelegramService interface {
	Run(ctx context.Context) error
	RunWebhook(ctx context.Context) error
}

// Sender defines interface for sending messages to Telegram.
type Sender interface {
	SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error)
	SendPhoto(chatID int64, photo gotgbot.InputFileOrString, opts *gotgbot.SendPhotoOpts) (*gotgbot.Message, error)
	EditMessageText(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error)
	DeleteMessage(chatID int64, messageID int64) (bool, error)
	AnswerCallbackQuery(callbackQueryId string, opts *gotgbot.AnswerCallbackQueryOpts) (bool, error)
}

// DefaultSender is the default implementation of Sender using gotgbot.Bot.
type DefaultSender struct {
	Bot *gotgbot.Bot
}

func (s *DefaultSender) SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
	return s.Bot.SendMessage(chatID, text, opts)
}

func (s *DefaultSender) SendPhoto(chatID int64, photo gotgbot.InputFileOrString, opts *gotgbot.SendPhotoOpts) (*gotgbot.Message, error) {
	return s.Bot.SendPhoto(chatID, photo, opts)
}

func (s *DefaultSender) EditMessageText(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
	return s.Bot.EditMessageText(text, opts)
}

func (s *DefaultSender) DeleteMessage(chatID int64, messageID int64) (bool, error) {
	return s.Bot.DeleteMessage(chatID, messageID, nil)
}

func (s *DefaultSender) AnswerCallbackQuery(id string, opts *gotgbot.AnswerCallbackQueryOpts) (bool, error) {
	return s.Bot.AnswerCallbackQuery(id, opts)
}

// NewTelegramService creates new telegram service.
func NewTelegramService(cfg *config.Config, log *slog.Logger, userSvc service.UserService, queries db.Querier, engine *fsm.Engine, cache *imgcache.Store) *telegramService {
	return &telegramService{
		cfg:      cfg,
		log:      log,
		userSvc:  userSvc,
		queries:  queries,
		engine:   engine,
		imgCache: cache,
		fileIDs:  make(map[string]string),
	}
}

type telegramService struct {
	cfg       *config.Config
	log       *slog.Logger
	userSvc   service.UserService
	queries   db.Querier
	engine    *fsm.Engine
	imgCache  *imgcache.Store
	sender    Sender // For testing
	bot       *gotgbot.Bot
	updater   *ext.Updater
	updaterMu sync.RWMutex
	fileIDs   map[string]string
	fileIDsMu sync.RWMutex
}

func telegramAllowedUpdates() []string {
	return []string{
		"message",
		"callback_query",
		"chat_member",
		"my_chat_member",
	}
}

func (s *telegramService) getSender(b *gotgbot.Bot) Sender {
	if s.sender != nil {
		return s.sender
	}
	return &DefaultSender{Bot: b}
}

func (s *telegramService) setUpdater(updater *ext.Updater) {
	s.updaterMu.Lock()
	defer s.updaterMu.Unlock()
	s.updater = updater
}

// Run starts the telegram bot using long polling.
func (s *telegramService) Run(ctx context.Context) error {
	tgBot, err := telegram.New(&s.cfg.Telegram)
	if err != nil {
		return err
	}
	s.bot = tgBot

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			errMsg := config.RedactString(err.Error(), s.cfg.Telegram.Token, s.cfg.Telegram.Webhook.Secret)
			s.log.Error("an error occurred while handling update",
				"error", errMsg,
				"user_id", ctx.EffectiveUser.Id,
				"chat_id", ctx.EffectiveChat.Id,
			)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: s.cfg.Telegram.Polling.MaxRoutines,
	})

	updater := ext.NewUpdater(dispatcher, &ext.UpdaterOpts{
		UnhandledErrFunc: func(err error) {
			errMsg := config.RedactString(err.Error(), s.cfg.Telegram.Token, s.cfg.Telegram.Webhook.Secret)
			s.log.Error("telegram polling error", "error", errMsg)
			time.Sleep(time.Second)
		},
	})
	s.setUpdater(updater)
	s.registerHandlers(dispatcher)

	err = updater.StartPolling(tgBot, &ext.PollingOpts{
		DropPendingUpdates: s.cfg.Telegram.Polling.DropPendingUpdates,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			AllowedUpdates: telegramAllowedUpdates(),
			Timeout:        s.cfg.Telegram.Polling.Timeout,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: s.cfg.Telegram.Polling.RequestTimeout,
			},
		},
	})
	if err != nil {
		errMsg := config.RedactString(err.Error(), s.cfg.Telegram.Token, s.cfg.Telegram.Webhook.Secret)
		return fmt.Errorf("failed to start polling: %s", errMsg)
	}
	s.log.Info("start polling")

	done := make(chan struct{})
	go func() {
		updater.Idle()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return updater.Stop()
	case <-done:
		return nil
	}
}

// RunWebhook sets up and starts the telegram bot using webhook.
func (s *telegramService) RunWebhook(ctx context.Context) error {
	tgBot, err := telegram.New(&s.cfg.Telegram)
	if err != nil {
		return err
	}
	s.bot = tgBot

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			errMsg := config.RedactString(err.Error(), s.cfg.Telegram.Token, s.cfg.Telegram.Webhook.Secret)
			s.log.Error("an error occurred while handling update",
				"error", errMsg,
				"user_id", ctx.EffectiveUser.Id,
				"chat_id", ctx.EffectiveChat.Id,
			)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: s.cfg.Telegram.Polling.MaxRoutines,
	})

	updater := ext.NewUpdater(dispatcher, nil)
	s.setUpdater(updater)
	s.registerHandlers(dispatcher)

	// Set webhook URL with Telegram
	webhookURL := s.cfg.Telegram.Webhook.URL
	if webhookURL == "" {
		return fmt.Errorf("TELEGRAM_WEBHOOK_URL must be set when webhook mode is enabled")
	}

	var secretToken string
	if s.cfg.Telegram.Webhook.Secret.Expose() != "" {
		secretToken = s.cfg.Telegram.Webhook.Secret.Expose()
	}

	_, err = tgBot.SetWebhook(webhookURL, &gotgbot.SetWebhookOpts{
		SecretToken:    secretToken,
		AllowedUpdates: telegramAllowedUpdates(),
	})
	if err != nil {
		errMsg := config.RedactString(err.Error(), s.cfg.Telegram.Token, s.cfg.Telegram.Webhook.Secret)
		return fmt.Errorf("failed to set webhook: %s", errMsg)
	}

	s.log.Info("webhook set successfully", "url", webhookURL)

	// Start webhook server
	listenAddr := fmt.Sprintf(":%d", s.cfg.Telegram.Webhook.ListenPort)
	err = updater.StartWebhook(tgBot, s.cfg.Telegram.Webhook.ListenPath, ext.WebhookOpts{
		ListenAddr:  listenAddr,
		SecretToken: secretToken,
	})
	if err != nil {
		errMsg := config.RedactString(err.Error(), s.cfg.Telegram.Token, s.cfg.Telegram.Webhook.Secret)
		return fmt.Errorf("failed to start webhook: %s", errMsg)
	}

	s.log.Info("webhook server started", "path", s.cfg.Telegram.Webhook.ListenPath, "addr", listenAddr)

	done := make(chan struct{})
	go func() {
		updater.Idle()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return updater.Stop()
	case <-done:
		return nil
	}
}

// InvalidateScheduleFileID removes cached file_id for a schedule image.
// Called when schedule is regenerated to ensure users receive updated image.
func (s *telegramService) InvalidateScheduleFileID(campusShortName string) {
	s.log.Info("invalidating cached file_id for schedule", "campus", campusShortName)
	// Build the same key used in sendRender
	// The key is the file path: tmp/schedules/{timezone}/{campus}.png
	// We need to find all cached keys for this campus and remove them
	s.fileIDsMu.Lock()
	defer s.fileIDsMu.Unlock()

	imageCacheKey := "imgcache:schedule:" + campusShortName

	for key := range s.fileIDs {
		if strings.HasSuffix(key, campusShortName+".png") || key == imageCacheKey {
			delete(s.fileIDs, key)
			s.log.Info("invalidated cached file_id for schedule", "campus", campusShortName, "key", key)
		}
	}
}
