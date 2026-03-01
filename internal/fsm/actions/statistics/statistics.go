package statistics

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"

	// "os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const (
	// Cooldown and timeout durations
	statsUpdateCooldownDuration    = 5 * time.Minute  // Дефолтный кулдаун для автоматических обновлений
	statsUpdateManualCooldown      = 10 * time.Second // Кулдаун для ручного обновления (кнопка Refresh)
	statsUpdateSafetyTimeout       = 30 * time.Second // Таймаут для проверки зависшего обновления
	peerDataCacheFreshnessDuration = 1 * time.Minute  // Время, в течение которого данные пира считаются свежими

	// Default skill category
	defaultSkillCategory = "General"

	// Alert messages
	alertDone            = "✔️ Готово"
	alertUpdating        = "🔄 Обновляю..."
	alertAlreadyUpdating = "⏳ Обновление уже идёт..."
	alertErrorToken      = "❌ Ошибка получения токена"
	alertErrorData       = "❌ Ошибка получения данных"

	// Default values for display
	defaultEmptyValue        = "—"
	defaultSocialMetricValue = "0.00"
	defaultCoalitionValue    = "Нет коалиции"
	defaultCampusValue       = "Неизвестный кампус"

	// Number formatting
	socialMetricFormat = "%.2f" // Формат для отображения социальных метрик (2 знака после запятой)
)

// Register registers statistics-related actions.
func Register(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	log *slog.Logger,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	repo fsm.StateRepository,
	aliasRegistrar func(alias, target string),
) {
	SetChartTempDir(cfg.Charts.TempDir)
	if aliasRegistrar != nil {
		aliasRegistrar("STATS_MENU", "statistics.yaml/AUTO_SYNC_STATS")
	}

	registry.Register("get_user_stats", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		log.Info("get_user_stats action invoked", "user_id", userID, "payload", payload)

		// 1. Get FSM state to check cooldown and busy status
		state, err := repo.GetState(ctx, userID)
		if err != nil {
			log.Error("failed to get FSM state", "user_id", userID, "error", err)
			return "", nil, err
		}
		if state == nil {
			return "", nil, fmt.Errorf("fsm state not found")
		}

		// 2. Get user account
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			log.Error("failed to get user account", "user_id", userID, "error", err)
			return "", nil, err
		}

		// Extract request type early
		requestType, _ := payload["request_type"].(string)

		// If we have a pending "Done" alert, show it first without overwriting
		// BUT ONLY if this is NOT a forced update request
		if alert, ok := state.Context["_alert"].(string); ok && alert == alertDone && requestType != "full_update" {
			log.Debug("returning early due to pending alert", "user_id", userID)
			_, dbVars, _ := getStatsFromDB(ctx, acc.S21Login, queries, log)
			return "", dbVars, nil
		}
		var dbVars map[string]any

		log.Info("checking busy status", "user_id", userID, "is_generating_chart", state.Context["is_generating_chart"])

		// Check if it's already updating (with safety timeout)
		if busy, ok := state.Context["is_generating_chart"].(bool); ok && busy {
			if lastUpdateRaw, ok := state.Context["last_stats_update"].(string); ok {
				lastUpdate, _ := time.Parse(time.RFC3339, lastUpdateRaw)
				if time.Since(lastUpdate) < statsUpdateSafetyTimeout {
					log.Debug("ignoring refresh request, already updating", "user_id", userID)
					return "", map[string]any{"_alert": alertAlreadyUpdating}, nil
				}
				log.Warn("background update seems stuck, allowing retry", "user_id", userID)
			}
		}

		// Check cooldown
		cooldown := statsUpdateCooldownDuration
		if requestType == "full_update" {
			cooldown = statsUpdateManualCooldown
		}

		if lastUpdateRaw, ok := state.Context["last_stats_update"].(string); ok {
			lastUpdate, _ := time.Parse(time.RFC3339, lastUpdateRaw)
			elapsed := time.Since(lastUpdate)
			if elapsed < cooldown {
				remaining := int(cooldown.Seconds() - elapsed.Seconds())
				if remaining <= 0 {
					remaining = 1
				}
				log.Info("stats refresh cooldown active", "user_id", userID, "remaining", remaining, "type", requestType)

				cooldownMsg := ""
				if requestType == "full_update" {
					cooldownMsg = fmt.Sprintf("⏳ Не так быстро! Подожди %d сек.", remaining)
				}

				_, dbVars, _ = getStatsFromDB(ctx, acc.S21Login, queries, log)
				if dbVars == nil {
					dbVars = make(map[string]any)
				}
				dbVars["_alert"] = cooldownMsg
				dbVars["cooldown_msg"] = cooldownMsg
				dbVars["is_cooldown"] = true
				return "", dbVars, nil
			}
		}

		// Load current stats from DB for initial display
		_, dbVars, _ = getStatsFromDB(ctx, acc.S21Login, queries, log)
		if dbVars == nil {
			dbVars = make(map[string]any)
		}

		// If it's a full update request or we have no level data (empty cache), proceed synchronously
		// Debug logging to see why it enters or skips
		log.Info("checking update condition", "requests_type", requestType, "has_level", dbVars["my_level"] != nil)

		if requestType == "full_update" || dbVars["my_level"] == nil {
			log.Info("performing synchronous stats update", "user_id", userID, "login", acc.S21Login)

			// Update context to indicate busy
			state.Context["is_generating_chart"] = true
			state.Context["last_stats_update"] = time.Now().Format(time.RFC3339)
			state.Context["_alert"] = alertUpdating

			// Save state IMMEDIATELY to lock concurrent requests
			if err := repo.SetState(ctx, state); err != nil {
				log.Error("failed to lock state for update", "user_id", userID, "error", err)
			}

			// Copy dbVars to return them immediately to the user
			dbVars["is_generating_chart"] = true
			dbVars["last_stats_update"] = state.Context["last_stats_update"]
			dbVars["_alert"] = state.Context["_alert"]

			// 2. Try to get API token
			token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
			if err != nil {
				log.Warn("update: failed to get token", "error", err)
				state.Context["is_generating_chart"] = false
				state.Context["_alert"] = alertErrorToken
				_ = repo.SetState(ctx, state)
				maps.Copy(dbVars, state.Context)
				return "", dbVars, nil
			}

			// 3. Call API for fresh stats
			log.Info("fetching participant data from API", "login", acc.S21Login)
			participant, err := s21Client.GetParticipant(ctx, token, acc.S21Login)
			if err != nil {
				log.Error("failed to get participant", "error", err)
				dbVars["_alert"] = alertErrorData
				dbVars["_is_generating_chart"] = false
				return "", dbVars, nil
			}
			log.Info("got participant data", "login", acc.S21Login, "status", participant.Status)

			// Check if XP changed (through stats cache)
			oldCache, _ := queries.GetParticipantStatsCache(ctx, acc.S21Login)
			xpChanged := oldCache.S21Login == "" || oldCache.ExpValue != int32(participant.ExpValue)

			// Save stats to participant_stats_cache (единое хранилище статистики)
			points, errPoints := s21Client.GetParticipantPoints(ctx, token, acc.S21Login)
			coalition, errCoalition := s21Client.GetParticipantCoalition(ctx, token, acc.S21Login)
			feedback, errFeedback := s21Client.GetParticipantFeedback(ctx, token, acc.S21Login)

			log.Debug("API calls for stats update",
				"points_nil", points == nil, "points_err", errPoints,
				"coalition_nil", coalition == nil, "coalition_err", errCoalition,
				"feedback_nil", feedback == nil, "feedback_err", errFeedback)

			cacheParams := db.UpsertParticipantStatsCacheParams{
				S21Login:     acc.S21Login,
				Level:        participant.Level,
				ExpValue:     int32(participant.ExpValue),
				Status:       db.EnumStudentStatus(participant.Status),
				ParallelName: pgtype.Text{Valid: false},
				ClassName:    pgtype.Text{Valid: false},
			}
			if participant.Campus.ID != "" {
				var campusUUID pgtype.UUID
				if err := campusUUID.Scan(participant.Campus.ID); err != nil {
					log.Warn("failed to parse campus UUID", "campus_id", participant.Campus.ID, "error", err)
					// Don't set CampusID if UUID parsing failed
				} else {
					cacheParams.CampusID = campusUUID
					// Ensure campus exists in DB; fetch & upsert if missing
					if err := service.EnsureCampusPresent(ctx, queries, s21Client, token, log, participant.Campus.ID); err != nil {
						log.Error("failed to ensure campus present during stats update", "login", acc.S21Login, "campus_id", participant.Campus.ID, "error", err)
					}
				}
			}
			if participant.ParallelName != nil {
				cacheParams.ParallelName = pgtype.Text{String: *participant.ParallelName, Valid: true}
			}
			if participant.ClassName != nil {
				cacheParams.ClassName = pgtype.Text{String: *participant.ClassName, Valid: true}
			}
			if points != nil {
				cacheParams.Prp = points.PeerReviewPoints
				cacheParams.Crp = points.CodeReviewPoints
				cacheParams.Coins = points.Coins
			}
			if coalition != nil && coalition.CoalitionID != 0 {
				cacheParams.CoalitionID = pgtype.Int2{Int16: int16(coalition.CoalitionID), Valid: true}
				if err := service.EnsureCoalitionPresent(ctx, queries, coalition, log); err != nil {
					log.Error("failed to ensure coalition present during stats update", "login", acc.S21Login, "coalition_id", coalition.CoalitionID, "error", err)
				}
			}
			if feedback != nil {
				cacheParams.Integrity = pgtype.Float4{Float32: float32(feedback.Integrity), Valid: true}
				cacheParams.Friendliness = pgtype.Float4{Float32: float32(feedback.Friendliness), Valid: true}
				cacheParams.Punctuality = pgtype.Float4{Float32: float32(feedback.Punctuality), Valid: true}
				cacheParams.Thoroughness = pgtype.Float4{Float32: float32(feedback.Thoroughness), Valid: true}
			}

			log.Info("upserting participant stats cache",
				"login", cacheParams.S21Login,
				"level", cacheParams.Level,
				"exp", cacheParams.ExpValue,
				"status", cacheParams.Status,
				"campus_id_valid", cacheParams.CampusID.Valid,
				"campus_id_value", cacheParams.CampusID,
				"coalition_id_valid", cacheParams.CoalitionID.Valid,
				"coalition_id_value", cacheParams.CoalitionID,
				"prp", cacheParams.Prp,
				"crp", cacheParams.Crp,
				"coins", cacheParams.Coins)

			if err := queries.UpsertParticipantStatsCache(ctx, cacheParams); err != nil {
				log.Error("failed to upsert participant stats cache in get_user_stats", "login", acc.S21Login, "error", err)
			} else {
				log.Info("successfully upserted participant stats cache", "login", acc.S21Login)
			}

			var chartPath string
			if xpChanged || requestType == "full_update" {
				log.Info("XP changed, refreshing skills and chart", "login", acc.S21Login)
				skillsResp, err := s21Client.GetParticipantSkills(ctx, token, acc.S21Login)
				if err == nil {
					skillMap := make(map[string]int32)
					for _, s := range skillsResp.Skills {
						skillMap[s.Name] = s.Points
						hash := hashSkillName(s.Name)
						_, _ = queries.UpsertSkill(ctx, db.UpsertSkillParams{
							ID: hash, Name: s.Name, Category: pgtype.Text{String: defaultSkillCategory, Valid: true},
						})
						_ = queries.UpsertParticipantSkill(ctx, db.UpsertParticipantSkillParams{
							S21Login: acc.S21Login, SkillID: hash, Value: s.Points,
						})
					}
					chartPath, _ = generateRadarChart(map[string]map[string]int32{acc.S21Login: skillMap}, []string{acc.S21Login})
				} else {
					log.Warn("failed to refresh skills from API, falling back to DB for chart", "login", acc.S21Login, "error", err)
				}
			} else {
				// XP не изменился — используем сохранённые в БД навыки.
				skills, err := queries.GetParticipantSkills(ctx, acc.S21Login)
				if err == nil && len(skills) > 0 {
					skillMap := make(map[string]int32)
					for _, s := range skills {
						skillMap[s.Name] = s.Value
					}
					chartPath, _ = generateRadarChart(map[string]map[string]int32{acc.S21Login: skillMap}, []string{acc.S21Login})
				} else if err != nil {
					log.Warn("failed to load skills from DB for chart", "login", acc.S21Login, "error", err)
				}
			}

			// Prepare updates for context
			state.Context["is_generating_chart"] = false
			state.Context["_alert"] = alertDone
			state.Context["my_exp"] = participant.ExpValue
			state.Context["my_level"] = participant.Level
			if feedback != nil {
				state.Context["my_interest"] = fmt.Sprintf(socialMetricFormat, feedback.Integrity)
				state.Context["my_friendliness"] = fmt.Sprintf(socialMetricFormat, feedback.Friendliness)
				state.Context["my_punctuality"] = fmt.Sprintf(socialMetricFormat, feedback.Punctuality)
				state.Context["my_thoroughness"] = fmt.Sprintf(socialMetricFormat, feedback.Thoroughness)
			}
			if points != nil {
				state.Context["my_prps"] = points.PeerReviewPoints
				state.Context["my_crps"] = points.CodeReviewPoints
				state.Context["my_coins"] = points.Coins
			}
			if participant.Campus.ID != "" {
				state.Context["my_campus"] = participant.Campus.ShortName
				b := cacheParams.CampusID.Bytes
				state.Context["campus_id"] = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
			}
			if chartPath != "" {
				state.Context["radar_chart_path"] = chartPath
			}

			// Save final state
			_ = repo.SetState(ctx, state)

			// Update dbVars to return them immediately to the user
			maps.Copy(dbVars, state.Context)
		}

		return "", dbVars, nil
	})

	registry.Register("get_peer_data_with_permissions", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		lang, _ := payload["language"].(string)
		login, ok := payload["login"].(string)
		if !ok {
			return "", nil, fmt.Errorf("login not found in payload")
		}

		// 0. Cache check: используем таблицу participant_stats_cache.
		if cacheRow, err := queries.GetParticipantStatsCache(ctx, login); err == nil {
			isExpelled := cacheRow.Status == db.EnumStudentStatusEXPELLED
			isFresh := cacheRow.LatSyncedAt.Valid && time.Since(cacheRow.LatSyncedAt.Time) < peerDataCacheFreshnessDuration
			if isExpelled || isFresh {
				log.Info("using cached peer data", "login", login, "is_expelled", isExpelled, "is_fresh", isFresh)
				return getPeerStatsFromCache(ctx, lang, login, cacheRow, queries, log)
			}
		}

		// 1. Get token
		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err != nil {
			log.Warn("failed to get valid token, falling back to cache/DB", "error", err)
			if cacheRow, cErr := queries.GetParticipantStatsCache(ctx, login); cErr == nil {
				return getPeerStatsFromCache(ctx, lang, login, cacheRow, queries, log)
			}
			return getPeerStatsFromDB(ctx, lang, login, queries, log)
		}

		// 2. Fetch peer from API
		participant, err := s21Client.GetParticipant(ctx, token, login)
		if err != nil {
			log.Error("failed to get peer from API", "peer", login, "error", err)
			if cacheRow, cErr := queries.GetParticipantStatsCache(ctx, login); cErr == nil {
				return getPeerStatsFromCache(ctx, lang, login, cacheRow, queries, log)
			}
			return getPeerStatsFromDB(ctx, lang, login, queries, log)
		}

		points, _ := s21Client.GetParticipantPoints(ctx, token, login)

		// If peer is expelled, do NOT query coalition (unnecessary call)
		var coalition *s21.ParticipantCoalitionV1DTO
		if participant.Status != string(db.EnumStudentStatusEXPELLED) {
			coalition, _ = s21Client.GetParticipantCoalition(ctx, token, login)
		}

		var feedback *s21.ParticipantFeedbackV1DTO
		if participant.Status != string(db.EnumStudentStatusEXPELLED) {
			feedback, _ = s21Client.GetParticipantFeedback(ctx, token, login)
		}

		// 3. Prepare variables
		vars := map[string]any{
			"peer_found":               true,
			"peer_login":               participant.Login,
			"peer_campus":              participant.Campus.ShortName,
			"peer_coalition":           defaultCoalitionValue,
			"peer_level":               participant.Level,
			"peer_exp":                 participant.ExpValue,
			"peer_status":              participant.Status,
			"peer_coins":               0,
			"peer_telegram":            "",
			"peer_id":                  0,
			"peer_interest":            defaultSocialMetricValue,
			"peer_friendliness":        defaultSocialMetricValue,
			"peer_punctuality":         defaultSocialMetricValue,
			"peer_thoroughness":        defaultSocialMetricValue,
			"peer_prps":                0,
			"peer_crps":                0,
			"alternative_contact":      "",
			"alternative_contact_line": "",
		}

		if participant.ClassName != nil {
			vars["peer_class"] = *participant.ClassName
		} else {
			vars["peer_class"] = defaultEmptyValue
		}
		if participant.ParallelName != nil {
			vars["peer_parallel"] = *participant.ParallelName
		} else {
			vars["peer_parallel"] = defaultEmptyValue
		}

		if points != nil {
			vars["peer_coins"] = points.Coins
			vars["peer_prps"] = points.PeerReviewPoints
			vars["peer_crps"] = points.CodeReviewPoints
		}
		if coalition != nil {
			vars["peer_coalition"] = coalition.CoalitionName
		}
		if feedback != nil {
			log.Info("got peer feedback from API", "login", login, "interest", feedback.Integrity, "friendliness", feedback.Friendliness, "punctuality", feedback.Punctuality, "thoroughness", feedback.Thoroughness)
			vars["peer_interest"] = fmt.Sprintf(socialMetricFormat, feedback.Integrity)
			vars["peer_friendliness"] = fmt.Sprintf(socialMetricFormat, feedback.Friendliness)
			vars["peer_punctuality"] = fmt.Sprintf(socialMetricFormat, feedback.Punctuality)
			vars["peer_thoroughness"] = fmt.Sprintf(socialMetricFormat, feedback.Thoroughness)
		} else if participant.Status != string(db.EnumStudentStatusEXPELLED) {
			log.Warn("peer feedback from API is nil", "login", login)
		}

		log.Info("get_peer_data_with_permissions vars final", "vars", vars)

		// 4. Сохраняем в кеш (participant_stats_cache) — единственное место хранения статистики.
		upsertParams := db.UpsertParticipantStatsCacheParams{
			S21Login:     login,
			Level:        participant.Level,
			ExpValue:     int32(participant.ExpValue),
			Status:       db.EnumStudentStatus(participant.Status),
			ParallelName: pgtype.Text{Valid: false},
			ClassName:    pgtype.Text{Valid: false},
		}
		if participant.Campus.ID != "" {
			var campusUUID pgtype.UUID
			if err := campusUUID.Scan(participant.Campus.ID); err != nil {
				log.Warn("failed to parse campus UUID in get_peer_data_with_permissions", "campus_id", participant.Campus.ID, "error", err)
				// Don't set CampusID if UUID parsing failed
			} else {
				upsertParams.CampusID = campusUUID
				// Ensure campus exists in DB; fetch & upsert if missing
				_ = service.EnsureCampusPresent(ctx, queries, s21Client, token, log, participant.Campus.ID)
			}
		}
		if participant.ParallelName != nil {
			upsertParams.ParallelName = pgtype.Text{String: *participant.ParallelName, Valid: true}
		}
		if participant.ClassName != nil {
			upsertParams.ClassName = pgtype.Text{String: *participant.ClassName, Valid: true}
		}
		if points != nil {
			upsertParams.Prp = points.PeerReviewPoints
			upsertParams.Crp = points.CodeReviewPoints
			upsertParams.Coins = points.Coins
		}
		if coalition != nil {
			upsertParams.CoalitionID = pgtype.Int2{Int16: int16(coalition.CoalitionID), Valid: true}
		}
		if feedback != nil {
			upsertParams.Integrity = pgtype.Float4{Float32: float32(feedback.Integrity), Valid: true}
			upsertParams.Friendliness = pgtype.Float4{Float32: float32(feedback.Friendliness), Valid: true}
			upsertParams.Punctuality = pgtype.Float4{Float32: float32(feedback.Punctuality), Valid: true}
			upsertParams.Thoroughness = pgtype.Float4{Float32: float32(feedback.Thoroughness), Valid: true}
		}
		if err := queries.UpsertParticipantStatsCache(ctx, upsertParams); err != nil {
			log.Error("failed to upsert participant stats cache", "login", login, "error", err)
		}

		// 5. Telegram/peer_id — только если пир зарегистрирован в боте (есть в user_accounts)
		peerTelegram := ""
		peerAcc, err := queries.GetUserAccountByS21Login(ctx, login)
		if err == nil {
			vars["peer_id"] = peerAcc.ExternalID
			if regUser, rErr := queries.GetRegisteredUserByS21Login(ctx, login); rErr == nil && regUser.AlternativeContact.Valid {
				vars["alternative_contact"] = regUser.AlternativeContact.String
			}
			// Get telegram username from peer profile (via participant_stats_cache + user_accounts)
			peerProfile, err := queries.GetPeerProfile(ctx, login)
			if err == nil && peerProfile.TelegramUsername != "" && peerProfile.IsSearchable.Valid && peerProfile.IsSearchable.Bool {
				peerTelegram = peerProfile.TelegramUsername
			}
		}
		vars["peer_telegram"] = peerTelegram
		vars["peer_contact_line"] = buildPeerContactLine(lang, peerTelegram)

		return "", vars, nil
	})

	registry.Register("get_user_skills", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		// Find login
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		// ONLY use DB for own skills to avoid API lag as requested
		skills, err := queries.GetParticipantSkills(ctx, acc.S21Login)
		if err != nil {
			return "", nil, err
		}

		skillMap := make(map[string]int32)
		for _, s := range skills {
			skillMap[s.Name] = s.Value
		}

		return "", map[string]any{
			"my_skills": skillMap,
		}, nil
	})

	registry.Register("generate_radar_chart", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		usersRaw, ok := payload["users"].([]any)
		if !ok {
			return "", nil, fmt.Errorf("users list not found in payload")
		}

		usersData := make(map[string]map[string]int32)
		var orderedLogins []string
		var token string

		for _, uRaw := range usersRaw {
			var login string
			isSelf := false
			switch v := uRaw.(type) {
			case string:
				if v == "$context.user_id" {
					acc, _ := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
						Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
					})
					login = acc.S21Login
					isSelf = true
				} else if _, err := strconv.ParseInt(v, 10, 64); err == nil {
					acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
						Platform: db.EnumPlatformTelegram, ExternalID: v,
					})
					if err == nil {
						login = acc.S21Login
						isSelf = (v == fmt.Sprintf("%d", userID))
					} else {
						login = v
					}
				} else {
					login = v
				}
			}

			if login == "" {
				continue
			}
			orderedLogins = append(orderedLogins, login)

			// Try payload first (if passed from get_user_skills)
			if isSelf {
				if skillsFromPayload, ok := payload["my_skills"].(map[string]int32); ok && len(skillsFromPayload) > 0 {
					usersData[login] = skillsFromPayload
					continue
				}
			}

			// Try DB
			skills, err := queries.GetParticipantSkills(ctx, login)
			if err == nil && len(skills) > 0 {
				skillsMap := make(map[string]int32)
				for _, s := range skills {
					skillsMap[s.Name] = s.Value
				}
				usersData[login] = skillsMap
				continue
			}

			// If not in DB, try API (including self as a fallback for comparisons)
			if token == "" {
				token, _ = credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
			}
			if token != "" {
				skillsResp, err := s21Client.GetParticipantSkills(ctx, token, login)
				if err == nil {
					skillsMap := make(map[string]int32)
					for _, s := range skillsResp.Skills {
						skillsMap[s.Name] = s.Points
						// Background save
						hash := hashSkillName(s.Name)
						_, _ = queries.UpsertSkill(ctx, db.UpsertSkillParams{ID: hash, Name: s.Name, Category: pgtype.Text{String: defaultSkillCategory, Valid: true}})
						_ = queries.UpsertParticipantSkill(ctx, db.UpsertParticipantSkillParams{S21Login: login, SkillID: hash, Value: s.Points})
					}
					usersData[login] = skillsMap
				}
			}
		}

		if len(usersData) == 0 {
			return "", nil, nil
		}

		chartPath, err := generateRadarChart(usersData, orderedLogins)
		if err != nil {
			return "", nil, err
		}

		return "", map[string]any{
			"radar_chart_path":      chartPath,
			"radar_comparison_path": chartPath,
		}, nil
	})
}

// getStatsFromDB загружает статистику зарегистрированного пользователя через GetMyProfile
// (registered_users JOIN participant_stats_cache).
func getStatsFromDB(ctx context.Context, s21Login string, queries db.Querier, log *slog.Logger) (string, map[string]any, error) {
	profile, err := queries.GetMyProfile(ctx, s21Login)
	if err != nil {
		log.Error("failed to get my profile from DB", "login", s21Login, "error", err)
		return "", nil, err
	}

	vars := map[string]any{
		"my_s21login": profile.S21Login,
	}

	if profile.ExpValue.Valid {
		vars["my_exp"] = profile.ExpValue.Int32
	}
	if profile.Level.Valid {
		vars["my_level"] = profile.Level.Int32
	}
	if profile.Status.Valid {
		vars["my_status"] = string(profile.Status.EnumStudentStatus)
	}

	if profile.CampusName.Valid && profile.CampusName.String != "" {
		vars["my_campus"] = profile.CampusName.String
	}
	if profile.CampusID.Valid {
		b := profile.CampusID.Bytes
		vars["campus_id"] = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	}
	if profile.CoalitionName.Valid && profile.CoalitionName.String != "" {
		vars["my_coalition"] = profile.CoalitionName.String
	}
	if profile.ClassName.Valid && profile.ClassName.String != "" {
		vars["my_class"] = profile.ClassName.String
	}
	if profile.ParallelName.Valid && profile.ParallelName.String != "" {
		vars["my_parallel"] = profile.ParallelName.String
	}
	if profile.Prp.Valid {
		vars["my_prps"] = profile.Prp.Int32
	}
	if profile.Crp.Valid {
		vars["my_crps"] = profile.Crp.Int32
	}
	if profile.Coins.Valid {
		vars["my_coins"] = profile.Coins.Int32
	}
	if profile.Integrity.Valid {
		vars["my_interest"] = fmt.Sprintf(socialMetricFormat, profile.Integrity.Float32)
	}
	if profile.Friendliness.Valid {
		vars["my_friendliness"] = fmt.Sprintf(socialMetricFormat, profile.Friendliness.Float32)
	}
	if profile.Punctuality.Valid {
		vars["my_punctuality"] = fmt.Sprintf(socialMetricFormat, profile.Punctuality.Float32)
	}
	if profile.Thoroughness.Valid {
		vars["my_thoroughness"] = fmt.Sprintf(socialMetricFormat, profile.Thoroughness.Float32)
	}

	return "", vars, nil
}

// getPeerStatsFromCache строит vars для FSM из строки participant_stats_cache (и при необходимости — telegram из user_accounts).
func getPeerStatsFromCache(ctx context.Context, lang string, login string, row db.GetParticipantStatsCacheRow, queries db.Querier, log *slog.Logger) (string, map[string]any, error) {
	vars := map[string]any{
		"peer_found":               true,
		"peer_login":               row.S21Login,
		"peer_campus":              defaultCampusValue,
		"peer_coalition":           defaultCoalitionValue,
		"peer_level":               row.Level,
		"peer_exp":                 row.ExpValue,
		"peer_coins":               row.Coins,
		"peer_prps":                row.Prp,
		"peer_crps":                row.Crp,
		"peer_telegram":            "",
		"peer_id":                  0,
		"peer_status":              string(row.Status),
		"peer_class":               defaultEmptyValue,
		"peer_parallel":            defaultEmptyValue,
		"peer_interest":            defaultSocialMetricValue,
		"peer_friendliness":        defaultSocialMetricValue,
		"peer_punctuality":         defaultSocialMetricValue,
		"peer_thoroughness":        defaultSocialMetricValue,
		"alternative_contact":      "",
		"alternative_contact_line": "",
	}
	if row.CampusName.Valid {
		vars["peer_campus"] = row.CampusName.String
	}
	if row.CoalitionName.Valid {
		vars["peer_coalition"] = row.CoalitionName.String
	}
	if row.ClassName.Valid {
		vars["peer_class"] = row.ClassName.String
	}
	if row.ParallelName.Valid {
		vars["peer_parallel"] = row.ParallelName.String
	}
	if row.Integrity.Valid {
		vars["peer_interest"] = fmt.Sprintf(socialMetricFormat, row.Integrity.Float32)
	}
	if row.Friendliness.Valid {
		vars["peer_friendliness"] = fmt.Sprintf(socialMetricFormat, row.Friendliness.Float32)
	}
	if row.Punctuality.Valid {
		vars["peer_punctuality"] = fmt.Sprintf(socialMetricFormat, row.Punctuality.Float32)
	}
	if row.Thoroughness.Valid {
		vars["peer_thoroughness"] = fmt.Sprintf(socialMetricFormat, row.Thoroughness.Float32)
	}
	peerTelegram := ""
	acc, err := queries.GetUserAccountByS21Login(ctx, login)
	if err == nil {
		vars["peer_id"] = acc.ExternalID
		if regUser, rErr := queries.GetRegisteredUserByS21Login(ctx, login); rErr == nil && regUser.AlternativeContact.Valid {
			contact := regUser.AlternativeContact.String
			vars["alternative_contact"] = contact
			vars["alternative_contact_line"] = buildAlternativeContactLine(lang, contact)
		}
		if acc.Username.Valid && acc.IsSearchable.Valid && acc.IsSearchable.Bool {
			peerTelegram = acc.Username.String
		}
	}
	vars["peer_telegram"] = peerTelegram
	vars["peer_contact_line"] = buildPeerContactLine(lang, peerTelegram)
	return "", vars, nil
}

// getPeerStatsFromDB — fallback для пиров, у которых нет записи в кеше, но есть в participant_stats_cache через GetPeerProfile.
func getPeerStatsFromDB(ctx context.Context, lang string, login string, queries db.Querier, log *slog.Logger) (string, map[string]any, error) {
	profile, err := queries.GetPeerProfile(ctx, login)
	if err != nil {
		log.Debug("peer not found in DB", "login", login, "error", err)
		return "", map[string]any{
			"peer_found": false,
		}, nil
	}

	vars := map[string]any{
		"peer_found":               true,
		"peer_login":               profile.S21Login,
		"peer_campus":              defaultCampusValue,
		"peer_coalition":           defaultCoalitionValue,
		"peer_level":               profile.Level,
		"peer_exp":                 profile.ExpValue,
		"peer_coins":               profile.Coins,
		"peer_prps":                profile.Prp,
		"peer_crps":                profile.Crp,
		"peer_telegram":            "",
		"peer_status":              string(profile.Status),
		"peer_class":               defaultEmptyValue,
		"peer_parallel":            defaultEmptyValue,
		"peer_interest":            defaultSocialMetricValue,
		"peer_friendliness":        defaultSocialMetricValue,
		"peer_punctuality":         defaultSocialMetricValue,
		"peer_thoroughness":        defaultSocialMetricValue,
		"alternative_contact":      "",
		"alternative_contact_line": "",
	}

	if profile.CampusName.Valid {
		vars["peer_campus"] = profile.CampusName.String
	}
	if profile.CoalitionName.Valid {
		vars["peer_coalition"] = profile.CoalitionName.String
	}
	if profile.ClassName.Valid {
		vars["peer_class"] = profile.ClassName.String
	}
	if profile.ParallelName.Valid {
		vars["peer_parallel"] = profile.ParallelName.String
	}
	if profile.Integrity.Valid {
		vars["peer_interest"] = fmt.Sprintf(socialMetricFormat, profile.Integrity.Float32)
	}
	if profile.Friendliness.Valid {
		vars["peer_friendliness"] = fmt.Sprintf(socialMetricFormat, profile.Friendliness.Float32)
	}
	if profile.Punctuality.Valid {
		vars["peer_punctuality"] = fmt.Sprintf(socialMetricFormat, profile.Punctuality.Float32)
	}
	if profile.Thoroughness.Valid {
		vars["peer_thoroughness"] = fmt.Sprintf(socialMetricFormat, profile.Thoroughness.Float32)
	}

	peerTelegram := ""
	if profile.TelegramUsername != "" && profile.IsSearchable.Valid && profile.IsSearchable.Bool {
		peerTelegram = profile.TelegramUsername
	}
	vars["peer_telegram"] = peerTelegram
	vars["peer_contact_line"] = buildPeerContactLine(lang, peerTelegram)
	if regUser, rErr := queries.GetRegisteredUserByS21Login(ctx, login); rErr == nil && regUser.AlternativeContact.Valid {
		contact := regUser.AlternativeContact.String
		vars["alternative_contact"] = contact
		vars["alternative_contact_line"] = buildAlternativeContactLine(lang, contact)
	}

	vars["peer_id"] = profile.ExternalID
	return "", vars, nil
}

func buildPeerContactLine(lang, telegram string) string {
	if telegram == "" {
		return ""
	}
	if lang == fsm.LangEn {
		return fmt.Sprintf("📬 *Contact:* @%s", telegram)
	}
	return fmt.Sprintf("📬 *Связь:* @%s", telegram)
}

func buildAlternativeContactLine(lang, contact string) string {
	if contact == "" {
		return ""
	}
	if lang == fsm.LangEn {
		return fmt.Sprintf("*Alt:* %s", contact)
	}
	return fmt.Sprintf("*Доп:* %s", contact)
}

func hashSkillName(name string) int32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return int32(h.Sum32())
}
