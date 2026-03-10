package admin_groups

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	maxDefenderWhitelistButtons = 10
	defenderWhitelistLoadLimit  = 100
	defenderLogsLoadLimit       = 10

	alertDefenderNoRightsRU = "Нужно право на бан участников. Включите для бота разрешение ban users и повторите запуск."

	defenderReasonUnregistered = "unregistered"
	defenderReasonBlocked      = "blocked"
	defenderReasonExpelled     = "expelled"
)

func registerDefenderActions(registry *fsm.LogicRegistry, log logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}

	registry.Register("load_group_defender_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		return "", updates, nil
	})

	registry.Register("set_group_defender_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "def_enable_on"
		if _, err := queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
			DefenderEnabled:     enable,
		}); err != nil {
			log.Warn("admin groups: failed to update defender_enabled", "chat_id", chatID, "user_id", userID, "error", err)
		}
		if !enable {
			if _, err := queries.UpdateTelegramGroupDefenderRemoveBlockedByOwner(ctx, db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
				ChatID:                chatID,
				OwnerTelegramUserID:   userID,
				DefenderRemoveBlocked: false,
			}); err != nil {
				log.Warn("admin groups: failed to auto-disable defender_remove_blocked", "chat_id", chatID, "user_id", userID, "error", err)
			}
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("set_group_defender_remove_blocked", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "def_blocked_on"
		if _, err := queries.UpdateTelegramGroupDefenderRemoveBlockedByOwner(ctx, db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
			ChatID:                chatID,
			OwnerTelegramUserID:   userID,
			DefenderRemoveBlocked: enable,
		}); err != nil {
			log.Warn("admin groups: failed to update defender_remove_blocked", "chat_id", chatID, "user_id", userID, "error", err)
		}
		if enable {
			if _, err := queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				DefenderEnabled:     true,
			}); err != nil {
				log.Warn("admin groups: failed to auto-enable defender", "chat_id", chatID, "user_id", userID, "error", err)
			}
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("run_group_defender", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", updates, nil
		}

		runner, ok := fsm.DefenderRunnerFromContext(ctx)
		if !ok {
			updates["defender_last_run_summary_ru"] = "Недоступно: transport runner не подключен."
			updates["defender_last_run_summary_en"] = "Unavailable: transport runner is not configured."
			return "", updates, nil
		}

		result, err := runner.RunGroupDefender(ctx, userID, chatID)
		if err != nil {
			updates["defender_last_run_summary_ru"] = fmt.Sprintf("Ошибка запуска: %v", err)
			updates["defender_last_run_summary_en"] = fmt.Sprintf("Run failed: %v", err)
			updates["defender_last_run_notice_ru"] = ""
			updates["defender_last_run_notice_en"] = ""
			return "", updates, nil
		}

		summary := fmt.Sprintf(
			"removed=%d, skip_whitelist=%d, skip_not_member=%d, skip_no_rights=%d, hit_unregistered=%d, hit_blocked=%d, errors=%d",
			result.Removed,
			result.SkippedWhitelist,
			result.SkippedNotMember,
			result.SkippedNoRights,
			result.SkippedUnregistered,
			result.SkippedBlocked,
			result.Errors,
		)
		updates["defender_last_run_summary_ru"] = summary
		updates["defender_last_run_summary_en"] = summary
		updates["defender_last_run_notice_ru"] = ""
		updates["defender_last_run_notice_en"] = ""
		if result.SkippedNoRights > 0 {
			updates["defender_last_run_notice_ru"] = "⚠️ Нужно право на ban users. Назначьте боту право бана участников и запустите снова."
			updates["defender_last_run_notice_en"] = "⚠️ Bot needs ban users permission. Grant ban rights and run again."
		}

		return "", updates, nil
	})

	registry.Register("preview_group_defender_violations", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			return "", updates, nil
		}

		type candidate struct {
			ID     int64
			Name   string
			Uname  string
			Reason string
		}
		candidates := make([]candidate, 0)

		if runner, ok := fsm.DefenderRunnerFromContext(ctx); ok {
			items, err := runner.PreviewGroupDefenderCandidates(ctx, userID, chatID)
			if err != nil {
				updates["defender_preview_summary_ru"] = "Не удалось собрать список участников для предпросмотра."
				updates["defender_preview_summary_en"] = "Failed to build preview from known members."
				return "", updates, nil
			}
			for _, item := range items {
				candidates = append(candidates, candidate{
					ID:     item.TelegramUserID,
					Name:   item.DisplayName,
					Uname:  item.Username,
					Reason: item.Reason,
				})
			}
		} else {
			knownMembers, err := queries.ListTelegramGroupKnownMembers(ctx, chatID)
			if err != nil {
				updates["defender_preview_summary_ru"] = "Не удалось собрать список участников для предпросмотра."
				updates["defender_preview_summary_en"] = "Failed to build preview from known members."
				return "", updates, nil
			}

			seen := make(map[int64]struct{})
			for _, member := range knownMembers {
				if member.IsBot {
					continue
				}

				whitelisted, err := queries.ExistsTelegramGroupWhitelist(ctx, db.ExistsTelegramGroupWhitelistParams{
					ChatID:         chatID,
					TelegramUserID: member.TelegramUserID,
				})
				if err != nil || whitelisted {
					continue
				}

				tgIDStr := strconv.FormatInt(member.TelegramUserID, 10)
				acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
					Platform:   db.EnumPlatformTelegram,
					ExternalID: tgIDStr,
				})
				if err != nil {
					if _, ok := seen[member.TelegramUserID]; !ok {
						seen[member.TelegramUserID] = struct{}{}
						candidates = append(candidates, candidate{ID: member.TelegramUserID, Reason: defenderReasonUnregistered})
					}
					continue
				}

				if !group.DefenderRemoveBlocked {
					continue
				}

				profile, err := queries.GetMyProfile(ctx, acc.S21Login)
				if err != nil || !profile.Status.Valid {
					continue
				}
				if profile.Status.EnumStudentStatus == db.EnumStudentStatusBLOCKED || profile.Status.EnumStudentStatus == db.EnumStudentStatusEXPELLED {
					if _, ok := seen[member.TelegramUserID]; ok {
						continue
					}
					reason := defenderReasonBlocked
					if profile.Status.EnumStudentStatus == db.EnumStudentStatusEXPELLED {
						reason = defenderReasonExpelled
					}
					seen[member.TelegramUserID] = struct{}{}
					candidates = append(candidates, candidate{ID: member.TelegramUserID, Reason: reason})
				}
			}
		}

		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

		if len(candidates) == 0 {
			updates["defender_preview_candidate_ids"] = ""
			updates["defender_preview_summary_ru"] = "Нарушителей не найдено среди известных участников."
			updates["defender_preview_summary_en"] = "No violators found among known members."
			return "", updates, nil
		}

		ids := make([]string, 0, len(candidates))
		lines := make([]string, 0, len(candidates))
		for _, c := range candidates {
			ids = append(ids, strconv.FormatInt(c.ID, 10))
			display := strings.TrimSpace(c.Name)
			if display == "" {
				display = strconv.FormatInt(c.ID, 10)
			}
			uname := strings.TrimPrefix(strings.TrimSpace(c.Uname), "@")
			mentionURL := fmt.Sprintf("tg://user?id=%d", c.ID)
			if uname != "" {
				mentionURL = "https://t.me/" + uname
			}
			mention := fmt.Sprintf("[%s](%s)", escapeMarkdownLinkText(display), mentionURL)
			usernameSuffix := ""
			if uname != "" {
				usernameSuffix = " @" + escapeMarkdownPlain(uname)
			}
			lines = append(lines, fmt.Sprintf("%s%s `%d` - `%s`", mention, usernameSuffix, c.ID, escapeMarkdownV2(c.Reason)))
		}
		updates["defender_preview_candidate_ids"] = strings.Join(ids, ",")
		previewText := fmt.Sprintf("Найдено: %d\n%s", len(candidates), strings.Join(lines, "\n"))
		updates["defender_preview_summary_ru"] = previewText
		updates["defender_preview_summary_en"] = previewText

		return "", updates, nil
	})

	registry.Register("add_group_defender_preview_to_whitelist", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		addedByID, err := queries.GetUserAccountIDByExternalId(ctx, db.GetUserAccountIDByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Не удалось определить администратора для whitelist."
			updates["defender_preview_summary_en"] = "Failed to resolve admin account for whitelist operation."
			return "", updates, nil
		}

		rawIDs := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_candidate_ids"]))
		ids := splitCSVInt64(rawIDs)
		added := 0
		for _, id := range ids {
			if id <= 0 {
				continue
			}
			if _, err := queries.UpsertTelegramGroupWhitelist(ctx, db.UpsertTelegramGroupWhitelistParams{
				ChatID:           chatID,
				TelegramUserID:   id,
				AddedByAccountID: addedByID,
			}); err == nil {
				added++
			}
		}

		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_preview_candidate_ids"] = ""
		updates["defender_preview_summary_ru"] = fmt.Sprintf("Добавлено в whitelist: %d", added)
		updates["defender_preview_summary_en"] = fmt.Sprintf("Added to whitelist: %d", added)
		return "", updates, nil
	})

	registry.Register("add_group_defender_whitelist_from_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		tgIDRaw := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		tgID, err := strconv.ParseInt(tgIDRaw, 10, 64)
		if err != nil || tgID <= 0 {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Некорректный Telegram ID."
			updates["defender_preview_summary_en"] = "Invalid Telegram ID."
			return "", updates, nil
		}

		addedByID, err := queries.GetUserAccountIDByExternalId(ctx, db.GetUserAccountIDByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Не удалось определить администратора для whitelist."
			updates["defender_preview_summary_en"] = "Failed to resolve admin account for whitelist operation."
			return "", updates, nil
		}

		if _, err := queries.UpsertTelegramGroupWhitelist(ctx, db.UpsertTelegramGroupWhitelistParams{
			ChatID:           chatID,
			TelegramUserID:   tgID,
			AddedByAccountID: addedByID,
		}); err != nil {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Не удалось добавить ID в whitelist."
			updates["defender_preview_summary_en"] = "Failed to add ID to whitelist."
			return "", updates, nil
		}

		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_preview_summary_ru"] = fmt.Sprintf("ID `%d` добавлен в whitelist.", tgID)
		updates["defender_preview_summary_en"] = fmt.Sprintf("ID `%d` added to whitelist.", tgID)
		return "", updates, nil
	})

	registry.Register("remove_group_defender_whitelist", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		wlID, ok := parseWhitelistRemoveID(strings.TrimSpace(fmt.Sprintf("%v", payload["id"])))
		if ok {
			_, _ = queries.DeleteTelegramGroupWhitelistByOwner(ctx, db.DeleteTelegramGroupWhitelistByOwnerParams{
				ChatID:              chatID,
				TelegramUserID:      wlID,
				OwnerTelegramUserID: userID,
			})
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("load_group_defender_logs", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		return "", updates, nil
	})
}

type logger interface {
	Warn(msg string, args ...any)
	Debug(msg string, args ...any)
}

func buildDefenderContextUpdates(ctx context.Context, userID int64, payload map[string]any, log logger, queries db.Querier) map[string]any {
	updates := map[string]any{
		"can_manage_selected_group": false,
	}
	mergeDefenderDefaults(updates, payload)

	chatID, err := parseSelectedGroupChatID(payload)
	if err != nil {
		return updates
	}
	group, err := requireOwnedGroup(ctx, queries, userID, chatID)
	if err != nil {
		return updates
	}

	updates["can_manage_selected_group"] = true
	if strings.TrimSpace(fmt.Sprintf("%v", payload["selected_group_title"])) == "" {
		updates["selected_group_title"] = group.ChatTitle
	}
	maps.Copy(updates, buildDefenderSettingsUpdates(group))

	whitelistRows, err := queries.ListTelegramGroupWhitelists(ctx, db.ListTelegramGroupWhitelistsParams{
		ChatID:   chatID,
		RowLimit: defenderWhitelistLoadLimit,
	})
	if err != nil {
		if log != nil {
			log.Warn("admin groups: failed to load defender whitelist", "chat_id", chatID, "error", err)
		}
	} else {
		applyWhitelistRows(ctx, queries, userID, chatID, updates, whitelistRows)
	}

	logRows, err := queries.ListTelegramGroupLogs(ctx, db.ListTelegramGroupLogsParams{
		ChatID:   chatID,
		RowLimit: defenderLogsLoadLimit,
	})
	if err != nil {
		if log != nil {
			log.Warn("admin groups: failed to load defender logs", "chat_id", chatID, "error", err)
		}
	} else {
		updates["defender_logs_list_ru"] = formatDefenderLogs(logRows, fsm.LangRu)
		updates["defender_logs_list_en"] = formatDefenderLogs(logRows, fsm.LangEn)
	}

	return updates
}

func mergeDefenderDefaults(updates map[string]any, payload map[string]any) {
	updates["defender_enabled"] = false
	updates["defender_enabled_label_ru"] = "❌ Выключено"
	updates["defender_enabled_label_en"] = "❌ Disabled"
	updates["defender_remove_blocked"] = false
	updates["defender_remove_blocked_label_ru"] = "❌ Выключено"
	updates["defender_remove_blocked_label_en"] = "❌ Disabled"
	updates["defender_whitelist_count"] = 0
	updates["defender_whitelist_list_ru"] = "Пусто"
	updates["defender_whitelist_list_en"] = "Empty"
	updates["defender_logs_list_ru"] = "Лог пуст."
	updates["defender_logs_list_en"] = "Log is empty."

	runSummaryRU := "—"
	runSummaryEN := "—"
	runNoticeRU := ""
	runNoticeEN := ""
	if payload != nil {
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_summary_ru"])); v != "" {
			runSummaryRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_summary_en"])); v != "" {
			runSummaryEN = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_notice_ru"])); v != "" {
			runNoticeRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_notice_en"])); v != "" {
			runNoticeEN = v
		}
	}
	updates["defender_last_run_summary_ru"] = runSummaryRU
	updates["defender_last_run_summary_en"] = runSummaryEN
	updates["defender_last_run_notice_ru"] = runNoticeRU
	updates["defender_last_run_notice_en"] = runNoticeEN

	previewIDs := ""
	previewRU := "Нажми «Предпросмотр», чтобы увидеть потенциальных нарушителей среди известных участников."
	previewEN := "Tap preview to see potential violators among known members."
	if payload != nil {
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_candidate_ids"])); v != "" {
			previewIDs = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_summary_ru"])); v != "" {
			previewRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_summary_en"])); v != "" {
			previewEN = v
		}
	}
	updates["defender_preview_candidate_ids"] = previewIDs
	updates["defender_preview_summary_ru"] = previewRU
	updates["defender_preview_summary_en"] = previewEN

	resetDefenderWhitelistSlots(updates)
}

func buildDefenderSettingsUpdates(group db.TelegramGroup) map[string]any {
	enabledRU := "❌ Выключено"
	enabledEN := "❌ Disabled"
	if group.DefenderEnabled {
		enabledRU = "✅ Включено"
		enabledEN = "✅ Enabled"
	}

	blockedRU := "❌ Выключено"
	blockedEN := "❌ Disabled"
	if group.DefenderRemoveBlocked {
		blockedRU = "✅ Включено"
		blockedEN = "✅ Enabled"
	}

	return map[string]any{
		"defender_enabled":                 group.DefenderEnabled,
		"defender_enabled_label_ru":        enabledRU,
		"defender_enabled_label_en":        enabledEN,
		"defender_remove_blocked":          group.DefenderRemoveBlocked,
		"defender_remove_blocked_label_ru": blockedRU,
		"defender_remove_blocked_label_en": blockedEN,
	}
}

func resetDefenderWhitelistSlots(updates map[string]any) {
	for i := 1; i <= maxDefenderWhitelistButtons; i++ {
		updates[fmt.Sprintf("defender_whitelist_remove_button_id_%d", i)] = ""
		updates[fmt.Sprintf("defender_whitelist_remove_label_%d", i)] = ""
	}
}

func applyWhitelistRows(ctx context.Context, queries db.Querier, ownerTelegramUserID, chatID int64, updates map[string]any, rows []db.TelegramGroupWhitelist) {
	updates["defender_whitelist_count"] = len(rows)
	if len(rows) == 0 {
		updates["defender_whitelist_list_ru"] = "Пусто"
		updates["defender_whitelist_list_en"] = "Empty"
		return
	}

	runner, hasRunner := fsm.DefenderRunnerFromContext(ctx)

	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		display := strconv.FormatInt(row.TelegramUserID, 10)
		mentionURL := fmt.Sprintf("tg://user?id=%d", row.TelegramUserID)
		usernameSuffix := ""
		uname := ""

		if hasRunner {
			if resolvedDisplay, resolvedUsername, err := runner.ResolveGroupMemberIdentity(ctx, ownerTelegramUserID, chatID, row.TelegramUserID); err == nil {
				if v := strings.TrimSpace(resolvedDisplay); v != "" {
					display = v
				}
				uname = strings.TrimPrefix(strings.TrimSpace(resolvedUsername), "@")
			}
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: strconv.FormatInt(row.TelegramUserID, 10),
		})
		if err == nil {
			if uname == "" {
				uname = strings.TrimPrefix(strings.TrimSpace(acc.Username.String), "@")
			}
			if login := strings.TrimSpace(acc.S21Login); login != "" && display == strconv.FormatInt(row.TelegramUserID, 10) {
				display = login
			}
		}

		if uname != "" {
			mentionURL = "https://t.me/" + uname
			usernameSuffix = " @" + escapeMarkdownPlain(uname)
		}

		mention := fmt.Sprintf("[%s](%s)", escapeMarkdownLinkText(display), mentionURL)
		lines = append(lines, fmt.Sprintf("%d. %s%s `%d`", i+1, mention, usernameSuffix, row.TelegramUserID))
		if i < maxDefenderWhitelistButtons {
			updates[fmt.Sprintf("defender_whitelist_remove_button_id_%d", i+1)] = fmt.Sprintf("def_wl_rm_%d", row.TelegramUserID)
			updates[fmt.Sprintf("defender_whitelist_remove_label_%d", i+1)] = buildWhitelistRemoveLabel(display, row.TelegramUserID)
		}
	}
	updates["defender_whitelist_list_ru"] = strings.Join(lines, "\n")
	updates["defender_whitelist_list_en"] = updates["defender_whitelist_list_ru"]
}

func buildWhitelistRemoveLabel(display string, telegramUserID int64) string {
	name := strings.TrimSpace(display)
	if name == "" {
		name = strconv.FormatInt(telegramUserID, 10)
	}
	const maxNameRunes = 18
	r := []rune(name)
	if len(r) > maxNameRunes {
		name = string(r[:maxNameRunes-1]) + "…"
	}
	return fmt.Sprintf("❌ %s (%d)", name, telegramUserID)
}

func formatDefenderLogs(rows []db.TelegramGroupLog, language string) string {
	if len(rows) == 0 {
		if language == fsm.LangEn {
			return "Log is empty."
		}
		return "Лог пуст."
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		ts := "—"
		if row.CreatedAt.Valid {
			ts = row.CreatedAt.Time.In(time.Local).Format("02.01 15:04")
		}

		sourceLabel := defenderLogSourceLabel(row.Source, language)
		resultLabel := defenderLogResultLabel(row.Action, row.Reason, language)
		line := fmt.Sprintf("• %s · %s\n  👤 `%d` · %s", ts, sourceLabel, row.TelegramUserID, resultLabel)
		if strings.TrimSpace(row.Details) != "" {
			if language == fsm.LangEn {
				line += "\n  ℹ️ " + "Details: " + fsm.EscapeMarkdown(row.Details)
			} else {
				line += "\n  ℹ️ " + "Детали: " + fsm.EscapeMarkdown(row.Details)
			}
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func defenderLogSourceLabel(source, language string) string {
	switch source {
	case "auto_join":
		if language == fsm.LangEn {
			return "Auto-check on join"
		}
		return "Автопроверка при входе"
	case "manual_run":
		if language == fsm.LangEn {
			return "Manual run"
		}
		return "Ручной запуск"
	case "preview":
		if language == fsm.LangEn {
			return "Preview"
		}
		return "Предпросмотр"
	default:
		return fsm.EscapeMarkdown(source)
	}
}

func defenderLogResultLabel(action, reason, language string) string {
	switch action + "/" + reason {
	case "removed/unregistered":
		if language == fsm.LangEn {
			return "✅ Removed: not registered in bot"
		}
		return "✅ Удалён: не зарегистрирован в боте"
	case "removed/blocked":
		if language == fsm.LangEn {
			return "✅ Removed: student status BLOCKED"
		}
		return "✅ Удалён: статус студента BLOCKED"
	case "removed/expelled":
		if language == fsm.LangEn {
			return "✅ Removed: student status EXPELLED"
		}
		return "✅ Удалён: статус студента EXPELLED"
	case "skipped_whitelist/whitelist":
		if language == fsm.LangEn {
			return "⏭ Skipped: in whitelist"
		}
		return "⏭ Пропущен: в whitelist"
	case "skipped_no_rights/bot_rights":
		if language == fsm.LangEn {
			return "⚠️ Skipped: bot has no ban rights"
		}
		return "⚠️ Пропущен: у бота нет права бана"
	case "skipped_not_member/not_member":
		if language == fsm.LangEn {
			return "⏭ Skipped: user is not a group member"
		}
		return "⏭ Пропущен: пользователь уже не участник группы"
	default:
		return fsm.EscapeMarkdown(action) + "/" + fsm.EscapeMarkdown(reason)
	}
}

func parseWhitelistRemoveID(buttonID string) (int64, bool) {
	after, ok := strings.CutPrefix(buttonID, "def_wl_rm_")
	if !ok || strings.TrimSpace(after) == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(after, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func splitCSVInt64(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]int64, 0, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		result = append(result, n)
	}
	return result
}

func escapeMarkdownV2(v string) string {
	r := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return r.Replace(strings.TrimSpace(v))
}

func escapeMarkdownLinkText(v string) string {
	r := strings.NewReplacer(
		"[", "\\[",
		"]", "\\]",
	)
	return r.Replace(strings.TrimSpace(v))
}

func escapeMarkdownPlain(v string) string {
	r := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"`", "\\`",
	)
	return r.Replace(strings.TrimSpace(v))
}
