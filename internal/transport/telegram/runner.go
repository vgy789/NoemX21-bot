package telegram

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/clients/telegram"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type TelegramService interface {
	Run()
	RunWebhook() error
	GetWebhookHandler() http.Handler
}

// Sender defines interface for sending messages to Telegram.
type Sender interface {
	SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error)
	SendPhoto(chatID int64, photo gotgbot.InputFile, opts *gotgbot.SendPhotoOpts) (*gotgbot.Message, error)
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

func (s *DefaultSender) SendPhoto(chatID int64, photo gotgbot.InputFile, opts *gotgbot.SendPhotoOpts) (*gotgbot.Message, error) {
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
func NewTelegramService(cfg *config.Config, log *slog.Logger, userSvc service.UserService, engine *fsm.Engine) TelegramService {
	return &telegramService{
		cfg:     cfg,
		log:     log,
		userSvc: userSvc,
		engine:  engine,
	}
}

type telegramService struct {
	cfg     *config.Config
	log     *slog.Logger
	userSvc service.UserService
	engine  *fsm.Engine
	sender  Sender // For testing
	bot     *gotgbot.Bot
	updater *ext.Updater
}

func (s *telegramService) getSender(b *gotgbot.Bot) Sender {
	if s.sender != nil {
		return s.sender
	}
	return &DefaultSender{Bot: b}
}

// Run starts the telegram bot using long polling.
func (s *telegramService) Run() {
	tgBot := telegram.MustNew(&s.cfg.Telegram)
	s.bot = tgBot

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			s.log.Error("an error occurred while handling update",
				"error", err,
				"user_id", ctx.EffectiveUser.Id,
				"chat_id", ctx.EffectiveChat.Id,
			)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: s.cfg.Telegram.Polling.MaxRoutines,
	})

	s.updater = ext.NewUpdater(dispatcher, nil)
	s.registerHandlers(dispatcher)

	err := s.updater.StartPolling(tgBot, &ext.PollingOpts{
		DropPendingUpdates: s.cfg.Telegram.Polling.DropPendingUpdates,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: s.cfg.Telegram.Polling.Timeout,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: s.cfg.Telegram.Polling.RequestTimeout,
			},
		},
	})
	if err != nil {
		panic("failed to start polling: " + err.Error())
	}
	s.log.Info("start polling")
	s.updater.Idle()
}

// RunWebhook sets up and starts the telegram bot using webhook.
func (s *telegramService) RunWebhook() error {
	tgBot := telegram.MustNew(&s.cfg.Telegram)
	s.bot = tgBot

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			s.log.Error("an error occurred while handling update",
				"error", err,
				"user_id", ctx.EffectiveUser.Id,
				"chat_id", ctx.EffectiveChat.Id,
			)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: s.cfg.Telegram.Polling.MaxRoutines,
	})

	s.updater = ext.NewUpdater(dispatcher, nil)
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

	_, err := tgBot.SetWebhook(webhookURL, &gotgbot.SetWebhookOpts{
		SecretToken: secretToken,
	})
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	s.log.Info("webhook set successfully", "url", webhookURL)

	// Start webhook server
	listenAddr := fmt.Sprintf(":%d", s.cfg.Telegram.Webhook.ListenPort)
	err = s.updater.StartWebhook(tgBot, s.cfg.Telegram.Webhook.ListenPath, ext.WebhookOpts{
		ListenAddr:  listenAddr,
		SecretToken: secretToken,
	})
	if err != nil {
		return err
	}

	s.log.Info("webhook server started", "path", s.cfg.Telegram.Webhook.ListenPath, "addr", listenAddr)
	return nil
}

// GetWebhookHandler returns the HTTP handler for webhook.
// Note: When using StartWebhook, the updater handles HTTP directly via ListenAndServe.
// This method is provided for custom HTTP server integration.
func (s *telegramService) GetWebhookHandler() http.Handler {
	if s.updater == nil {
		s.log.Warn("webhook handler requested before updater initialized")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "webhook not initialized", http.StatusServiceUnavailable)
		})
	}
	// ext.Updater implements http.Handler via its ServeHTTP method for webhook updates
	return &updaterHandler{updater: s.updater, path: s.cfg.Telegram.Webhook.ListenPath}
}

// updaterHandler wraps ext.Updater to only handle requests at the configured path.
type updaterHandler struct {
	updater *ext.Updater
	path    string
}

func (h *updaterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != h.path {
		http.NotFound(w, r)
		return
	}
	// ext.Updater has ServeHTTP method
	if handler, ok := interface{}(h.updater).(http.Handler); ok {
		handler.ServeHTTP(w, r)
	} else {
		http.Error(w, "updater does not implement http.Handler", http.StatusInternalServerError)
	}
}
