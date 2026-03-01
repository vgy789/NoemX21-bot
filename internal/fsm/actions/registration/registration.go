package registration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"hash/fnv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	stats "github.com/vgy789/noemx21-bot/internal/fsm/actions/statistics"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// Register registers registration-related actions.
func Register(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	log *slog.Logger,
	queries db.Querier,
	userSvc service.UserService,
	rcClient *rocketchat.Client,
	s21Client *s21.Client,
	credService *service.CredentialService,
	otpProvider service.OTPProvider,
	aliasRegistrar func(alias, target string),
) {
	if aliasRegistrar != nil {
		aliasRegistrar("START", "registration.yaml/START")
	}
	// Helper to extract user info from payload/context
	getUserInfo := func(ctx context.Context, userID int64, payload map[string]any) fsm.UserInfo {
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
	registry.Register("is_user_registered", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ui := getUserInfo(ctx, userID, payload)

		platform := db.EnumPlatformTelegram
		if strings.ToLower(ui.Platform) == "rocketchat" {
			platform = db.EnumPlatformRocketchat
		}

		_, err := userSvc.GetProfileByExternalID(ctx, platform, fmt.Sprintf("%d", ui.ID))
		isRegistered := err == nil
		log.Debug("checking registration status", "user_id", ui.ID, "platform", ui.Platform, "registered", isRegistered)
		return "", map[string]any{"registered": isRegistered}, nil
	})

	// Validate School21 user
	registry.Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		login := payload["login"].(string)
		log.Debug("validating school21 user", "login", login)

		// 1. Get bot's own token to use for verification
		// In a real app, we should cache this token.
		authResp, err := s21Client.Auth(ctx, cfg.Init.SchoolLogin, cfg.Init.SchoolPassword.Expose())
		if err != nil {
			log.Error("failed to authenticate bot to School21 API", "error", err)
			return "", map[string]any{"api_status": 500}, nil
		}

		// 2. Check the student login via API
		participant, err := s21Client.GetParticipant(ctx, authResp.AccessToken, login)
		if err != nil {
			// Check if it was a 404 (user not found)
			if strings.Contains(err.Error(), "status 404") || strings.Contains(err.Error(), "body error: status 404") {
				return "", map[string]any{"api_status": 404}, nil
			}
			log.Error("S21 API GetParticipant failed", "login", login, "error", err)
			return "", map[string]any{"api_status": 502}, nil
		}

		// 3. Return successful validation data
		parallelName := ""
		if participant.ParallelName != nil {
			parallelName = *participant.ParallelName
		}
		return "", map[string]any{
			"api_status": 200,
			"s21_login":  login,
			"s21_user": map[string]any{
				"status":       participant.Status,
				"parallelName": parallelName,
			},
		}, nil
	})

	// Find and verify RocketChat user and update ID in DB
	registry.Register("find_and_verify_rocket_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		login := payload["login"].(string)

		// 1. Check if account already registered/linked in our DB
		ua, err := queries.GetUserAccountByS21Login(ctx, login)
		if err == nil {
			if ua.ExternalID != fmt.Sprintf("%d", userID) {
				log.Debug("student already registered to another telegram account", "login", login)
				return "", map[string]any{"email_already_registered": true}, nil
			}
		}

		// 2. Call Rocket.Chat API to find the real user and verify email
		// This ensures we get the *current* ID even if DB is outdated, and validates security
		rcUser, err := rcClient.GetUserInfo(ctx, login)
		if err != nil {
			log.Error("failed to find user in RocketChat API", "login", login, "error", err)
			return "", map[string]any{"rocket_user_not_found": true}, nil
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
			return "", map[string]any{"email_mismatch": true, "rocket_user_found": true}, nil
		}

		// 4. Update the Rocket.Chat ID in the database
		// If registered user doesn't exist, create it.
		regUser, err := queries.GetRegisteredUserByS21Login(ctx, login)
		if err != nil {
			log.Info("creating new registered user during registration", "login", login)
			_, err = queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
				S21Login:           login,
				RocketchatID:       rcUser.User.ID,
				Timezone:           "UTC",
				AlternativeContact: pgtype.Text{Valid: false},
				HasCoffeeBan:       pgtype.Bool{Valid: false},
			})
			if err != nil {
				return "", nil, fmt.Errorf("failed to create registered user: %w", err)
			}
		} else if regUser.RocketchatID != rcUser.User.ID {
			log.Info("updating rocketchat id for registered user", "login", login, "old_id", regUser.RocketchatID, "new_id", rcUser.User.ID)
			_, err = queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
				S21Login:           regUser.S21Login,
				RocketchatID:       rcUser.User.ID,
				Timezone:           regUser.Timezone,
				AlternativeContact: regUser.AlternativeContact,
				HasCoffeeBan:       regUser.HasCoffeeBan,
			})
			if err != nil {
				return "", nil, fmt.Errorf("failed to update rocketchat id: %w", err)
			}
		}

		log.Debug("rocket user verified", "login", login, "rc_id", rcUser.User.ID)
		return "", map[string]any{
			"rocket_user_found": true,
			"email_verified":    true,
		}, nil
	})

	// Generate and send OTP
	registry.Register("generate_otp", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		login := payload["login"].(string)

		// 0. Check rate limits
		rl := service.GetRateLimiter()
		if err := rl.CheckAndRecord(userID, login); err != nil {
			log.Warn("rate limit exceeded", "user_id", userID, "login", login, "error", err)
			return "", nil, err // FSM will handle this as a generic error or we can signal specific state
		}

		// Set S21 login in context for verification
		ctx = context.WithValue(ctx, fsm.ContextKeyS21Login, login)

		ui := getUserInfo(ctx, userID, payload)
		log.Debug("generating OTP with user info",
			"user_id", ui.ID,
			"username", ui.Username,
			"platform", ui.Platform,
			"first_name", ui.FirstName,
			"last_name", ui.LastName)

		if err := otpProvider.GenerateAndSendOTP(ctx, login, ui); err != nil {
			if strings.HasPrefix(err.Error(), "RATE_LIMIT:") {
				var remaining int
				_, _ = fmt.Sscanf(err.Error(), "RATE_LIMIT:%d", &remaining)
				return "", map[string]any{
					"otp_sent":             false,
					"otp_rate_limited":     true,
					"otp_retry_after_secs": remaining,
				}, nil
			}
			log.Error("failed to generate OTP", "error", err)
			return "", nil, err
		}

		return "", map[string]any{
			"otp_sent":  true,
			"s21_login": login,
		}, nil
	})

	// Verify OTP code
	registry.Register("verify_otp", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		code := payload["code"].(string)

		s21Login, ok := payload["student_id"].(string)
		if !ok {
			if login, ok := payload["login"].(string); ok {
				s21Login = login
			} else {
				return "", nil, fmt.Errorf("student login not found in payload")
			}
		}

		ctx = context.WithValue(ctx, fsm.ContextKeyS21Login, s21Login)

		valid, err := otpProvider.VerifyOTP(ctx, userID, code)
		if err != nil {
			log.Error("OTP verification failed", "error", err)
			// Treat unexpected errors as invalid code to allow FSM to transition back
			valid = false
		}

		if valid {
			// Successful verification resets rate limiting to avoid stale bans
			service.GetRateLimiter().Reset(userID)
		}

		ua, err := queries.GetUserAccountByS21Login(ctx, s21Login)
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
				S21Login:     s21Login,
				Platform:     platform,
				ExternalID:   fmt.Sprintf("%d", ui.ID),
				Username:     username,
				IsSearchable: pgtype.Bool{Bool: true, Valid: true},
				Role:         db.NullEnumUserRole{EnumUserRole: db.EnumUserRoleUser, Valid: true},
			})
			if err != nil {
				log.Error("failed to create user account", "error", err, "s21_login", s21Login)
			} else {
				log.Info("created user account", "user_account_id", uaCreated.ID, "s21_login", s21Login)

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

		updates := map[string]any{
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
	registry.Register("load_user_profile", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		ui := getUserInfo(ctx, userID, payload)

		platform := db.EnumPlatformTelegram
		if strings.ToLower(ui.Platform) == "rocketchat" {
			platform = db.EnumPlatformRocketchat
		}

		// Get user account to fetch their login
		ua, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   platform,
			ExternalID: fmt.Sprintf("%d", ui.ID),
		})
		if err != nil {
			log.Error("failed to get user account", "user_id", ui.ID, "error", err)
			return "", map[string]any{"profile_loaded": false}, nil
		}

		studentLogin := ua.S21Login

		// Fetch fresh data from School21 API and save to database
		var token string
		if credService != nil {
			var err error
			token, err = credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
			if err != nil {
				log.Warn("failed to get API token for profile sync", "error", err)
				// Continue anyway - will use cached data
			}
		}
		if token != "" {
			participant, err := s21Client.GetParticipant(ctx, token, studentLogin)
			if err != nil {
				log.Warn("failed to sync profile data from API", "login", studentLogin, "error", err)
				// Continue anyway - will use cached data
			} else {
				campusTimezone := ""
				// Save stats to participant_stats_cache
				cacheParams := db.UpsertParticipantStatsCacheParams{
					S21Login:     studentLogin,
					Level:        participant.Level,
					ExpValue:     int32(participant.ExpValue),
					Status:       db.EnumStudentStatus(participant.Status),
					ParallelName: pgtype.Text{Valid: false},
					ClassName:    pgtype.Text{Valid: false},
				}

				// Add campus info
				if participant.Campus.ID != "" {
					var campusUUID pgtype.UUID
					if err := campusUUID.Scan(participant.Campus.ID); err != nil {
						log.Warn("failed to parse campus UUID in registration", "campus_id", participant.Campus.ID, "error", err)
						// Don't set CampusID if UUID parsing failed
					} else {
						cacheParams.CampusID = campusUUID

						// Ensure campus exists in DB: fetch & upsert if missing
						if err := service.EnsureCampusPresent(ctx, queries, s21Client, token, log, participant.Campus.ID); err != nil {
							log.Error("failed to ensure campus present in registration", "login", studentLogin, "campus_id", participant.Campus.ID, "error", err)
						} else if campus, cErr := queries.GetCampusByID(ctx, campusUUID); cErr == nil {
							if campus.Timezone.Valid {
								campusTimezone = strings.TrimSpace(campus.Timezone.String)
							}
						}
					}
				}

				// Add parallel and class names
				if participant.ParallelName != nil {
					cacheParams.ParallelName = pgtype.Text{String: *participant.ParallelName, Valid: true}
				}
				if participant.ClassName != nil {
					cacheParams.ClassName = pgtype.Text{String: *participant.ClassName, Valid: true}
				}

				// Fetch and add stats (points, coalition, feedback)
				points, _ := s21Client.GetParticipantPoints(ctx, token, studentLogin)
				if points != nil {
					cacheParams.Prp = points.PeerReviewPoints
					cacheParams.Crp = points.CodeReviewPoints
					cacheParams.Coins = points.Coins
				}

				coalition, _ := s21Client.GetParticipantCoalition(ctx, token, studentLogin)
				if coalition != nil && coalition.CoalitionID != 0 {
					cacheParams.CoalitionID = pgtype.Int2{Int16: int16(coalition.CoalitionID), Valid: true}
					if err := service.EnsureCoalitionPresent(ctx, queries, coalition, log); err != nil {
						log.Error("failed to ensure coalition present in registration", "login", studentLogin, "coalition_id", coalition.CoalitionID, "error", err)
					}
				}

				feedback, _ := s21Client.GetParticipantFeedback(ctx, token, studentLogin)
				if feedback != nil {
					cacheParams.Integrity = pgtype.Float4{Float32: float32(feedback.Integrity), Valid: true}
					cacheParams.Friendliness = pgtype.Float4{Float32: float32(feedback.Friendliness), Valid: true}
					cacheParams.Punctuality = pgtype.Float4{Float32: float32(feedback.Punctuality), Valid: true}
					cacheParams.Thoroughness = pgtype.Float4{Float32: float32(feedback.Thoroughness), Valid: true}
				}

				// Save to database (логируем ошибку вместо её подавления)
				if err := queries.UpsertParticipantStatsCache(ctx, cacheParams); err != nil {
					log.Error("failed to upsert participant stats cache in registration", "login", studentLogin, "error", err)
				}

				// Default registered_users.timezone to campus timezone, but never override user custom choice.
				if campusTimezone != "" {
					if regUser, rErr := queries.GetRegisteredUserByS21Login(ctx, studentLogin); rErr == nil {
						currentTZ := strings.TrimSpace(regUser.Timezone)
						if currentTZ == "" || strings.EqualFold(currentTZ, "UTC") {
							_, uErr := queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
								S21Login:           regUser.S21Login,
								RocketchatID:       regUser.RocketchatID,
								Timezone:           campusTimezone,
								AlternativeContact: regUser.AlternativeContact,
								HasCoffeeBan:       regUser.HasCoffeeBan,
							})
							if uErr != nil {
								log.Warn("failed to set default timezone from campus", "login", studentLogin, "timezone", campusTimezone, "error", uErr)
							}
						}
					}
				}

				// Also fetch participant skills and upsert them, then generate radar chart
				skillsResp, err := s21Client.GetParticipantSkills(ctx, token, studentLogin)
				if err == nil && skillsResp != nil {
					skillMap := make(map[string]int32)
					for _, s := range skillsResp.Skills {
						skillMap[s.Name] = s.Points
						// compute hash same way as statistics.hashSkillName
						h := fnv.New32a()
						h.Write([]byte(s.Name))
						hash := int32(h.Sum32())
						_, _ = queries.UpsertSkill(ctx, db.UpsertSkillParams{ID: hash, Name: s.Name, Category: pgtype.Text{String: "General", Valid: true}})
						_ = queries.UpsertParticipantSkill(ctx, db.UpsertParticipantSkillParams{S21Login: studentLogin, SkillID: hash, Value: s.Points})
					}
					// Generate radar chart image and store path in cache/context via stats package
					if chartPath, err := stats.GenerateRadarChartFromData(map[string]map[string]int32{studentLogin: skillMap}, []string{studentLogin}); err == nil {
						// store radar chart path in participant_stats_cache? For now just log and ignore on failure
						log.Info("generated radar chart for new user", "login", studentLogin, "path", chartPath)
					}
				}

				log.Info("synced profile data from API", "login", studentLogin)
			}
		}

		// Load complete profile from database
		profile, err := userSvc.GetProfileByExternalID(ctx, platform, fmt.Sprintf("%d", ui.ID))
		if err != nil {
			log.Error("failed to load user profile", "user_id", ui.ID, "platform", ui.Platform, "error", err)
			return "", map[string]any{"profile_loaded": false}, nil
		}

		log.Info("user profile loaded", "user_id", userID, "login", profile.Login)

		return "", map[string]any{
			"profile_loaded": true,
			"name":           profile.Login,
			"s21_login":      profile.Login,
			"campus":         profile.CampusName,
			"campus_id":      profile.CampusID,
			"coalition":      profile.CoalitionName,
			"level":          profile.Level,
			"exp":            profile.Exp,
			"prp":            profile.PRP,
			"crp":            profile.CRP,
			"coins":          profile.Coins,
		}, nil
	})
}
