package admin_groups

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func registerWelcomeActions(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	registry.Register("load_group_welcome_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", loadGroupWelcomeContext(ctx, queries, userID, payload), nil
	})

	registry.Register("set_group_welcome_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "welcome_enable_on"
		if _, err := queries.UpdateTelegramGroupWelcomeEnabledByOwner(ctx, db.UpdateTelegramGroupWelcomeEnabledByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
			WelcomeEnabled:      enable,
		}); err != nil {
			log.Warn("admin groups: failed to update welcome_enabled", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupWelcomeContext(ctx, queries, userID, payload), nil
	})

	registry.Register("set_group_welcome_delete_service_messages", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "welcome_delete_service_on"
		if _, err := queries.UpdateTelegramGroupWelcomeDeleteServiceMessagesByOwner(ctx, db.UpdateTelegramGroupWelcomeDeleteServiceMessagesByOwnerParams{
			ChatID:                       chatID,
			OwnerTelegramUserID:          userID,
			WelcomeDeleteServiceMessages: enable,
		}); err != nil {
			log.Warn("admin groups: failed to update welcome_delete_service_messages", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupWelcomeContext(ctx, queries, userID, payload), nil
	})

	registry.Register("set_group_welcome_destination_general", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		if _, err := queries.UpdateTelegramGroupWelcomeDestinationByOwner(ctx, db.UpdateTelegramGroupWelcomeDestinationByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
			WelcomeThreadID:     0,
			WelcomeThreadLabel:  "Общий чат",
		}); err != nil {
			log.Warn("admin groups: failed to reset welcome destination", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupWelcomeContext(ctx, queries, userID, payload), nil
	})
}

func loadGroupWelcomeContext(ctx context.Context, queries db.Querier, userID int64, payload map[string]any) map[string]any {
	updates := map[string]any{
		"can_manage_selected_group":                false,
		"welcome_enabled":                          false,
		"welcome_enabled_label_ru":                 "❌ Выключено",
		"welcome_enabled_label_en":                 "❌ Disabled",
		"welcome_destination_label":                "Общий чат",
		"welcome_destination_label_en":             "General chat",
		"welcome_delete_service_messages":          true,
		"welcome_delete_service_messages_label_ru": "✅ Включено",
		"welcome_delete_service_messages_label_en": "✅ Enabled",
	}

	chatID, err := parseSelectedGroupChatID(payload)
	if err != nil {
		return updates
	}

	group, err := requireOwnedGroup(ctx, queries, userID, chatID)
	if err != nil {
		return updates
	}
	updates["can_manage_selected_group"] = true

	if group.WelcomeEnabled {
		updates["welcome_enabled"] = true
		updates["welcome_enabled_label_ru"] = "✅ Включено"
		updates["welcome_enabled_label_en"] = "✅ Enabled"
	}

	destinationRU := strings.TrimSpace(group.WelcomeThreadLabel)
	if destinationRU == "" {
		destinationRU = "Общий чат"
	}
	destinationEN := destinationRU
	if group.WelcomeThreadID == 0 {
		destinationEN = "General chat"
	}
	updates["welcome_destination_label"] = destinationRU
	updates["welcome_destination_label_en"] = destinationEN

	updates["welcome_delete_service_messages"] = group.WelcomeDeleteServiceMessages
	if !group.WelcomeDeleteServiceMessages {
		updates["welcome_delete_service_messages_label_ru"] = "❌ Выключено"
		updates["welcome_delete_service_messages_label_en"] = "❌ Disabled"
	}

	return updates
}
