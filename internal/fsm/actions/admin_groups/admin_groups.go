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
		}

		// Since the detailed telegram_groups tables are missing in the current schema,
		// we check for club leadership if available.
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			log.Debug("admin groups: user account lookup failed", "user_id", userID, "error", err)
			return "", updates, nil
		}

		updates["is_bot_owner"] = (acc.S21Login == cfg.Init.SchoolLogin)

		clubs, err := queries.GetGlobalClubs(ctx)
		if err == nil {
			var ownedClubs []db.GetGlobalClubsRow
			for _, c := range clubs {
				if c.LeaderLogin.Valid && c.LeaderLogin.String == acc.S21Login {
					ownedClubs = append(ownedClubs, c)
				}
			}

			updates["is_group_owner"] = len(ownedClubs) > 0
			if len(ownedClubs) > 0 {
				updates["chat_id"] = ownedClubs[0].ID
				updates["chat_title"] = strings.TrimSpace(ownedClubs[0].Name)
				if len(ownedClubs) > 1 {
					updates["chat_title"] = fmt.Sprintf("%s (+%d)", strings.TrimSpace(ownedClubs[0].Name), len(ownedClubs)-1)
				}
			}
		}

		return "", updates, nil
	})
}
