package admin_groups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const maxManagedGroups = 10

const (
	memberTagFormatLogin      = "login"
	memberTagFormatLoginLevel = "login_level"
	alertMemberTagNoRightsRU  = "Нужно право на Member Tags. Включите для бота в админах группы разрешение на изменение плашек и повторите."
)

var errGroupAccessDenied = errors.New("group access denied")

// Register registers admin-related actions.
func Register(registry *fsm.LogicRegistry, cfg *config.Config, log *slog.Logger, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("ADMIN_GROUPS_MENU", "admin_groups.yaml/ADMIN_MENU")
	}
	if registry == nil || queries == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	registry.Register("load_admin_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"is_group_owner": false,
			"is_bot_owner":   false,
			"groups_count":   0,
		}
		resetGroupSlots(updates)

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err == nil && cfg != nil {
			updates["is_bot_owner"] = (acc.S21Login == cfg.Init.SchoolLogin)
		}

		groups, err := queries.ListTelegramGroupsByOwner(ctx, userID)
		if err != nil {
			log.Debug("admin groups: failed to load groups", "user_id", userID, "error", err)
			return "", updates, nil
		}

		updates["is_group_owner"] = len(groups) > 0
		updates["groups_count"] = len(groups)

		limit := min(len(groups), maxManagedGroups)
		for i := range limit {
			slot := i + 1
			g := groups[i]
			groupID := fmt.Sprintf("%d", g.ChatID)
			updates[fmt.Sprintf("group_chat_id_%d", slot)] = groupID
			updates[fmt.Sprintf("group_button_id_%d", slot)] = fmt.Sprintf("group_%s", groupID)

			title := strings.TrimSpace(g.ChatTitle)
			if title == "" {
				title = fmt.Sprintf("Group %s", groupID)
			}
			updates[fmt.Sprintf("group_title_%d", slot)] = title
		}

		return "", updates, nil
	})

	registry.Register("select_admin_group", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		inputID := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		updates := map[string]any{
			"selected_group_chat_id": "",
			"selected_group_title":   "",
		}

		for i := 1; i <= maxManagedGroups; i++ {
			btnID := strings.TrimSpace(fmt.Sprintf("%v", payload[fmt.Sprintf("group_button_id_%d", i)]))
			chatID := strings.TrimSpace(fmt.Sprintf("%v", payload[fmt.Sprintf("group_chat_id_%d", i)]))
			title := strings.TrimSpace(fmt.Sprintf("%v", payload[fmt.Sprintf("group_title_%d", i)]))
			if btnID == "" || chatID == "" {
				continue
			}
			if btnID == inputID {
				updates["selected_group_chat_id"] = chatID
				updates["selected_group_title"] = title
				mergeRunSummaryDefaults(updates, nil)
				return "", updates, nil
			}
		}

		if after, ok := strings.CutPrefix(inputID, "group_"); ok {
			chatID := strings.TrimSpace(after)
			if chatID != "" {
				updates["selected_group_chat_id"] = chatID
				updates["selected_group_title"] = chatID
			}
		}
		mergeRunSummaryDefaults(updates, nil)

		return "", updates, nil
	})

	registry.Register("load_group_member_tags_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"can_manage_selected_group": false,
		}
		mergeMemberTagSettingsDefaults(updates)
		mergeRunSummaryDefaults(updates, payload)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}

		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			if !errors.Is(err, errGroupAccessDenied) {
				log.Warn("admin groups: failed to load selected group", "chat_id", chatID, "user_id", userID, "error", err)
			}
			return "", updates, nil
		}

		updates["can_manage_selected_group"] = true
		if strings.TrimSpace(fmt.Sprintf("%v", payload["selected_group_title"])) == "" {
			updates["selected_group_title"] = group.ChatTitle
		}
		settingsUpdates := buildMemberTagSettingsUpdates(group)
		maps.Copy(updates, settingsUpdates)

		return "", updates, nil
	})

	registry.Register("set_group_member_tags_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{}
		mergeRunSummaryDefaults(updates, payload)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}

		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "tags_enable_on"
		updatedRows, err := queries.UpdateTelegramGroupMemberTagsEnabledByOwner(ctx, db.UpdateTelegramGroupMemberTagsEnabledByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
			MemberTagsEnabled:   enable,
		})
		if err != nil {
			log.Warn("admin groups: failed to update member_tags_enabled", "chat_id", chatID, "user_id", userID, "error", err)
			return "", updates, nil
		}
		if updatedRows == 0 {
			return "", updates, nil
		}

		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			return "", updates, nil
		}
		maps.Copy(updates, buildMemberTagSettingsUpdates(group))

		return "", updates, nil
	})

	registry.Register("set_group_member_tag_format", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{}
		mergeRunSummaryDefaults(updates, payload)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}

		format := memberTagFormatLogin
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "tags_fmt_login_level" {
			format = memberTagFormatLoginLevel
		}

		updatedRows, err := queries.UpdateTelegramGroupMemberTagFormatByOwner(ctx, db.UpdateTelegramGroupMemberTagFormatByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
			MemberTagFormat:     format,
		})
		if err != nil {
			log.Warn("admin groups: failed to update member_tag_format", "chat_id", chatID, "user_id", userID, "error", err)
			return "", updates, nil
		}
		if updatedRows == 0 {
			return "", updates, nil
		}

		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			return "", updates, nil
		}
		maps.Copy(updates, buildMemberTagSettingsUpdates(group))

		return "", updates, nil
	})

	registry.Register("run_group_member_tags", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{}

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			mergeRunSummaryDefaults(updates, payload)
			return "", updates, nil
		}

		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			mergeRunSummaryDefaults(updates, payload)
			return "", updates, nil
		}

		runner, ok := fsm.MemberTagRunnerFromContext(ctx)
		if !ok {
			updates["member_tags_last_run_summary_ru"] = "Недоступно: transport runner не подключён."
			updates["member_tags_last_run_summary_en"] = "Unavailable: transport runner is not configured."
			return "", updates, nil
		}

		mode := fsm.MemberTagRunModeKeepExisting
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "tags_run_clear" {
			mode = fsm.MemberTagRunModeClearAndApply
		}

		result, err := runner.RunGroupMemberTags(ctx, userID, chatID, mode)
		if err != nil {
			updates["member_tags_last_run_summary_ru"] = fmt.Sprintf("Ошибка запуска: %v", err)
			updates["member_tags_last_run_summary_en"] = fmt.Sprintf("Run failed: %v", err)
		} else {
			updates["member_tags_last_run_summary_ru"] = fmt.Sprintf(
				"updated=%d, skip_existing=%d, skip_unregistered=%d, skip_not_member=%d, skip_no_rights=%d, errors=%d",
				result.Updated, result.SkippedExisting, result.SkippedUnregistered, result.SkippedNotMember, result.SkippedNoRights, result.Errors,
			)
			updates["member_tags_last_run_summary_en"] = updates["member_tags_last_run_summary_ru"]
			if result.SkippedNoRights > 0 {
				updates["_alert"] = alertMemberTagNoRightsRU
			}
		}

		return "", updates, nil
	})

	registerDefenderActions(registry, log, queries)
	registerPRRActions(registry, log, queries)
}

func resetGroupSlots(updates map[string]any) {
	for i := 1; i <= maxManagedGroups; i++ {
		updates[fmt.Sprintf("group_chat_id_%d", i)] = ""
		updates[fmt.Sprintf("group_button_id_%d", i)] = ""
		updates[fmt.Sprintf("group_title_%d", i)] = ""
	}
}

func parseSelectedGroupChatID(payload map[string]any) (int64, error) {
	raw := strings.TrimSpace(fmt.Sprintf("%v", payload["selected_group_chat_id"]))
	if raw == "" {
		return 0, fmt.Errorf("selected_group_chat_id is empty")
	}
	chatID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid selected_group_chat_id: %w", err)
	}
	return chatID, nil
}

func requireOwnedGroup(ctx context.Context, queries db.Querier, userID, chatID int64) (db.TelegramGroup, error) {
	group, err := queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return db.TelegramGroup{}, err
	}
	if group.OwnerTelegramUserID != userID || !group.IsActive || !group.IsInitialized {
		return db.TelegramGroup{}, errGroupAccessDenied
	}
	return group, nil
}

func mergeMemberTagSettingsDefaults(updates map[string]any) {
	updates["member_tags_enabled"] = false
	updates["member_tags_enabled_label_ru"] = "❌ Выключено"
	updates["member_tags_enabled_label_en"] = "❌ Disabled"
	updates["member_tags_toggle_button_label_ru"] = "⚙️ Автотегирование: ⬜"
	updates["member_tags_toggle_button_label_en"] = "⚙️ Auto-tagging: ⬜"
	updates["member_tag_format"] = memberTagFormatLogin
	updates["member_tag_format_label_ru"] = "login"
	updates["member_tag_format_label_en"] = "login"
	updates["member_tag_login_option_label_ru"] = "✅ login"
	updates["member_tag_login_option_label_en"] = "✅ login"
	updates["member_tag_login_level_option_label_ru"] = "⬜ login [lvl]"
	updates["member_tag_login_level_option_label_en"] = "⬜ login [lvl]"
}

func mergeRunSummaryDefaults(updates map[string]any, payload map[string]any) {
	ru := "—"
	en := "—"
	if payload != nil {
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["member_tags_last_run_summary_ru"])); v != "" {
			ru = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["member_tags_last_run_summary_en"])); v != "" {
			en = v
		}
	}
	updates["member_tags_last_run_summary_ru"] = ru
	updates["member_tags_last_run_summary_en"] = en
}

func buildMemberTagSettingsUpdates(group db.TelegramGroup) map[string]any {
	format := strings.TrimSpace(group.MemberTagFormat)
	if format != memberTagFormatLoginLevel {
		format = memberTagFormatLogin
	}

	formatLabel := "login"
	if format == memberTagFormatLoginLevel {
		formatLabel = "login [lvl]"
	}

	enabledLabelRU := "❌ Выключено"
	enabledLabelEN := "❌ Disabled"
	toggleLabelRU := "⚙️ Автотегирование: ⬜"
	toggleLabelEN := "⚙️ Auto-tagging: ⬜"
	if group.MemberTagsEnabled {
		enabledLabelRU = "✅ Включено"
		enabledLabelEN = "✅ Enabled"
		toggleLabelRU = "⚙️ Автотегирование: ✅"
		toggleLabelEN = "⚙️ Auto-tagging: ✅"
	}

	loginOptionRU := "✅ login"
	loginOptionEN := "✅ login"
	loginLvlOptionRU := "⬜ login [lvl]"
	loginLvlOptionEN := "⬜ login [lvl]"
	if format == memberTagFormatLoginLevel {
		loginOptionRU = "⬜ login"
		loginOptionEN = "⬜ login"
		loginLvlOptionRU = "✅ login [lvl]"
		loginLvlOptionEN = "✅ login [lvl]"
	}

	return map[string]any{
		"member_tags_enabled":                    group.MemberTagsEnabled,
		"member_tags_enabled_label_ru":           enabledLabelRU,
		"member_tags_enabled_label_en":           enabledLabelEN,
		"member_tags_toggle_button_label_ru":     toggleLabelRU,
		"member_tags_toggle_button_label_en":     toggleLabelEN,
		"member_tag_format":                      format,
		"member_tag_format_label_ru":             formatLabel,
		"member_tag_format_label_en":             formatLabel,
		"member_tag_login_option_label_ru":       loginOptionRU,
		"member_tag_login_option_label_en":       loginOptionEN,
		"member_tag_login_level_option_label_ru": loginLvlOptionRU,
		"member_tag_login_level_option_label_en": loginLvlOptionEN,
	}
}
