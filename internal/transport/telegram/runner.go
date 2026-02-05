package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/jackc/pgx/v5/pgtype"
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
func NewTelegramService(cfg *config.Config, log *slog.Logger, studentSvc service.StudentService, queries db.Querier, rcClient *rocketchat.Client) TelegramService {
	// Initialize FSM components
	parser := fsm.NewFlowParser("docs/specs/flows", log) // Assuming CWD is root
	repoFSM := fsm.NewMemoryStateRepository()

	// Helper to update language in DB if user account exists
	updateLanguage := func(ctx context.Context, userID int64, langCode string) {
		// Try to find user account
		ua, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			// Account doesn't exist yet (likely during registration), just return
			return
		}

		// Account exists, upsert settings
		// We try to fetch existing settings to preserve other fields, or use defaults
		settings, err := queries.GetUserBotSettings(ctx, ua.ID)

		// Defaults
		notifications := pgtype.Bool{Bool: true, Valid: true}
		reviews := []byte("[]")

		if err == nil {
			// Preserve existing values
			notifications = settings.NotificationsEnabled
			reviews = settings.ReviewPostCampusIds
		}

		_, err = queries.UpsertUserBotSettings(ctx, db.UpsertUserBotSettingsParams{
			UserAccountID:        ua.ID,
			LanguageCode:         pgtype.Text{String: langCode, Valid: true},
			NotificationsEnabled: notifications,
			ReviewPostCampusIds:  reviews,
		})
		if err != nil {
			log.Error("failed to update user language", "error", err, "user_id", userID, "lang", langCode)
		} else {
			log.Info("updated user language", "user_id", userID, "lang", langCode)
		}
	}

	// Create registry and register actions
	registry := fsm.NewLogicRegistry()

	// Register Input actions
	registry.Register("input:set_ru", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("switching language to RU", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangRu)
		return "", map[string]interface{}{"language": fsm.LangRu}, nil
	})
	registry.Register("input:set_en", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("switching language to EN", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangEn)
		return "", map[string]interface{}{"language": fsm.LangEn}, nil
	})

	// Register Settings menu language actions (same logic)
	registry.Register("input:ru", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("settings: switching language to RU", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangRu)
		return "", map[string]interface{}{"language": fsm.LangRu}, nil
	})
	registry.Register("input:en", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		log.Info("settings: switching language to EN", "user_id", userID)
		updateLanguage(ctx, userID, fsm.LangEn)
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
		student, err := queries.GetStudentProfile(ctx, login)
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
		otpSvc := service.NewOTPService(queries, rcClient, cfg, log)

		// Set student ID in context for verification
		ctx = context.WithValue(ctx, fsm.ContextKeyStudentID, login)

		if err := otpSvc.GenerateAndSendOTP(ctx, userID, login); err != nil {
			log.Error("failed to generate OTP", "error", err)
			return "", nil, err
		}

		return "", map[string]interface{}{
			"otp_sent":  true,
			"s21_login": login,
		}, nil
	})

	// Verify OTP code
	registry.Register("verify_otp", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		code := payload["code"].(string)
		otpSvc := service.NewOTPService(queries, rcClient, cfg, log)

		// Get student ID from payload
		studentID, ok := payload["student_id"].(string)
		if !ok {
			// Fallback: try to find it in payload as login (from last step)
			if login, ok := payload["login"].(string); ok {
				studentID = login
			} else {
				return "", nil, fmt.Errorf("student ID not found in payload")
			}
		}

		// Set student ID in context for verification (if needed by underlying storage)
		ctx = context.WithValue(ctx, fsm.ContextKeyStudentID, studentID)

		valid, err := otpSvc.VerifyOTP(ctx, userID, code)
		if err != nil {
			log.Error("OTP verification failed", "error", err)
			return "", nil, err
		}

		// Check if student login is already taken by another account
		ua, err := queries.GetUserAccountByStudentId(ctx, studentID)
		accountExists := err == nil

		isOwnAccount := false
		if accountExists {
			isOwnAccount = ua.ExternalID == fmt.Sprintf("%d", userID)
		}

		// email_unique is true if either no account exists or it's current user's own account
		emailUnique := !accountExists || isOwnAccount

		// If OTP is correct and email is unique and NO account exists for THIS user yet, create it
		if valid && emailUnique && !isOwnAccount {
			// Create User Account
			uaCreated, err := queries.CreateUserAccount(ctx, db.CreateUserAccountParams{
				StudentID:    studentID,
				Platform:     db.EnumPlatformTelegram,
				ExternalID:   fmt.Sprintf("%d", userID),
				Username:     pgtype.Text{Valid: false},
				IsSearchable: pgtype.Bool{Bool: true, Valid: true},
				Role:         db.NullEnumUserRole{EnumUserRole: db.EnumUserRoleUser, Valid: true},
			})
			if err != nil {
				log.Error("failed to create user account", "error", err, "student_id", studentID)
			} else {
				log.Info("created user account", "user_account_id", uaCreated.ID, "student_id", studentID)

				// Persist Language Preference
				langCode := fsm.LangRu // Default
				if val, ok := payload["language"].(string); ok {
					langCode = val
				} else if val, ok := ctx.Value("language").(string); ok {
					langCode = val
				}

				_, err = queries.UpsertUserBotSettings(ctx, db.UpsertUserBotSettingsParams{
					UserAccountID:        uaCreated.ID,
					LanguageCode:         pgtype.Text{String: langCode, Valid: true},
					NotificationsEnabled: pgtype.Bool{Bool: true, Valid: true},
					ReviewPostCampusIds:  []byte("[]"),
				})
				if err != nil {
					log.Error("failed to save initial user settings", "error", err)
				}
			}
		}

		return "", map[string]interface{}{
			"code_correct": valid,
			"email_unique": emailUnique,
			"is_own_email": isOwnAccount,
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
		db:         queries,
	}
}

type telegramService struct {
	cfg        *config.Config
	log        *slog.Logger
	studentSvc service.StudentService
	engine     *fsm.Engine
	sender     Sender // For testing
	db         db.Querier
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
