package statistics

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
	"github.com/vgy789/noemx21-bot/internal/service/telegramvisibility"
)

const (
	// Cooldown and timeout durations
	statsUpdateCooldownDuration    = 5 * time.Minute  // Дефолтный кулдаун для автоматических обновлений
	statsUpdateManualCooldown      = 10 * time.Second // Кулдаун для ручного обновления (кнопка Refresh)
	statsUpdateSafetyTimeout       = 30 * time.Second // Таймаут для проверки зависшего обновления
	peerDataCacheFreshnessDuration = 10 * time.Minute // Кэшируем данные пира на 10 минут

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

var latinLoginRegex = regexp.MustCompile(`[a-z]+`)

func campusNameString(name any) string {
	switch v := name.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case pgtype.Text:
		if v.Valid {
			return v.String
		}
		return ""
	case interface{ String() string }:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

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
		isBotOwner := (acc.S21Login == cfg.Init.SchoolLogin)
		state.Context["is_bot_owner"] = isBotOwner

		// Extract request type early
		requestType, _ := payload["request_type"].(string)

		// If we have a pending "Done" alert, show it first without overwriting
		// BUT ONLY if this is NOT a forced update request
		if alert, ok := state.Context["_alert"].(string); ok && alert == alertDone && requestType != "full_update" {
			log.Debug("returning early due to pending alert", "user_id", userID)
			_, dbVars, _ := getStatsFromDB(ctx, acc.S21Login, queries, log)
			if dbVars == nil {
				dbVars = make(map[string]any)
			}
			dbVars["is_bot_owner"] = (acc.S21Login == cfg.Init.SchoolLogin)
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
				dbVars["is_bot_owner"] = (acc.S21Login == cfg.Init.SchoolLogin)
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

		// Set bot owner flag for admin features
		dbVars["is_bot_owner"] = (acc.S21Login == cfg.Init.SchoolLogin)

		// School21 refreshes are performed only by the local browser snapshot tool.
		// A manual refresh therefore returns the latest imported cache unchanged.
		if requestType == "full_update" {
			dbVars["_alert"] = alertDone
		}

		return "", dbVars, nil
	})

	registry.Register("get_peer_data_with_permissions", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		lang, _ := payload["language"].(string)
		login, ok := payload["login"].(string)
		if !ok {
			return "", nil, fmt.Errorf("login not found in payload")
		}
		login = strings.ToLower(strings.TrimSpace(login))
		login = latinLoginRegex.FindString(login)
		if login == "" {
			return "", map[string]any{"peer_found": false}, nil
		}
		payload["login"] = login

		allowed, err := queries.IsTelegramAccountEffectivelySearchable(ctx, fmt.Sprintf("%d", userID))
		if err != nil || !allowed {
			return "", map[string]any{"peer_found": false, "search_allowed": false}, nil
		}
		if cacheRow, err := queries.GetParticipantStatsCache(ctx, login); err == nil {
			return getPeerStatsFromCache(ctx, lang, login, cacheRow, queries, log)
		}
		return getPeerStatsFromDB(ctx, lang, login, queries, log)

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

	registry.Register("search_peer", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		login, ok := payload["login"].(string)
		if !ok {
			return "", nil, fmt.Errorf("login not found in payload")
		}
		login = strings.ToLower(strings.TrimSpace(login))
		login = latinLoginRegex.FindString(login)
		if login == "" {
			return "", map[string]any{"peer_found": false}, nil
		}
		payload["login"] = login
		action, ok := registry.Get("get_peer_data_with_permissions")
		if !ok {
			return "", nil, fmt.Errorf("action not found")
		}
		return action(ctx, userID, payload)
	})

	registry.Register("generate_radar_chart", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		usersRaw, ok := payload["users"].([]any)
		if !ok {
			return "", nil, fmt.Errorf("users list not found in payload")
		}

		usersData := make(map[string]map[string]int32)
		var orderedLogins []string

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

				// Clean login just in case it contains extra text
				login = strings.ToLower(strings.TrimSpace(login))
				login = latinLoginRegex.FindString(login)
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

			// Missing skills remain absent until a local snapshot import provides them.
		}

		if len(usersData) == 0 {
			return "", nil, nil
		}

		chartPath, err := generateRadarChart(usersData, orderedLogins)
		if err != nil {
			return "", nil, err
		}

		legacyChartPath := ""
		if len(usersData) == 1 {
			legacyChartPath, _ = generateRadarChartLegacy(usersData, orderedLogins)
		}

		return "", map[string]any{
			"radar_chart_path":        chartPath,
			"radar_chart_path_legacy": legacyChartPath,
			"radar_comparison_path":   chartPath,
		}, nil
	})
}

func triggerMemberTagSyncOnLevelChange(ctx context.Context, log *slog.Logger, telegramUserID int64, oldLevel, newLevel int32) {
	if telegramUserID == 0 || oldLevel == newLevel {
		return
	}
	runner, ok := fsm.MemberTagRunnerFromContext(ctx)
	if !ok {
		return
	}
	if err := runner.SyncMemberTagsForRegisteredUser(ctx, telegramUserID); err != nil {
		if log != nil {
			log.Warn("failed to sync member tags on level change", "telegram_user_id", telegramUserID, "old_level", oldLevel, "new_level", newLevel, "error", err)
		}
	}
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
		"my_s21login": strings.ToLower(profile.S21Login),
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

	if campusName := campusNameString(profile.CampusName); campusName != "" {
		vars["my_campus"] = campusName
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
	if profile.ProfileUpdatedAt.Valid {
		vars["profile_updated_at"] = profile.ProfileUpdatedAt.Time.UTC().Format(time.RFC3339)
	}

	return "", vars, nil
}

// getPeerStatsFromCache строит vars для FSM из строки participant_stats_cache (и при необходимости — telegram из user_accounts).
func getPeerStatsFromCache(ctx context.Context, lang string, login string, row db.GetParticipantStatsCacheRow, queries db.Querier, log *slog.Logger) (string, map[string]any, error) {
	vars := map[string]any{
		"peer_found":               true,
		"peer_login":               strings.ToLower(row.S21Login),
		"peer_campus":              defaultCampusValue,
		"peer_coalition":           defaultCoalitionValue,
		"peer_level":               row.Level,
		"peer_exp":                 row.ExpValue,
		"peer_coins":               row.Coins,
		"peer_prps":                row.Prp,
		"peer_crps":                row.Crp,
		"peer_telegram":            "",
		"peer_status":              string(row.Status),
		"peer_class":               defaultEmptyValue,
		"peer_parallel":            defaultEmptyValue,
		"peer_interest":            defaultSocialMetricValue,
		"peer_friendliness":        defaultSocialMetricValue,
		"peer_punctuality":         defaultSocialMetricValue,
		"peer_thoroughness":        defaultSocialMetricValue,
		"alternative_contact":      "",
		"alternative_contact_line": "",
		"profile_updated_at":       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if campusName := campusNameString(row.CampusName); campusName != "" {
		vars["peer_campus"] = campusName
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
		if regUser, rErr := queries.GetRegisteredUserByS21Login(ctx, login); rErr == nil && regUser.AlternativeContact.Valid {
			contact := regUser.AlternativeContact.String
			vars["alternative_contact"] = contact
			vars["alternative_contact_line"] = buildAlternativeContactLine(lang, contact)
		}
		if acc.Username.Valid && telegramvisibility.Effective(acc.IsSearchable, acc.TelegramVisibilityEndsAt, time.Now()) {
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
		log.Debug("peer not found in profile cache", "error_type", fmt.Sprintf("%T", err))
		return "", map[string]any{
			"peer_found": false,
		}, nil
	}

	vars := map[string]any{
		"peer_found":               true,
		"peer_login":               strings.ToLower(profile.S21Login),
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
		"profile_updated_at":       profile.ProfileUpdatedAt.Time.UTC().Format(time.RFC3339),
	}

	if campusName := campusNameString(profile.CampusName); campusName != "" {
		vars["peer_campus"] = campusName
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
	if profile.TelegramUsername != "" && profile.IsSearchable {
		peerTelegram = profile.TelegramUsername
	}
	vars["peer_telegram"] = peerTelegram
	vars["peer_contact_line"] = buildPeerContactLine(lang, peerTelegram)
	if regUser, rErr := queries.GetRegisteredUserByS21Login(ctx, login); rErr == nil && regUser.AlternativeContact.Valid {
		contact := regUser.AlternativeContact.String
		vars["alternative_contact"] = contact
		vars["alternative_contact_line"] = buildAlternativeContactLine(lang, contact)
	}

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
