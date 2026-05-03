package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/jackc/pgx/v5/pgxpool"
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
	GetWebhookHandler() http.Handler
}

// Sender defines interface for sending messages to Telegram.
type Sender interface {
	SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error)
	SendPhoto(chatID int64, photo gotgbot.InputFileOrString, opts *gotgbot.SendPhotoOpts) (*gotgbot.Message, error)
	SendMediaGroup(chatID int64, media []gotgbot.InputMedia, opts *gotgbot.SendMediaGroupOpts) ([]gotgbot.Message, error)
	EditMessageText(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error)
	EditMessageReplyMarkup(opts *gotgbot.EditMessageReplyMarkupOpts) (*gotgbot.Message, bool, error)
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

func (s *DefaultSender) SendMediaGroup(chatID int64, media []gotgbot.InputMedia, opts *gotgbot.SendMediaGroupOpts) ([]gotgbot.Message, error) {
	return s.Bot.SendMediaGroup(chatID, media, opts)
}

func (s *DefaultSender) EditMessageText(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
	return s.Bot.EditMessageText(text, opts)
}

func (s *DefaultSender) EditMessageReplyMarkup(opts *gotgbot.EditMessageReplyMarkupOpts) (*gotgbot.Message, bool, error) {
	return s.Bot.EditMessageReplyMarkup(opts)
}

func (s *DefaultSender) DeleteMessage(chatID int64, messageID int64) (bool, error) {
	return s.Bot.DeleteMessage(chatID, messageID, nil)
}

func (s *DefaultSender) AnswerCallbackQuery(id string, opts *gotgbot.AnswerCallbackQueryOpts) (bool, error) {
	return s.Bot.AnswerCallbackQuery(id, opts)
}

// NewTelegramService creates new telegram service.
func NewTelegramService(cfg *config.Config, log *slog.Logger, userSvc service.UserService, queries db.Querier, pool *pgxpool.Pool, engine *fsm.Engine, cache *imgcache.Store) *telegramService {
	return &telegramService{
		cfg:         cfg,
		log:         log,
		userSvc:     userSvc,
		queries:     queries,
		engine:      engine,
		imgCache:    cache,
		pollingLock: newPollingLocker(pool, log),
		fileIDs:     make(map[string]string),
	}
}

type telegramService struct {
	cfg         *config.Config
	log         *slog.Logger
	userSvc     service.UserService
	queries     db.Querier
	engine      *fsm.Engine
	imgCache    *imgcache.Store
	sender      Sender // For testing
	bot         *gotgbot.Bot
	updater     *ext.Updater
	updaterMu   sync.RWMutex
	pollingLock pollingLocker
	fileIDs     map[string]string
	fileIDsMu   sync.RWMutex
}

type activeInitializedTelegramGroupsLister interface {
	ListActiveInitializedTelegramGroups(ctx context.Context) ([]db.TelegramGroup, error)
}

func telegramAllowedUpdates() []string {
	return []string{
		"message",
		"callback_query",
		"chat_member",
		"my_chat_member",
		"chat_join_request",
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

func (s *telegramService) getUpdater() *ext.Updater {
	s.updaterMu.RLock()
	defer s.updaterMu.RUnlock()
	return s.updater
}

// Run starts the telegram bot using long polling.
func (s *telegramService) Run(ctx context.Context) error {
	locker := s.pollingLock
	if locker == nil {
		locker = noopPollingLocker{}
	}

	lease, err := locker.Acquire(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer func() {
		if err := lease.Release(); err != nil {
			if s.log != nil {
				s.log.Error("failed to release telegram polling lock", "error", err)
			}
		}
	}()

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

	dropPendingUpdates := s.cfg.Telegram.Polling.DropPendingUpdates
	if dropPendingUpdates {
		s.log.Warn("DROP_PENDING_UPDATES=true conflicts with moderation consistency; forcing false")
		dropPendingUpdates = false
	}

	err = updater.StartPolling(tgBot, &ext.PollingOpts{
		DropPendingUpdates: dropPendingUpdates,
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
	s.startConsistencySweep(ctx, tgBot)
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

	s.startConsistencySweep(ctx, tgBot)
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

// GetWebhookHandler returns the HTTP handler for webhook.
// Note: When using StartWebhook, the updater handles HTTP directly via ListenAndServe.
// This method is provided for custom HTTP server integration.
func (s *telegramService) GetWebhookHandler() http.Handler {
	return &updaterHandler{service: s, path: s.cfg.Telegram.Webhook.ListenPath}
}

// updaterHandler wraps ext.Updater to only handle requests at the configured path.
type updaterHandler struct {
	service *telegramService
	path    string
}

func (h *updaterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != h.path {
		http.NotFound(w, r)
		return
	}
	updater := h.service.getUpdater()
	if updater == nil {
		h.service.log.Warn("webhook request received before updater initialized")
		http.Error(w, "webhook not initialized", http.StatusServiceUnavailable)
		return
	}
	if handler, ok := any(updater).(http.Handler); ok {
		handler.ServeHTTP(w, r)
	} else {
		http.Error(w, "updater does not implement http.Handler", http.StatusInternalServerError)
	}
}

func (s *telegramService) startConsistencySweep(parentCtx context.Context, b *gotgbot.Bot) {
	if s == nil || s.queries == nil || b == nil {
		return
	}
	lister, ok := s.queries.(activeInitializedTelegramGroupsLister)
	if !ok {
		return
	}

	go func() {
		sweepCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		groups, err := lister.ListActiveInitializedTelegramGroups(sweepCtx)
		if err != nil {
			s.log.Warn("telegram consistency sweep: failed to list groups", "error", err)
			return
		}
		if len(groups) == 0 {
			return
		}

		s.log.Info("telegram consistency sweep started", "groups", len(groups))
		processedGroups := 0
		for _, group := range groups {
			select {
			case <-parentCtx.Done():
				return
			default:
			}

			if (!group.DefenderEnabled || !group.DefenderRecheckKnownMembers) && !group.MemberTagsEnabled {
				continue
			}
			if err := s.reconcileKnownGroupMembers(sweepCtx, b, group); err != nil {
				s.log.Warn("telegram consistency sweep: group reconcile failed", "chat_id", group.ChatID, "error", err)
				continue
			}
			processedGroups++
		}
		s.log.Info("telegram consistency sweep finished", "groups", len(groups), "processed", processedGroups)
	}()
}

func (s *telegramService) reconcileKnownGroupMembers(ctx context.Context, b *gotgbot.Bot, group db.TelegramGroup) error {
	if s == nil || s.queries == nil || b == nil {
		return nil
	}
	if !group.IsActive || !group.IsInitialized {
		return nil
	}

	knownMembers, err := s.queries.ListTelegramGroupKnownMembers(ctx, group.ChatID)
	if err != nil {
		return err
	}
	if len(knownMembers) == 0 {
		return nil
	}

	for _, known := range knownMembers {
		if known.TelegramUserID == 0 || known.IsBot {
			continue
		}

		if group.DefenderEnabled && group.DefenderRecheckKnownMembers {
			s.tryAutoDefenderForKnownGroup(ctx, b, group, known.TelegramUserID)
		}
		if group.MemberTagsEnabled {
			s.tryAutoAssignMemberTagForKnownGroup(ctx, b, group, known.TelegramUserID)
		}
	}
	return nil
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
