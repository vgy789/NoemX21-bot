package admin_groups

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const maxManagedGroups = 10

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

		return "", updates, nil
	})
}

func resetGroupSlots(updates map[string]any) {
	for i := 1; i <= maxManagedGroups; i++ {
		updates[fmt.Sprintf("group_chat_id_%d", i)] = ""
		updates[fmt.Sprintf("group_button_id_%d", i)] = ""
		updates[fmt.Sprintf("group_title_%d", i)] = ""
	}
}
