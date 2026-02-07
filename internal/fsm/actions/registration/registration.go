package registration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// Register registers registration-related actions.
func Register(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	log *slog.Logger,
	queries db.Querier,
	studentSvc service.StudentService,
	rcClient *rocketchat.Client,
	s21Client *s21.Client,
) {
	// Helper to extract user info from payload/context
	getUserInfo := func(ctx context.Context, userID int64, payload map[string]interface{}) fsm.UserInfo {
		ui := fsm.UserInfo{
			ID:        userID,
			Platform:  "Unknown",
			Username:  "Unknown",
			FirstName: "Unknown",
			LastName:  "Unknown",
		}

		// Try context first (most reliable)
		if ctxUI, ok := ctx.Value(fsm.ContextKeyUserInfo).(*fsm.UserInfo); ok {
			ui = *ctxUI
		}

		// Fallback for empty strings from context
		if ui.Platform == "" || ui.Platform == "Unknown" {
			ui.Platform = "Telegram" // Default platform for this bot
		}
		if ui.Username == "" {
			ui.Username = "none"
		}
		if ui.FirstName == "" {
			ui.FirstName = "User"
		}

		// Override with payload if present (useful for testing or specific overrides)
		if p, ok := payload["platform"].(string); ok && p != "" {
			ui.Platform = p
		}
		if u, ok := payload["username"].(string); ok && u != "" {
			ui.Username = u
		}
		if f, ok := payload["first_name"].(string); ok && f != "" {
			ui.FirstName = f
		}
		if l, ok := payload["last_name"].(string); ok && l != "" {
			ui.LastName = l
		}

		return ui
	}

	// Register System actions for registration flow
	registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		ui := getUserInfo(ctx, userID, payload)

		platform := db.EnumPlatformTelegram
		if strings.ToLower(ui.Platform) == "rocketchat" {
			platform = db.EnumPlatformRocketchat
		}

		_, err := studentSvc.GetProfileByExternalID(ctx, platform, fmt.Sprintf("%d", ui.ID))
		isRegistered := err == nil
		log.Debug("checking registration status", "user_id", ui.ID, "platform", ui.Platform, "registered", isRegistered)
		return "", map[string]interface{}{"registered": isRegistered}, nil
	})

	// Validate School21 user
	registry.Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login := payload["login"].(string)
		log.Debug("validating school21 user", "login", login)

		// 1. Get bot's own token to use for verification
		// In a real app, we should cache this token.
		authResp, err := s21Client.Auth(ctx, cfg.Init.SchoolLogin, cfg.Init.SchoolPassword.Expose())
		if err != nil {
			log.Error("failed to authenticate bot to School21 API", "error", err)
			return "", map[string]interface{}{"api_status": 500}, nil
		}

		// 2. Check the student login via API
		participant, err := s21Client.GetParticipant(ctx, authResp.AccessToken, login)
		if err != nil {
			// Check if it was a 404 (user not found)
			if strings.Contains(err.Error(), "status 404") || strings.Contains(err.Error(), "body error: status 404") {
				return "", map[string]interface{}{"api_status": 404}, nil
			}
			log.Error("S21 API GetParticipant failed", "login", login, "error", err)
			return "", map[string]interface{}{"api_status": 502}, nil
		}

		// 3. Return successful validation data
		return "", map[string]interface{}{
			"api_status": 200,
			"s21_login":  login,
			"user": map[string]interface{}{
				"status":       participant.Status,
				"parallelName": participant.ParallelName,
			},
		}, nil
	})

	// Find and verify RocketChat user and update ID in DB
	registry.Register("find_and_verify_rocket_user", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login := payload["login"].(string)

		// 1. Check if account already registered/linked in our DB
		ua, err := queries.GetUserAccountByStudentId(ctx, login)
		if err == nil {
			if ua.ExternalID != fmt.Sprintf("%d", userID) {
				log.Debug("student already registered to another telegram account", "login", login)
				return "", map[string]interface{}{"email_already_registered": true}, nil
			}
		}

		// 2. Call Rocket.Chat API to find the real user and verify email
		// This ensures we get the *current* ID even if DB is outdated, and validates security
		rcUser, err := rcClient.GetUserInfo(ctx, login)
		if err != nil {
			log.Error("failed to find user in RocketChat API", "login", login, "error", err)
			return "", map[string]interface{}{"rocket_user_not_found": true}, nil
		}

		// 3. Verify email
		expectedEmail := fmt.Sprintf("%s@student.21-school.ru", login)
		emailVerified := false
		for _, email := range rcUser.User.Emails {
			if email.Address == expectedEmail && email.Verified {
				emailVerified = true
				break
			}
		}

		if !emailVerified {
			log.Warn("rocketchat email verification failed", "login", login)
			return "", map[string]interface{}{"email_mismatch": true, "rocket_user_found": true}, nil
		}

		// 4. Update the Rocket.Chat ID in the database
		// If student doesn't exist, create it.
		student, err := queries.GetStudentByS21Login(ctx, login)
		if err != nil {
			log.Info("creating new student record during registration", "login", login)
			_, err = queries.UpsertStudent(ctx, db.UpsertStudentParams{
				S21Login:           login,
				RocketchatID:       rcUser.User.ID,
				Status:             db.NullEnumStudentStatus{EnumStudentStatus: db.EnumStudentStatusACTIVE, Valid: true},
				Timezone:           "UTC",
				CampusID:           pgtype.UUID{Valid: false},
				CoalitionID:        pgtype.Int2{Valid: false},
				AlternativeContact: pgtype.Text{Valid: false},
				HasCoffeeBan:       pgtype.Bool{Valid: false},
				Level:              pgtype.Int4{Valid: false},
				ExpValue:           pgtype.Int4{Valid: false},
				Prp:                pgtype.Int4{Valid: false},
				Crp:                pgtype.Int4{Valid: false},
				Coins:              pgtype.Int4{Valid: false},
				ParallelName:       pgtype.Text{Valid: false},
			})
			if err != nil {
				return "", nil, fmt.Errorf("failed to create student: %w", err)
			}
		} else if student.RocketchatID != rcUser.User.ID {
			log.Info("updating rocketchat id for student", "login", login, "old_id", student.RocketchatID, "new_id", rcUser.User.ID)
			_, err = queries.UpsertStudent(ctx, db.UpsertStudentParams{
				S21Login:           student.S21Login,
				RocketchatID:       rcUser.User.ID, // Update ID
				CampusID:           student.CampusID,
				CoalitionID:        student.CoalitionID,
				Status:             student.Status,
				Timezone:           student.Timezone,
				AlternativeContact: student.AlternativeContact,
				HasCoffeeBan:       student.HasCoffeeBan,
				Level:              student.Level,
				ExpValue:           student.ExpValue,
				Prp:                student.Prp,
				Crp:                student.Crp,
				Coins:              student.Coins,
				ParallelName:       student.ParallelName,
			})
			if err != nil {
				return "", nil, fmt.Errorf("failed to update rocketchat id: %w", err)
			}
		}

		log.Debug("rocket user verified", "login", login, "rc_id", rcUser.User.ID)
		return "", map[string]interface{}{
			"rocket_user_found": true,
			"email_verified":    true,
		}, nil
	})

	// Generate and send OTP
	registry.Register("generate_otp", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login := payload["login"].(string)

		// 0. Check rate limits
		rl := service.GetRateLimiter()
		if err := rl.CheckAndRecord(userID, login); err != nil {
			log.Warn("rate limit exceeded", "user_id", userID, "login", login, "error", err)
			return "", nil, err // FSM will handle this as a generic error or we can signal specific state
		}

		otpSvc := service.NewOTPService(queries, rcClient, cfg, log)

		// Set student ID in context for verification
		ctx = context.WithValue(ctx, fsm.ContextKeyStudentID, login)

		ui := getUserInfo(ctx, userID, payload)
		log.Debug("generating OTP with user info",
			"user_id", ui.ID,
			"username", ui.Username,
			"platform", ui.Platform,
			"first_name", ui.FirstName,
			"last_name", ui.LastName)

		if err := otpSvc.GenerateAndSendOTP(ctx, login, ui); err != nil {
			if strings.HasPrefix(err.Error(), "RATE_LIMIT:") {
				var remaining int
				_, _ = fmt.Sscanf(err.Error(), "RATE_LIMIT:%d", &remaining)
				return "", map[string]interface{}{
					"otp_sent":             false,
					"otp_rate_limited":     true,
					"otp_retry_after_secs": remaining,
				}, nil
			}
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
			// Treat unexpected errors as invalid code to allow FSM to transition back
			valid = false
		}

		ua, err := queries.GetUserAccountByStudentId(ctx, studentID)
		accountExists := err == nil

		isOwnAccount := false
		if accountExists {
			isOwnAccount = ua.ExternalID == fmt.Sprintf("%d", userID)
		}

		emailUnique := !accountExists || isOwnAccount

		if valid && emailUnique && !isOwnAccount {
			ui := getUserInfo(ctx, userID, payload)

			username := pgtype.Text{Valid: false}
			if ui.Username != "" {
				username = pgtype.Text{String: ui.Username, Valid: true}
			}

			// Map string platform to enum
			platform := db.EnumPlatformTelegram
			switch strings.ToLower(ui.Platform) {
			case "telegram":
				platform = db.EnumPlatformTelegram
			case "rocketchat":
				platform = db.EnumPlatformRocketchat
			}

			uaCreated, err := queries.CreateUserAccount(ctx, db.CreateUserAccountParams{
				StudentID:    studentID,
				Platform:     platform,
				ExternalID:   fmt.Sprintf("%d", ui.ID),
				Username:     username,
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

		updates := map[string]interface{}{
			"code_correct": valid,
			"email_unique": emailUnique,
			"is_own_email": isOwnAccount,
		}

		if !valid {
			updates["last_input_invalid"] = true
			updates["otp_correct"] = false
		} else {
			updates["otp_correct"] = true
		}

		return "", updates, nil
	})

	// Load user profile after successful registration
	registry.Register("load_user_profile", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		ui := getUserInfo(ctx, userID, payload)

		platform := db.EnumPlatformTelegram
		if strings.ToLower(ui.Platform) == "rocketchat" {
			platform = db.EnumPlatformRocketchat
		}

		profile, err := studentSvc.GetProfileByExternalID(ctx, platform, fmt.Sprintf("%d", ui.ID))
		if err != nil {
			log.Error("failed to load user profile", "user_id", ui.ID, "platform", ui.Platform, "error", err)
			return "", map[string]interface{}{"profile_loaded": false}, nil
		}

		log.Info("user profile loaded", "user_id", userID, "login", profile.Login)

		return "", map[string]interface{}{
			"profile_loaded": true,
			"name":           profile.Login,
			"s21_login":      profile.Login,
			"campus":         profile.CampusName,
			"coalition":      profile.CoalitionName,
			"level":          profile.Level,
			"exp":            profile.Exp,
			"prp":            profile.PRP,
			"crp":            profile.CRP,
			"coins":          profile.Coins,
		}, nil
	})
}
