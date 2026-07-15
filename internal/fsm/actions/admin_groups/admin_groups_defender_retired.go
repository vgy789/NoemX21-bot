package admin_groups

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

var retiredDefenderActionNames = []string{
	"load_group_defender_campus_filter_options",
	"defender_campus_prev_page",
	"defender_campus_next_page",
	"set_group_defender_filter_campus",
	"load_group_defender_tribe_filter_options",
	"defender_tribe_prev_page",
	"defender_tribe_next_page",
	"set_group_defender_filter_tribe",
	"set_defender_cleanup_scope_unregistered",
	"set_defender_cleanup_scope_blocked",
	"set_defender_cleanup_scope_campus",
	"set_defender_cleanup_scope_tribe",
	"load_group_defender_cleanup_campus_options",
	"set_group_defender_cleanup_campus_target",
	"defender_cleanup_campus_prev_page",
	"defender_cleanup_campus_next_page",
	"load_group_defender_cleanup_tribe_options",
	"set_group_defender_cleanup_tribe_target",
	"defender_cleanup_tribe_prev_page",
	"defender_cleanup_tribe_next_page",
	"run_group_defender",
	"preview_group_defender_violations",
	"add_group_defender_preview_to_whitelist",
	"add_group_defender_whitelist_from_input",
	"remove_group_defender_whitelist",
	"load_group_defender_logs",
}

func registerSafeDefenderActions(registry *fsm.LogicRegistry, log logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}

	registry.Register("load_group_defender_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("set_group_defender_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) != "def_enable_off" {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["_alert"] = "Defender retired: включение фейс-контроля отключено. Можно только выключить старые флаги."
			return "", updates, nil
		}
		group, err := requireLegacyGroupManagerAccess(ctx, queries, userID, chatID)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: group.OwnerTelegramUserID,
			DefenderEnabled:     false,
		}); err != nil && log != nil {
			log.Warn("admin groups: failed to disable retired defender_enabled", "chat_id", chatID, "user_id", userID, "error", err)
		}
		if _, err := queries.UpdateTelegramGroupDefenderRemoveBlockedByOwner(ctx, db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
			ChatID:                chatID,
			OwnerTelegramUserID:   group.OwnerTelegramUserID,
			DefenderRemoveBlocked: false,
		}); err != nil && log != nil {
			log.Warn("admin groups: failed to disable retired defender_remove_blocked", "chat_id", chatID, "user_id", userID, "error", err)
		}
		if _, err := queries.UpdateTelegramGroupDefenderRecheckKnownMembersByOwner(ctx, db.UpdateTelegramGroupDefenderRecheckKnownMembersByOwnerParams{
			ChatID:                      chatID,
			OwnerTelegramUserID:         group.OwnerTelegramUserID,
			DefenderRecheckKnownMembers: false,
		}); err != nil && log != nil {
			log.Warn("admin groups: failed to disable retired defender_recheck_known_members", "chat_id", chatID, "user_id", userID, "error", err)
		}
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["_alert"] = "Фейс-контроль выключен. Автоматические удаления участников не выполняются."
		return "", updates, nil
	})

	registry.Register("set_group_defender_remove_blocked", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return disableSingleRetiredDefenderFlag(ctx, userID, payload, log, queries, "remove_blocked")
	})

	registry.Register("set_group_defender_recheck_known_members", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return disableSingleRetiredDefenderFlag(ctx, userID, payload, log, queries, "recheck_known")
	})

	for _, name := range retiredDefenderActionNames {
		registry.Register(name, func(context.Context, int64, map[string]any) (string, map[string]any, error) {
			return "", map[string]any{
				"can_manage_selected_group": false,
				"_alert":                    "Defender отключён: Group Manager переведён в legacy-режим без автоматических удалений.",
			}, nil
		})
	}
}

func disableSingleRetiredDefenderFlag(ctx context.Context, userID int64, payload map[string]any, log logger, queries db.Querier, flag string) (string, map[string]any, error) {
	chatID, err := parseSelectedGroupChatID(payload)
	if err != nil {
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	}
	group, err := requireLegacyGroupManagerAccess(ctx, queries, userID, chatID)
	if err != nil {
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	}

	switch flag {
	case "remove_blocked":
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) != "def_blocked_off" {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["_alert"] = "Defender retired: этот флаг можно только выключить."
			return "", updates, nil
		}
		if _, err := queries.UpdateTelegramGroupDefenderRemoveBlockedByOwner(ctx, db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
			ChatID:                chatID,
			OwnerTelegramUserID:   group.OwnerTelegramUserID,
			DefenderRemoveBlocked: false,
		}); err != nil && log != nil {
			log.Warn("admin groups: failed to disable retired defender_remove_blocked", "chat_id", chatID, "user_id", userID, "error", err)
		}
	case "recheck_known":
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) != "def_recheck_known_off" {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["_alert"] = "Defender retired: этот флаг можно только выключить."
			return "", updates, nil
		}
		if _, err := queries.UpdateTelegramGroupDefenderRecheckKnownMembersByOwner(ctx, db.UpdateTelegramGroupDefenderRecheckKnownMembersByOwnerParams{
			ChatID:                      chatID,
			OwnerTelegramUserID:         group.OwnerTelegramUserID,
			DefenderRecheckKnownMembers: false,
		}); err != nil && log != nil {
			log.Warn("admin groups: failed to disable retired defender_recheck_known_members", "chat_id", chatID, "user_id", userID, "error", err)
		}
	}

	updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
	updates["_alert"] = "Настройка выключена. Автоматические удаления участников не выполняются."
	return "", updates, nil
}
