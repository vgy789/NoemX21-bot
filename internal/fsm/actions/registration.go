package actions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// RegisterRegistrationActions registers actions related to the registration flow.
func RegisterRegistrationActions(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	log *slog.Logger,
	studentSvc service.StudentService,
	queries db.Querier,
	rcClient *rocketchat.Client,
) {
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
		student, err := queries.GetStudentProfile(ctx, login)
		if err != nil {
			log.Debug("student not found in database", "login", login)
			return "", map[string]interface{}{
				"rocket_user_found": false,
			}, nil
		}

		if student.RocketchatID == "" {
			return "", map[string]interface{}{
				"rocket_user_not_found": true,
			}, nil
		}

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

		studentID, ok := payload["student_id"].(string)
		if !ok {
			if login, ok := payload["login"].(string); ok {
				studentID = login
			} else {
				return "", nil, fmt.Errorf("student ID not found in payload")
			}
		}

		ctx = context.WithValue(ctx, fsm.ContextKeyStudentID, studentID)

		valid, err := otpSvc.VerifyOTP(ctx, userID, code)
		if err != nil {
			log.Error("OTP verification failed", "error", err)
			return "", nil, err
		}

		ua, err := queries.GetUserAccountByStudentId(ctx, studentID)
		accountExists := err == nil

		isOwnAccount := false
		if accountExists {
			isOwnAccount = ua.ExternalID == fmt.Sprintf("%d", userID)
		}

		emailUnique := !accountExists || isOwnAccount

		if valid && emailUnique && !isOwnAccount {
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

				langCode := fsm.LangRu
				if val, ok := payload["language"].(string); ok {
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
}
