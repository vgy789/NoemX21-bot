package telegram

import (
	"context"
	"log/slog"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/clients/telegram"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

//go:generate mockgen -source=$GOFILE -destination=mock/runner_mock.go -package=mock
type TelegramService interface {
	Run()
}

// Sender defines interface for sending messages to Telegram.
type Sender interface {
	SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error)
	EditMessageText(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error)
	AnswerCallbackQuery(callbackQueryId string, opts *gotgbot.AnswerCallbackQueryOpts) (bool, error)
}

// DefaultSender is the default implementation of Sender using gotgbot.Bot.
type DefaultSender struct {
	Bot *gotgbot.Bot
}

func (s *DefaultSender) SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
	return s.Bot.SendMessage(chatID, text, opts)
}

func (s *DefaultSender) EditMessageText(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
	return s.Bot.EditMessageText(text, opts)
}

func (s *DefaultSender) AnswerCallbackQuery(id string, opts *gotgbot.AnswerCallbackQueryOpts) (bool, error) {
	return s.Bot.AnswerCallbackQuery(id, opts)
}

// NewTelegramService creates new telegram service.
func NewTelegramService(cfg *config.TelegramBot, log *slog.Logger, studentSvc service.StudentService) TelegramService {
	// Initialize FSM components
	parser := fsm.NewFlowParser("docs/specs/flows", log) // Assuming CWD is root
	repoFSM := fsm.NewMemoryStateRepository()

	// Create registry and register actions
	registry := fsm.NewLogicRegistry()

	// Register Input actions
	registry.Register("input:set_ru", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("switching language to RU", "user_id", userID)
		return "", map[string]interface{}{"language": fsm.LangRu}, nil
	})
	registry.Register("input:set_en", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("switching language to EN", "user_id", userID)
		return "", map[string]interface{}{"language": fsm.LangEn}, nil
	})

	// Register System actions
	registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		_, err := studentSvc.GetProfileByTelegramID(ctx, userID)
		isRegistered := err == nil
		log.Debug("checking registration status", "user_id", userID, "registered", isRegistered)
		return "", map[string]interface{}{"registered": isRegistered}, nil
	})

	// Initialize Engine with sanitizer
	sanitizer := func(text string) string {
		return strings.ReplaceAll(text, "_", "\\_")
	}

	engine := fsm.NewEngine(parser, repoFSM, log, registry, sanitizer)

	return &telegramService{
		cfg:        cfg,
		log:        log,
		studentSvc: studentSvc,
		engine:     engine,
	}
}

type telegramService struct {
	cfg        *config.TelegramBot
	log        *slog.Logger
	studentSvc service.StudentService
	engine     *fsm.Engine
	sender     Sender // For testing
}

func (s *telegramService) getSender(b *gotgbot.Bot) Sender {
	if s.sender != nil {
		return s.sender
	}
	return &DefaultSender{Bot: b}
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
