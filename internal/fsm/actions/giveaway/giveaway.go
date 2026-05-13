package giveaway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const (
	contestKey        = "sapphire_100_coins"
	stateActive       = "active"
	stateFinalizing   = "finalizing"
	stateFinished     = "finished"
	jobStatusRunning  = "running"
	jobStatusFinished = "finished"
	jobStatusFailed   = "failed"
	pageLimit         = 1000
	finalSyncPause    = 1500 * time.Millisecond
	progressPageSize  = 30
)

var excludedTitleFragments = []string{
	"qa_ex",
	"bsa_ex",
	"_ex-",
	"exam",
}

// Register registers giveaway-related actions.
func Register(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	log *slog.Logger,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	aliasRegistrar func(alias, target string),
) {
	if aliasRegistrar != nil {
		aliasRegistrar("GIVEAWAY_MENU", "giveaway.yaml/GIVEAWAY_MENU")
		aliasRegistrar("GIVEAWAY_PROGRESS", "giveaway.yaml/GIVEAWAY_PROGRESS")
		aliasRegistrar("GIVEAWAY_ADMIN_MENU", "giveaway.yaml/GIVEAWAY_ADMIN_MENU")
	}

	loadMainContext := func(ctx context.Context, userID int64, payload map[string]any) map[string]any {
		updates := map[string]any{
			"is_sapphire":             false,
			"giveaway_active":         false,
			"giveaway_is_participant": false,
			"giveaway_owner":          false,
			"giveaway_join_available": false,
			"giveaway_admin_can_stop": false,
		}

		login := strings.TrimSpace(fmt.Sprintf("%v", payload["my_s21login"]))
		isOwner := boolFromAny(payload["is_bot_owner"])

		coalition := strings.TrimSpace(fmt.Sprintf("%v", payload["my_coalition"]))
		if !isSapphireCoalitionName(coalition) {
			acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: fmt.Sprintf("%d", userID),
			})
			if err == nil {
				if login == "" {
					login = strings.TrimSpace(acc.S21Login)
				}
				profile, profileErr := queries.GetMyProfile(ctx, acc.S21Login)
				if profileErr == nil && profile.CoalitionName.Valid {
					coalition = strings.TrimSpace(profile.CoalitionName.String)
				}
			}
		}
		isSapphire := isSapphireCoalitionName(coalition)

		updates["is_sapphire"] = isSapphire
		updates["giveaway_owner"] = isOwner

		if !isSapphire && !isOwner {
			return updates
		}

		state, err := ensureContestState(ctx, queries)
		if err != nil {
			log.Warn("giveaway: failed to ensure contest state", "error", err)
			return updates
		}
		isActive := strings.TrimSpace(state.Status) == stateActive
		updates["giveaway_active"] = isActive

		isParticipant := false
		var participant db.SapphireGiveawayParticipant
		if login != "" {
			participant, err = queries.GetSapphireGiveawayParticipantByLogin(ctx, login)
			if err == nil {
				isParticipant = true
			} else if err != pgx.ErrNoRows {
				log.Warn("giveaway: failed to load participant", "login", login, "error", err)
			}
		}
		updates["giveaway_is_participant"] = isParticipant
		updates["giveaway_join_available"] = isActive && isSapphire && !isParticipant
		updates["giveaway_admin_can_stop"] = isOwner && isActive

		if isActive && isParticipant {
			token, tokenErr := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
			if tokenErr != nil {
				log.Warn("giveaway: failed to get token for progress sync", "error", tokenErr)
				return updates
			}
			if syncErr := syncParticipantProgress(ctx, queries, s21Client, token, participant); syncErr != nil {
				log.Warn("giveaway: failed to sync participant progress", "login", participant.S21Login, "error", syncErr)
			}
		}

		return updates
	}

	registry.Register("load_sapphire_giveaway_main_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", loadMainContext(ctx, userID, payload), nil
	})

	registry.Register("load_sapphire_giveaway_join_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"giveaway_join_hint": "Нажми «Участвую!», чтобы зафиксировать старт и baseline по зеленым проектам.",
		}
		mainUpdates := loadMainContext(ctx, userID, payload)
		maps.Copy(updates, mainUpdates)
		if !boolFromAny(updates["giveaway_active"]) {
			updates["giveaway_join_hint"] = "Конкурс уже остановлен."
		} else if !boolFromAny(updates["is_sapphire"]) {
			updates["giveaway_join_hint"] = "Участвовать могут только участники трайба Sapphire."
		} else if boolFromAny(updates["giveaway_is_participant"]) {
			updates["giveaway_join_hint"] = "Ты уже участвуешь в конкурсе."
		}
		return "", updates, nil
	})

	registry.Register("join_sapphire_giveaway", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"giveaway_is_participant": true,
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			updates["_alert"] = "❌ Не удалось получить профиль."
			return "", updates, nil
		}

		state, err := ensureContestState(ctx, queries)
		if err != nil {
			updates["_alert"] = "❌ Не удалось получить состояние конкурса."
			return "", updates, nil
		}
		if strings.TrimSpace(state.Status) != stateActive {
			updates["_alert"] = "🚫 Конкурс уже остановлен."
			return "", updates, nil
		}

		if !strings.Contains(strings.ToLower(fmt.Sprintf("%v", payload["my_coalition"])), "sapphire") {
			updates["_alert"] = "🚫 Участвовать могут только Sapphire."
			return "", updates, nil
		}

		if _, err := queries.GetSapphireGiveawayParticipantByLogin(ctx, acc.S21Login); err == nil {
			updates["_alert"] = "✅ Ты уже участвуешь."
			return "", updates, nil
		} else if err != pgx.ErrNoRows {
			updates["_alert"] = "❌ Ошибка проверки участия."
			return "", updates, nil
		}

		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err != nil {
			updates["_alert"] = "❌ Не удалось получить токен S21."
			return "", updates, nil
		}

		projects, err := fetchAcceptedProjectsAll(ctx, s21Client, token, acc.S21Login)
		if err != nil {
			updates["_alert"] = "❌ Не удалось получить проекты."
			return "", updates, nil
		}

		baselineSet := make(map[int64]struct{})
		for _, p := range projects {
			if !isEligibleProject(p) {
				continue
			}
			baselineSet[p.ID] = struct{}{}
		}
		baselineJSON, err := marshalProjectIDSet(baselineSet)
		if err != nil {
			updates["_alert"] = "❌ Ошибка сохранения baseline."
			return "", updates, nil
		}

		emptyJSON := []byte("[]")
		if err := queries.CreateSapphireGiveawayParticipant(ctx, db.CreateSapphireGiveawayParticipantParams{
			S21Login:             acc.S21Login,
			TelegramUserID:       userID,
			BaselineProjectIds:   baselineJSON,
			CountedProjectIds:    emptyJSON,
			CountedProjectsCount: 0,
		}); err != nil {
			updates["_alert"] = "❌ Не удалось зафиксировать участие."
			return "", updates, nil
		}

		updates["giveaway_is_participant"] = true
		updates["giveaway_join_available"] = false
		updates["_alert"] = "🎉 Участие подтверждено!"
		return "", updates, nil
	})

	registry.Register("load_sapphire_giveaway_progress", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"giveaway_progress_text":          "Пока нет участников.",
			"giveaway_progress_page":          1,
			"giveaway_progress_total_pages":   1,
			"giveaway_progress_has_prev_page": false,
			"giveaway_progress_has_next_page": false,
		}

		mainUpdates := loadMainContext(ctx, userID, payload)
		maps.Copy(updates, mainUpdates)

		rows, err := queries.ListSapphireGiveawayParticipants(ctx)
		if err != nil {
			updates["giveaway_progress_text"] = "Не удалось загрузить прогресс."
			return "", updates, nil
		}
		page := intFromAny(payload["giveaway_progress_page"], 1)
		progressText, currentPage, totalPages, hasPrev, hasNext := formatProgressPage(rows, page, progressPageSize)
		updates["giveaway_progress_text"] = progressText
		updates["giveaway_progress_page"] = currentPage
		updates["giveaway_progress_total_pages"] = totalPages
		updates["giveaway_progress_has_prev_page"] = hasPrev
		updates["giveaway_progress_has_next_page"] = hasNext
		return "", updates, nil
	})

	registry.Register("giveaway_progress_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := intFromAny(payload["giveaway_progress_page"], 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"giveaway_progress_page": page}, nil
	})

	registry.Register("giveaway_progress_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := intFromAny(payload["giveaway_progress_page"], 1)
		page++
		return "", map[string]any{"giveaway_progress_page": page}, nil
	})

	registry.Register("load_sapphire_giveaway_admin", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"giveaway_admin_status_text":  "Нет доступа.",
			"giveaway_admin_job_text":     "—",
			"giveaway_export_text":        "",
			"giveaway_admin_can_stop":     false,
			"giveaway_admin_refreshed_at": time.Now().Format("15:04:05"),
			"giveaway_admin_hint_text":    "",
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", updates, nil
		}

		isOwner := acc.S21Login == cfg.Init.SchoolLogin
		if !isOwner {
			return "", updates, nil
		}

		state, err := ensureContestState(ctx, queries)
		if err != nil {
			updates["giveaway_admin_status_text"] = "Ошибка загрузки состояния."
			return "", updates, nil
		}

		updates["giveaway_admin_can_stop"] = strings.TrimSpace(state.Status) == stateActive
		updates["giveaway_admin_status_text"] = strings.TrimSpace(state.Status)

		if job, err := queries.GetLatestSapphireGiveawaySyncJob(ctx); err == nil {
			updates["giveaway_admin_job_text"] = fmt.Sprintf(
				"id=%d status=%s processed=%d/%d failed=%d",
				job.ID,
				job.Status,
				job.ProcessedCount,
				job.TotalCount,
				job.FailedCount,
			)
			updates["giveaway_export_text"] = strings.TrimSpace(job.ExportText)
		} else if err == pgx.ErrNoRows {
			updates["giveaway_admin_job_text"] = "Проверка ещё не запускалась."
			if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", payload["language"])), fsm.LangEn) {
				updates["giveaway_admin_hint_text"] = "Press \"⛔ Stop and Final Sync\" to start the first check."
			} else {
				updates["giveaway_admin_hint_text"] = "Нажми «⛔ Остановить и актуализировать», чтобы запустить первую проверку."
			}
		} else {
			updates["giveaway_admin_job_text"] = "Не удалось загрузить job."
		}

		return "", updates, nil
	})

	registry.Register("start_sapphire_giveaway_final_sync", func(ctx context.Context, userID int64, _ map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"_alert": "❌ Не удалось запустить финальную проверку.",
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", updates, nil
		}
		if acc.S21Login != cfg.Init.SchoolLogin {
			updates["_alert"] = "🚫 Только владелец бота может запустить финальную проверку."
			return "", updates, nil
		}

		state, err := ensureContestState(ctx, queries)
		if err != nil {
			return "", updates, nil
		}
		if strings.TrimSpace(state.Status) != stateActive {
			updates["_alert"] = "ℹ️ Конкурс уже остановлен или проверка уже запущена."
			return "", updates, nil
		}

		updatedRows, err := queries.UpdateSapphireGiveawayStateStatusIfCurrent(ctx, db.UpdateSapphireGiveawayStateStatusIfCurrentParams{
			ContestKey: contestKey,
			Status:     stateActive,
			Status_2:   stateFinalizing,
		})
		if err != nil {
			return "", updates, nil
		}
		if updatedRows == 0 {
			updates["_alert"] = "ℹ️ Финальная проверка уже запущена."
			return "", updates, nil
		}

		logins, err := queries.ListSapphireGiveawayParticipantLogins(ctx)
		if err != nil {
			_ = queries.UpdateSapphireGiveawayStateStatus(ctx, db.UpdateSapphireGiveawayStateStatusParams{
				ContestKey: contestKey,
				Status:     stateActive,
			})
			return "", updates, nil
		}

		job, err := queries.CreateSapphireGiveawaySyncJob(ctx, db.CreateSapphireGiveawaySyncJobParams{
			Status:                    jobStatusRunning,
			TotalCount:                int32(len(logins)),
			ProcessedCount:            0,
			FailedCount:               0,
			ExportText:                "",
			RequestedByTelegramUserID: userID,
		})
		if err != nil {
			_ = queries.UpdateSapphireGiveawayStateStatus(ctx, db.UpdateSapphireGiveawayStateStatusParams{
				ContestKey: contestKey,
				Status:     stateActive,
			})
			return "", updates, nil
		}

		if err := queries.SetSapphireGiveawayStateFinalSyncJob(ctx, db.SetSapphireGiveawayStateFinalSyncJobParams{
			ContestKey:     contestKey,
			FinalSyncJobID: pgtype.Int8{Int64: job.ID, Valid: true},
		}); err != nil {
			_ = queries.UpdateSapphireGiveawayStateStatus(ctx, db.UpdateSapphireGiveawayStateStatusParams{
				ContestKey: contestKey,
				Status:     stateActive,
			})
			return "", updates, nil
		}

		go runFinalSyncJob(log, cfg, queries, s21Client, credService, job.ID)

		updates["_alert"] = "✅ Финальная проверка запущена."
		updates["giveaway_admin_can_stop"] = false
		return "", updates, nil
	})
}

func runFinalSyncJob(
	log *slog.Logger,
	cfg *config.Config,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	jobID int64,
) {
	ctx := context.Background()

	logins, err := queries.ListSapphireGiveawayParticipantLogins(ctx)
	if err != nil {
		_ = queries.FinishSapphireGiveawaySyncJob(ctx, db.FinishSapphireGiveawaySyncJobParams{
			ID:             jobID,
			Status:         jobStatusFailed,
			ProcessedCount: 0,
			FailedCount:    0,
			ExportText:     "",
		})
		_ = queries.UpdateSapphireGiveawayStateStatus(ctx, db.UpdateSapphireGiveawayStateStatusParams{
			ContestKey: contestKey,
			Status:     stateFinished,
		})
		return
	}

	token, tokenErr := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
	processed := int32(0)
	failed := int32(0)

	for _, login := range logins {
		if tokenErr != nil {
			failed++
			processed++
			_ = queries.UpdateSapphireGiveawayParticipantSyncError(ctx, db.UpdateSapphireGiveawayParticipantSyncErrorParams{
				S21Login:      login,
				LastSyncError: tokenErr.Error(),
			})
		} else {
			participant, err := queries.GetSapphireGiveawayParticipantByLogin(ctx, login)
			if err != nil {
				failed++
				processed++
			} else {
				if err := syncParticipantProgress(ctx, queries, s21Client, token, participant); err != nil {
					failed++
					_ = queries.UpdateSapphireGiveawayParticipantSyncError(ctx, db.UpdateSapphireGiveawayParticipantSyncErrorParams{
						S21Login:      login,
						LastSyncError: err.Error(),
					})
				}
				_ = queries.SetSapphireGiveawayParticipantFinalSyncJob(ctx, db.SetSapphireGiveawayParticipantFinalSyncJobParams{
					S21Login:           login,
					LastFinalSyncJobID: pgtype.Int8{Int64: jobID, Valid: true},
				})
				processed++
			}
		}

		_ = queries.UpdateSapphireGiveawaySyncJobProgress(ctx, db.UpdateSapphireGiveawaySyncJobProgressParams{
			ID:             jobID,
			ProcessedCount: processed,
			FailedCount:    failed,
		})
		time.Sleep(finalSyncPause)
	}

	rows, err := queries.ListSapphireGiveawayParticipants(ctx)
	exportText := ""
	if err == nil {
		exportText = buildExportText(rows)
	}

	_ = queries.FinishSapphireGiveawaySyncJob(ctx, db.FinishSapphireGiveawaySyncJobParams{
		ID:             jobID,
		Status:         jobStatusFinished,
		ProcessedCount: processed,
		FailedCount:    failed,
		ExportText:     exportText,
	})
	_ = queries.UpdateSapphireGiveawayStateStatus(ctx, db.UpdateSapphireGiveawayStateStatusParams{
		ContestKey: contestKey,
		Status:     stateFinished,
	})

	if log != nil {
		log.Info("giveaway: final sync finished", "job_id", jobID, "processed", processed, "failed", failed)
	}
}

func syncParticipantProgress(
	ctx context.Context,
	queries db.Querier,
	s21Client *s21.Client,
	token string,
	participant db.SapphireGiveawayParticipant,
) error {
	projects, err := fetchAcceptedProjectsAll(ctx, s21Client, token, participant.S21Login)
	if err != nil {
		return err
	}

	baseline, err := decodeProjectIDSet(participant.BaselineProjectIds)
	if err != nil {
		return err
	}
	counted, err := decodeProjectIDSet(participant.CountedProjectIds)
	if err != nil {
		return err
	}

	for _, p := range projects {
		if !isEligibleProject(p) {
			continue
		}
		if _, exists := baseline[p.ID]; exists {
			continue
		}
		counted[p.ID] = struct{}{}
	}

	encoded, err := marshalProjectIDSet(counted)
	if err != nil {
		return err
	}

	return queries.UpdateSapphireGiveawayParticipantProgress(ctx, db.UpdateSapphireGiveawayParticipantProgressParams{
		S21Login:             participant.S21Login,
		CountedProjectIds:    encoded,
		CountedProjectsCount: int32(len(counted)),
	})
}

func fetchAcceptedProjectsAll(ctx context.Context, s21Client *s21.Client, token, login string) ([]s21.ParticipantProjectV1DTO, error) {
	projects := make([]s21.ParticipantProjectV1DTO, 0, pageLimit)
	offset := 0
	for {
		resp, err := s21Client.GetParticipantProjects(ctx, token, login, pageLimit, offset, "ACCEPTED")
		if err != nil {
			return nil, err
		}
		if resp == nil || len(resp.Projects) == 0 {
			break
		}
		projects = append(projects, resp.Projects...)
		if len(resp.Projects) < pageLimit {
			break
		}
		offset += len(resp.Projects)
	}
	return projects, nil
}

func isEligibleProject(project s21.ParticipantProjectV1DTO) bool {
	title := strings.ToLower(strings.TrimSpace(project.Title))
	courseTitle := strings.ToLower(strings.TrimSpace(project.CourseTitle))

	if strings.Contains(title, "bootcamp") || strings.Contains(courseTitle, "bootcamp") {
		return false
	}
	for _, fragment := range excludedTitleFragments {
		if strings.Contains(title, fragment) {
			return false
		}
	}
	return true
}

func decodeProjectIDSet(raw []byte) (map[int64]struct{}, error) {
	out := make(map[int64]struct{})
	if len(raw) == 0 {
		return out, nil
	}
	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

func marshalProjectIDSet(set map[int64]struct{}) ([]byte, error) {
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return json.Marshal(ids)
}

func formatProgressPage(rows []db.SapphireGiveawayParticipant, page, pageSize int) (string, int, int, bool, bool) {
	if len(rows) == 0 {
		return "Пока нет участников.", 1, 1, false, false
	}
	if pageSize <= 0 {
		pageSize = progressPageSize
	}

	totalPages := max((len(rows)+pageSize-1)/pageSize, 1)
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	end := min(start+pageSize, len(rows))

	var b strings.Builder
	for i, row := range rows[start:end] {
		fmt.Fprintf(&b, "%d. %s – %d\n", start+i+1, row.S21Login, row.CountedProjectsCount)
	}
	fmt.Fprintf(&b, "\nСтраница %d/%d", page, totalPages)

	return strings.TrimSpace(b.String()), page, totalPages, page > 1, page < totalPages
}

func buildExportText(rows []db.SapphireGiveawayParticipant) string {
	if len(rows) == 0 {
		return ""
	}
	cp := make([]db.SapphireGiveawayParticipant, len(rows))
	copy(cp, rows)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].S21Login < cp[j].S21Login
	})
	chunks := make([]string, 0, len(cp))
	for _, row := range cp {
		chunks = append(chunks, fmt.Sprintf("%s:%d", row.S21Login, row.CountedProjectsCount))
	}
	return strings.Join(chunks, " ")
}

func boolFromAny(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func intFromAny(v any, def int) int {
	switch x := v.(type) {
	case int:
		if x > 0 {
			return x
		}
	case int32:
		if x > 0 {
			return int(x)
		}
	case int64:
		if x > 0 {
			return int(x)
		}
	case float64:
		if x > 0 {
			return int(x)
		}
	}
	return def
}

func isSapphireCoalitionName(name string) bool {
	v := strings.ToLower(strings.TrimSpace(name))
	if v == "" || v == "—" || v == "-" {
		return false
	}
	return strings.Contains(v, "sapphire") ||
		strings.Contains(v, "sapphir") ||
		strings.Contains(v, "сапфир") ||
		strings.Contains(v, "сапф")
}
