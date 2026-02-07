package statistics

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers statistics-related actions.
func Register(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("STATS_MENU", "statistics.yaml/STATS_SEARCH_MENU")
	}

	registry.Register("get_user_stats", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		// 1. Get student login by external ID
		// We assume Telegram platform for now
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			log.Error("failed to get user account", "user_id", userID, "error", err)
			return "", nil, err
		}

		// 2. Get profile
		profile, err := queries.GetStudentProfile(ctx, acc.StudentID)
		if err != nil {
			log.Error("failed to get student profile", "login", acc.StudentID, "error", err)
			return "", nil, err
		}

		// 3. Prepare variables
		vars := map[string]interface{}{
			"my_s21login":  profile.S21Login,
			"my_exp":       profile.ExpValue.Int32,
			"my_level":     profile.Level.Int32,
			"my_prps":      profile.Prp.Int32,
			"my_crps":      profile.Crp.Int32,
			"my_coins":     profile.Coins.Int32,
			"my_campus":    "Неизвестный кампус",
			"my_coalition": "Нет коалиции",
		}

		if profile.CampusName.Valid {
			vars["my_campus"] = profile.CampusName.String
		}
		if profile.CoalitionName.Valid {
			vars["my_coalition"] = profile.CoalitionName.String
		}

		return "", vars, nil
	})

	registry.Register("get_peer_data_with_permissions", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login, ok := payload["login"].(string)
		if !ok {
			return "", nil, fmt.Errorf("login not found in payload")
		}

		// 1. Get peer profile
		profile, err := queries.GetPeerProfile(ctx, login)
		if err != nil {
			// Handle not found
			return "", map[string]interface{}{
				"peer_found": false,
			}, nil
		}

		// 2. Prepare variables
		vars := map[string]interface{}{
			"peer_found":     true,
			"peer_login":     profile.S21Login,
			"peer_campus":    "Неизвестный кампус",
			"peer_coalition": "Нет коалиции",
			"peer_level":     profile.Level.Int32,
			"peer_exp":       profile.ExpValue.Int32,
			"peer_coins":     profile.Coins.Int32,
			"peer_telegram":  profile.TelegramUsername,
			"peer_id":        0, // We don't necessarily have their platform ID if they haven't registered
		}

		if profile.CampusName.Valid {
			vars["peer_campus"] = profile.CampusName.String
		}
		if profile.CoalitionName.Valid {
			vars["peer_coalition"] = profile.CoalitionName.String
		}

		// Try to get their platform ID if they are registered
		acc, err := queries.GetUserAccountByStudentId(ctx, login)
		if err == nil {
			vars["peer_id"] = acc.ExternalID
		}

		return "", vars, nil
	})

	registry.Register("get_user_skills", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		// Find login
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		skills, err := queries.GetStudentSkills(ctx, acc.StudentID)
		if err != nil {
			return "", nil, err
		}

		// Format skills for context
		skillMap := make(map[string]int32)
		for _, s := range skills {
			skillMap[s.Name] = s.Value
		}

		return "", map[string]interface{}{
			"my_skills": skillMap,
		}, nil
	})

	registry.Register("generate_radar_chart", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		usersRaw, ok := payload["users"].([]interface{})
		if !ok {
			return "", nil, fmt.Errorf("users list not found in payload")
		}

		// For now, let's just generate a simple Mermaid list or pie
		// Since Mermaid radar is tricky, we'll use a comment or a simple text chart
		// Wait, let's try a simple Mermaid pie chart as placeholder
		mermaid := "```mermaid\npie title Навыки\n"

		for _, uRaw := range usersRaw {
			var uID string
			switch v := uRaw.(type) {
			case string:
				uID = v
			case int64:
				uID = fmt.Sprintf("%d", v)
			case float64:
				uID = fmt.Sprintf("%.0f", v)
			}

			// Get login
			acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: uID,
			})
			if err != nil {
				continue
			}

			skills, _ := queries.GetStudentSkills(ctx, acc.StudentID)
			for _, s := range skills {
				mermaid += fmt.Sprintf("    \"%s (%s)\" : %d\n", s.Name, acc.StudentID, s.Value)
			}
		}
		mermaid += "```"

		return "", map[string]interface{}{
			"radar_chart_mermaid":      mermaid,
			"radar_comparison_mermaid": mermaid,
		}, nil
	})
}
