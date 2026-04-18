package admin_groups

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	maxGroupModeratorButtons  = 10
	targetModeratorStateKey   = "target_moderator_user_id"
	moderatorSelectButtonPref = "grp_mod_sel_"
)

type groupModeratorsStore interface {
	ListTelegramGroupModerators(ctx context.Context, chatID int64) ([]db.TelegramGroupModerator, error)
	UpsertTelegramGroupModerator(ctx context.Context, arg db.UpsertTelegramGroupModeratorParams) (db.TelegramGroupModerator, error)
	GetTelegramGroupModeratorByChatAndUser(ctx context.Context, chatID, telegramUserID int64) (db.TelegramGroupModerator, error)
	UpdateTelegramGroupModeratorPermissions(ctx context.Context, arg db.UpdateTelegramGroupModeratorPermissionsParams) (db.TelegramGroupModerator, error)
	DeleteTelegramGroupModeratorByChatAndUser(ctx context.Context, chatID, telegramUserID int64) error
}

type groupModerationCommandsStore interface {
	GetTelegramGroupModerationCommandsEnabledByChatID(ctx context.Context, chatID int64) (bool, error)
	UpdateTelegramGroupModerationCommandsEnabled(ctx context.Context, arg db.UpdateTelegramGroupModerationCommandsEnabledParams) (int64, error)
}

type telegramUserAccountByS21LoginResolver interface {
	GetTelegramUserAccountByS21Login(ctx context.Context, s21Login string) (db.UserAccount, error)
}

func registerModeratorActions(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	registry.Register("load_group_moderators_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"can_manage_selected_group": false,
		}
		mergeModeratorsDefaults(updates)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			return "", updates, nil
		}
		updates["can_manage_selected_group"] = true
		if strings.TrimSpace(fmt.Sprintf("%v", payload["selected_group_title"])) == "" {
			updates["selected_group_title"] = group.ChatTitle
		}
		if store, ok := queries.(groupModerationCommandsStore); ok {
			enabled, err := store.GetTelegramGroupModerationCommandsEnabledByChatID(ctx, chatID)
			if err != nil {
				log.Warn("admin groups: failed to load moderation_commands_enabled", "chat_id", chatID, "user_id", userID, "error", err)
			} else {
				applyModerationCommandsEnabled(updates, enabled)
			}
		}

		store, ok := queries.(groupModeratorsStore)
		if !ok {
			updates["moderators_list_formatted_ru"] = "Недоступно: хранилище модераторов не подключено"
			updates["moderators_list_formatted_en"] = "Unavailable: moderators storage is not configured"
			return "", updates, nil
		}

		rows, err := store.ListTelegramGroupModerators(ctx, chatID)
		if err != nil {
			log.Warn("admin groups: failed to load moderators", "chat_id", chatID, "user_id", userID, "error", err)
			updates["moderators_list_formatted_ru"] = "Не удалось загрузить список модераторов"
			updates["moderators_list_formatted_en"] = "Failed to load moderators list"
			return "", updates, nil
		}

		applyModeratorsRows(ctx, queries, updates, rows)
		return "", updates, nil
	})

	registry.Register("set_group_moderation_commands_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", map[string]any{}, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", map[string]any{}, nil
		}

		store, ok := queries.(groupModerationCommandsStore)
		if !ok {
			return "", map[string]any{
				"_alert": "Недоступно: переключатель команд модерации не подключён",
			}, nil
		}

		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "mod_cmds_enable"
		rows, err := store.UpdateTelegramGroupModerationCommandsEnabled(ctx, db.UpdateTelegramGroupModerationCommandsEnabledParams{
			ChatID:                    chatID,
			ModerationCommandsEnabled: enable,
		})
		if err != nil {
			log.Warn("admin groups: failed to update moderation_commands_enabled", "chat_id", chatID, "user_id", userID, "enabled", enable, "error", err)
			return "", map[string]any{
				"_alert": "Не удалось обновить переключатель команд модерации",
			}, nil
		}
		if rows == 0 {
			return "", map[string]any{}, nil
		}

		alert := "Команды модерации включены"
		if !enable {
			alert = "Команды модерации выключены"
		}
		return "", map[string]any{
			"_alert": alert,
		}, nil
	})

	registry.Register("add_group_moderator_from_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", map[string]any{}, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", map[string]any{}, nil
		}

		store, ok := queries.(groupModeratorsStore)
		if !ok {
			return "GROUP_MODERATORS_ADD_INPUT", map[string]any{
				"_alert": "Недоступно: хранилище модераторов не подключено",
			}, nil
		}

		input := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		account, tgID, errRU, _ := resolveModeratorTargetAccount(ctx, queries, input)
		if errRU != "" {
			return "GROUP_MODERATORS_ADD_INPUT", map[string]any{
				"_alert": errRU,
			}, nil
		}

		addedByAccountID, err := queries.GetUserAccountIDByExternalId(ctx, db.GetUserAccountIDByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil || addedByAccountID <= 0 {
			return "GROUP_MODERATORS_ADD_INPUT", map[string]any{
				"_alert": "Не удалось определить аккаунт администратора для добавления модератора",
			}, nil
		}

		_, err = store.UpsertTelegramGroupModerator(ctx, db.UpsertTelegramGroupModeratorParams{
			ChatID:           chatID,
			TelegramUserID:   tgID,
			CanBan:           false,
			CanMute:          false,
			FullAccess:       false,
			AddedByAccountID: addedByAccountID,
		})
		if err != nil {
			log.Warn("admin groups: failed to upsert moderator", "chat_id", chatID, "target_tg_id", tgID, "error", err)
			return "GROUP_MODERATORS_ADD_INPUT", map[string]any{
				"_alert": "Не удалось добавить модератора",
			}, nil
		}

		display := strings.TrimSpace(account.S21Login)
		if display == "" {
			display = fmt.Sprintf("%d", tgID)
		}

		return "", map[string]any{
			targetModeratorStateKey: fmt.Sprintf("%d", tgID),
			"_alert":                fmt.Sprintf("Модератор %s добавлен", display),
		}, nil
	})

	registry.Register("select_group_moderator", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		idRaw := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		targetID, ok := parseModeratorSelectButtonID(idRaw)
		if !ok {
			return "", map[string]any{}, nil
		}
		return "", map[string]any{
			targetModeratorStateKey: fmt.Sprintf("%d", targetID),
		}, nil
	})

	registry.Register("load_target_moderator_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := map[string]any{
			"can_manage_selected_group": false,
		}
		mergeTargetModeratorDefaults(updates)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			return "", updates, nil
		}
		updates["can_manage_selected_group"] = true
		if strings.TrimSpace(fmt.Sprintf("%v", payload["selected_group_title"])) == "" {
			updates["selected_group_title"] = group.ChatTitle
		}

		store, ok := queries.(groupModeratorsStore)
		if !ok {
			updates["_alert"] = "Недоступно: хранилище модераторов не подключено"
			return "", updates, nil
		}

		targetID, ok := parseTargetModeratorID(payload)
		if !ok {
			updates["_alert"] = "Сначала выбери модератора из списка"
			return "GROUP_MODERATORS_MENU", updates, nil
		}

		modRow, err := store.GetTelegramGroupModeratorByChatAndUser(ctx, chatID, targetID)
		if err != nil {
			updates["_alert"] = "Модератор не найден в списке"
			return "GROUP_MODERATORS_MENU", updates, nil
		}

		display, username := resolveModeratorDisplay(ctx, queries, modRow.TelegramUserID)
		applyTargetModerator(updates, modRow, display, username)
		return "", updates, nil
	})

	registry.Register("toggle_moderator_permission", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", map[string]any{}, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", map[string]any{}, nil
		}

		store, ok := queries.(groupModeratorsStore)
		if !ok {
			return "", map[string]any{
				"_alert": "Недоступно: хранилище модераторов не подключено",
			}, nil
		}

		targetID, ok := parseTargetModeratorID(payload)
		if !ok {
			return "GROUP_MODERATORS_MENU", map[string]any{
				"_alert": "Сначала выбери модератора из списка",
			}, nil
		}

		modRow, err := store.GetTelegramGroupModeratorByChatAndUser(ctx, chatID, targetID)
		if err != nil {
			return "GROUP_MODERATORS_MENU", map[string]any{
				"_alert": "Модератор не найден",
			}, nil
		}

		switch strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) {
		case "toggle_perm_ban":
			modRow.CanBan = !modRow.CanBan
		case "toggle_perm_mute":
			modRow.CanMute = !modRow.CanMute
		case "toggle_perm_full":
			modRow.FullAccess = !modRow.FullAccess
		default:
			return "", map[string]any{}, nil
		}

		modRow, err = store.UpdateTelegramGroupModeratorPermissions(ctx, db.UpdateTelegramGroupModeratorPermissionsParams{
			ChatID:         chatID,
			TelegramUserID: targetID,
			CanBan:         modRow.CanBan,
			CanMute:        modRow.CanMute,
			FullAccess:     modRow.FullAccess,
		})
		if err != nil {
			return "", map[string]any{
				"_alert": "Не удалось обновить права модератора",
			}, nil
		}

		updates := map[string]any{
			targetModeratorStateKey: fmt.Sprintf("%d", targetID),
		}
		display, username := resolveModeratorDisplay(ctx, queries, modRow.TelegramUserID)
		applyTargetModerator(updates, modRow, display, username)
		return "", updates, nil
	})

	registry.Register("remove_group_moderator", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", map[string]any{}, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", map[string]any{}, nil
		}

		store, ok := queries.(groupModeratorsStore)
		if !ok {
			return "", map[string]any{
				"_alert": "Недоступно: хранилище модераторов не подключено",
			}, nil
		}

		targetID, ok := parseTargetModeratorID(payload)
		if !ok {
			return "GROUP_MODERATORS_MENU", map[string]any{
				"_alert": "Сначала выбери модератора из списка",
			}, nil
		}

		if err := store.DeleteTelegramGroupModeratorByChatAndUser(ctx, chatID, targetID); err != nil {
			log.Warn("admin groups: failed to remove moderator", "chat_id", chatID, "target_tg_id", targetID, "error", err)
			return "GROUP_MODERATORS_MENU", map[string]any{
				"_alert": "Не удалось удалить модератора",
			}, nil
		}

		return "", map[string]any{
			targetModeratorStateKey: "",
			"_alert":                "Модератор удалён",
		}, nil
	})
}

func mergeModeratorsDefaults(updates map[string]any) {
	updates["moderators_list_formatted_ru"] = "Пусто"
	updates["moderators_list_formatted_en"] = "Empty"
	applyModerationCommandsEnabled(updates, true)
	for i := 1; i <= maxGroupModeratorButtons; i++ {
		updates[fmt.Sprintf("moderator_button_id_%d", i)] = ""
		updates[fmt.Sprintf("moderator_button_label_%d", i)] = ""
	}
}

func applyModerationCommandsEnabled(updates map[string]any, enabled bool) {
	updates["group_moderation_commands_enabled"] = enabled
	updates["group_moderation_commands_enabled_label_ru"] = "❌ Выключены"
	updates["group_moderation_commands_enabled_label_en"] = "❌ Disabled"
	if enabled {
		updates["group_moderation_commands_enabled_label_ru"] = "✅ Включены"
		updates["group_moderation_commands_enabled_label_en"] = "✅ Enabled"
	}
}

func mergeTargetModeratorDefaults(updates map[string]any) {
	updates["target_mod_name"] = "—"
	updates["mod_can_ban_label_ru"] = "нет"
	updates["mod_can_ban_label_en"] = "no"
	updates["mod_can_mute_label_ru"] = "нет"
	updates["mod_can_mute_label_en"] = "no"
	updates["mod_full_access_label_ru"] = "нет"
	updates["mod_full_access_label_en"] = "no"
	updates["mod_can_ban_status_ru"] = "⬜"
	updates["mod_can_ban_status_en"] = "⬜"
	updates["mod_can_mute_status_ru"] = "⬜"
	updates["mod_can_mute_status_en"] = "⬜"
	updates["mod_full_access_status_ru"] = "⬜"
	updates["mod_full_access_status_en"] = "⬜"
}

func applyModeratorsRows(ctx context.Context, queries db.Querier, updates map[string]any, rows []db.TelegramGroupModerator) {
	if len(rows) == 0 {
		updates["moderators_list_formatted_ru"] = "Пусто"
		updates["moderators_list_formatted_en"] = "Empty"
		return
	}

	linesRU := make([]string, 0, len(rows))
	linesEN := make([]string, 0, len(rows))
	for i, row := range rows {
		display, username := resolveModeratorDisplay(ctx, queries, row.TelegramUserID)
		mentionURL := fmt.Sprintf("tg://openmessage?user_id=%d", row.TelegramUserID)
		mention := fmt.Sprintf("[%s](%s)", escapeMarkdownLinkText(display), mentionURL)
		usernameSuffix := ""
		if username != "" {
			usernameSuffix = " @" + escapeMarkdownPlain(username)
		}

		banMark := boolMark(row.CanBan)
		muteMark := boolMark(row.CanMute)
		fullMark := boolMark(row.FullAccess)
		linesRU = append(linesRU, fmt.Sprintf("%d. %s%s · бан=%s мут=%s full=%s", i+1, mention, usernameSuffix, banMark, muteMark, fullMark))
		linesEN = append(linesEN, fmt.Sprintf("%d. %s%s · ban=%s mute=%s full=%s", i+1, mention, usernameSuffix, banMark, muteMark, fullMark))

		if i < maxGroupModeratorButtons {
			updates[fmt.Sprintf("moderator_button_id_%d", i+1)] = fmt.Sprintf("%s%d", moderatorSelectButtonPref, row.TelegramUserID)
			updates[fmt.Sprintf("moderator_button_label_%d", i+1)] = buildModeratorButtonLabel(display, row.TelegramUserID)
		}
	}
	updates["moderators_list_formatted_ru"] = strings.Join(linesRU, "\n")
	updates["moderators_list_formatted_en"] = strings.Join(linesEN, "\n")
}

func applyTargetModerator(updates map[string]any, row db.TelegramGroupModerator, display, username string) {
	name := display
	if username != "" {
		name = fmt.Sprintf("%s (@%s)", display, username)
	}
	updates[targetModeratorStateKey] = fmt.Sprintf("%d", row.TelegramUserID)
	updates["target_mod_name"] = escapeMarkdownPlain(name)

	updates["mod_can_ban_label_ru"] = boolYesNoRU(row.CanBan)
	updates["mod_can_ban_label_en"] = boolYesNoEN(row.CanBan)
	updates["mod_can_mute_label_ru"] = boolYesNoRU(row.CanMute)
	updates["mod_can_mute_label_en"] = boolYesNoEN(row.CanMute)
	updates["mod_full_access_label_ru"] = boolYesNoRU(row.FullAccess)
	updates["mod_full_access_label_en"] = boolYesNoEN(row.FullAccess)

	updates["mod_can_ban_status_ru"] = boolMark(row.CanBan)
	updates["mod_can_ban_status_en"] = boolMark(row.CanBan)
	updates["mod_can_mute_status_ru"] = boolMark(row.CanMute)
	updates["mod_can_mute_status_en"] = boolMark(row.CanMute)
	updates["mod_full_access_status_ru"] = boolMark(row.FullAccess)
	updates["mod_full_access_status_en"] = boolMark(row.FullAccess)
}

func resolveModeratorDisplay(ctx context.Context, queries db.Querier, telegramUserID int64) (string, string) {
	display := strconv.FormatInt(telegramUserID, 10)
	username := ""

	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: strconv.FormatInt(telegramUserID, 10),
	})
	if err == nil {
		if login := strings.TrimSpace(acc.S21Login); login != "" {
			display = login
		}
		username = strings.TrimPrefix(strings.TrimSpace(acc.Username.String), "@")
	}
	return display, username
}

func resolveModeratorTargetAccount(ctx context.Context, queries db.Querier, raw string) (db.UserAccount, int64, string, string) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return db.UserAccount{}, 0, "Укажи Telegram ID, школьный логин или @username", "Provide Telegram ID, school login or @username"
	}

	if tgID, err := strconv.ParseInt(input, 10, 64); err == nil && tgID > 0 {
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: strconv.FormatInt(tgID, 10),
		})
		if err != nil {
			return db.UserAccount{}, 0, "Пользователь с таким Telegram ID не зарегистрирован в боте", "User with this Telegram ID is not registered"
		}
		return acc, tgID, "", ""
	}

	if strings.HasPrefix(input, "@") {
		username, ok := normalizeTelegramUsernameInput(input)
		if !ok {
			return db.UserAccount{}, 0, "Неверный @username, используй формат @username", "Invalid @username format"
		}
		resolver, ok := queries.(telegramUserAccountByUsernameResolver)
		if !ok {
			return db.UserAccount{}, 0, "Не удалось разрешить @username в Telegram ID", "Failed to resolve @username to Telegram ID"
		}
		acc, err := resolver.GetTelegramUserAccountByUsername(ctx, username)
		if err != nil {
			return db.UserAccount{}, 0, fmt.Sprintf("Не удалось найти пользователя @%s", escapeMarkdownPlain(username)), fmt.Sprintf("Could not find @%s", escapeMarkdownPlain(username))
		}
		tgID, err := strconv.ParseInt(strings.TrimSpace(acc.ExternalID), 10, 64)
		if err != nil || tgID <= 0 {
			return db.UserAccount{}, 0, fmt.Sprintf("Для @%s не найден Telegram ID", escapeMarkdownPlain(username)), fmt.Sprintf("No Telegram ID found for @%s", escapeMarkdownPlain(username))
		}
		return acc, tgID, "", ""
	}

	if resolver, ok := queries.(telegramUserAccountByS21LoginResolver); ok {
		acc, err := resolver.GetTelegramUserAccountByS21Login(ctx, input)
		if err == nil {
			tgID, convErr := strconv.ParseInt(strings.TrimSpace(acc.ExternalID), 10, 64)
			if convErr == nil && tgID > 0 {
				return acc, tgID, "", ""
			}
		}
	}

	acc, err := queries.GetUserAccountByS21Login(ctx, input)
	if err != nil {
		return db.UserAccount{}, 0, fmt.Sprintf("Не удалось найти пользователя по нику %s", escapeMarkdownPlain(input)), fmt.Sprintf("Could not resolve login %s", escapeMarkdownPlain(input))
	}
	if acc.Platform != db.EnumPlatformTelegram {
		return db.UserAccount{}, 0, fmt.Sprintf("Пользователь %s не привязал Telegram", escapeMarkdownPlain(input)), fmt.Sprintf("User %s has no Telegram link", escapeMarkdownPlain(input))
	}

	tgID, err := strconv.ParseInt(strings.TrimSpace(acc.ExternalID), 10, 64)
	if err != nil || tgID <= 0 {
		return db.UserAccount{}, 0, fmt.Sprintf("Для ника %s не найден Telegram ID", escapeMarkdownPlain(input)), fmt.Sprintf("No Telegram ID found for login %s", escapeMarkdownPlain(input))
	}
	return acc, tgID, "", ""
}

func parseModeratorSelectButtonID(buttonID string) (int64, bool) {
	after, ok := strings.CutPrefix(strings.TrimSpace(buttonID), moderatorSelectButtonPref)
	if !ok || strings.TrimSpace(after) == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(after, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func parseTargetModeratorID(payload map[string]any) (int64, bool) {
	raw := strings.TrimSpace(fmt.Sprintf("%v", payload[targetModeratorStateKey]))
	if raw == "" || raw == "<nil>" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func boolMark(v bool) string {
	if v {
		return "✅"
	}
	return "⬜"
}

func boolYesNoRU(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

func boolYesNoEN(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func buildModeratorButtonLabel(display string, telegramUserID int64) string {
	name := strings.TrimSpace(display)
	if name == "" {
		name = strconv.FormatInt(telegramUserID, 10)
	}
	const maxRunes = 16
	r := []rune(name)
	if len(r) > maxRunes {
		name = string(r[:maxRunes-1]) + "…"
	}
	return fmt.Sprintf("⚙️ %s (%d)", name, telegramUserID)
}
