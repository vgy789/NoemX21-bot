package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/telegram"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

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
func NewTelegramService(cfg *config.Config, log *slog.Logger, studentSvc service.StudentService, db *db.Queries, rcClient *rocketchat.Client) TelegramService {
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

	// Register System actions for registration flow
	registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		_, err := studentSvc.GetProfileByTelegramID(ctx, userID)
		isRegistered := err == nil
		log.Debug("checking registration status", "user_id", userID, "registered", isRegistered)
		return "", map[string]interface{}{"registered": isRegistered}, nil
	})

	// Validate School21 user
	registry.Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login := payload["login"].(string)
		// This action will be implemented via S21 client
		// For now, return success to allow flow to continue
		log.Debug("validating school21 user", "login", login)
		return "", map[string]interface{}{
			"api_status": 200,
			"user": map[string]interface{}{
				"status":       "ACTIVE",
				"parallelName": "Core program",
			},
		}, nil
	})

	// Find and verify RocketChat user
	registry.Register("find_and_verify_rocket_user", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login := payload["login"].(string)
		// Get student info from database
		student, err := db.GetStudentProfile(ctx, login)
		if err != nil {
			log.Debug("student not found in database", "login", login)
			return "", map[string]interface{}{
				"rocket_user_found": false,
				"rocket_api_error":  true,
			}, nil
		}

		// Check if student has RocketChat ID
		if student.RocketchatID == "" {
			return "", map[string]interface{}{
				"rocket_user_not_found": true,
			}, nil
		}

		// For now, assume verification passes
		// In production, verify email matches
		log.Debug("rocket user found", "login", login, "rc_id", student.RocketchatID)
		return "", map[string]interface{}{
			"rocket_user_found": true,
			"email_verified":    true,
		}, nil
	})

	// Generate and send OTP
	registry.Register("generate_otp", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login := payload["login"].(string)
		otpSvc := service.NewOTPService(db, rcClient, cfg, log)

		// Set student ID in context for verification
		ctx = context.WithValue(ctx, fsm.ContextKeyStudentID, login)

		if err := otpSvc.GenerateAndSendOTP(ctx, userID, login); err != nil {
			log.Error("failed to generate OTP", "error", err)
			return "", nil, err
		}

		return "", map[string]interface{}{
			"otp_sent": true,
		}, nil
	})

	// Verify OTP code
	registry.Register("verify_otp", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		code := payload["code"].(string)
		otpSvc := service.NewOTPService(db, rcClient, cfg, log)

		// Get student ID from context
		studentID, ok := ctx.Value(fsm.ContextKeyStudentID).(string)
		if !ok {
			return "", nil, fmt.Errorf("student ID not found in context")
		}

		// Set student ID in context for verification
		ctx = context.WithValue(ctx, fsm.ContextKeyStudentID, studentID)

		valid, err := otpSvc.VerifyOTP(ctx, userID, code)
		if err != nil {
			log.Error("OTP verification failed", "error", err)
			return "", nil, err
		}

		// Check if student already registered
		_, err = studentSvc.GetProfileByTelegramID(ctx, userID)
		isRegistered := err == nil

		return "", map[string]interface{}{
			"code_correct":     valid,
			"email_unique":     !isRegistered,
			"email_registered": isRegistered,
		}, nil
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
	cfg        *config.Config
	log        *slog.Logger
	studentSvc service.StudentService
	engine     *fsm.Engine
	sender     Sender // For testing
	db         *db.Queries
	rcClient   *rocketchat.Client
}

func (s *telegramService) getSender(b *gotgbot.Bot) Sender {
	if s.sender != nil {
		return s.sender
	}
	return &DefaultSender{Bot: b}
}

// Run starts the telegram bot.
func (s *telegramService) Run() {
	tgBot := telegram.MustNew(&s.cfg.Telegram)

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

	updater := ext.NewUpdater(dispatcher, nil)
	s.registerHandlers(dispatcher)

	err := updater.StartPolling(tgBot, &ext.PollingOpts{
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
	updater.Idle()
}
