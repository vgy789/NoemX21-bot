package clubs

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers clubs-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("CLUBS_MENU", "clubs.yaml/INIT_CLUBS")
	}

	registry.Register("get_campus_info", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, ok := payload["campus_id"].(string)
		if !ok || campusIDStr == "" || campusIDStr == "$context.campus_id" {
			// Try to fetch from DB if missing in context
			acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: fmt.Sprintf("%d", userID),
			})
			if err == nil {
				profile, err := queries.GetMyProfile(ctx, acc.S21Login)
				if err == nil && profile.CampusID.Valid {
					b := profile.CampusID.Bytes
					campusIDStr = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
				}
			}
		}

		if campusIDStr == "" || campusIDStr == "$context.campus_id" {
			return "", nil, fmt.Errorf("campus_id missing for campus info fetch and could not be recovered from DB")
		}

		var campusUUID pgtype.UUID
		if err := campusUUID.Scan(campusIDStr); err != nil {
			return "", nil, fmt.Errorf("invalid campus_id format: %w (got: %s)", err, campusIDStr)
		}

		campus, err := queries.GetCampusByID(ctx, campusUUID)
		if err != nil {
			return "", nil, fmt.Errorf("failed to fetch campus info: %w", err)
		}

		return "", map[string]any{
			"leader_name":      campus.LeaderName.String,
			"leader_form_link": campus.LeaderFormLink.String,
			"campus_id":        campusIDStr, // Ensure it's back in context
		}, nil
	})

	registry.Register("get_clubs", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		// Log the raw payload for debugging
		fmt.Printf("DEBUG: get_clubs payload: %+v\n", payload)

		var isLocal bool
		if val, ok := payload["is_local"]; ok {
			switch v := val.(type) {
			case bool:
				isLocal = v
			case string:
				isLocal = (v == "true")
			default:
				isLocal = false
			}
		}
		fmt.Printf("DEBUG: isLocal evaluated to: %v\n", isLocal)

		var clubsList string

		if isLocal {
			campusIDStr, ok := payload["campus_id"].(string)
			if !ok || campusIDStr == "" || campusIDStr == "$context.campus_id" {
				// Try to fetch from DB if missing in context
				acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
					Platform:   db.EnumPlatformTelegram,
					ExternalID: fmt.Sprintf("%d", userID),
				})
				if err == nil {
					profile, err := queries.GetMyProfile(ctx, acc.S21Login)
					if err == nil && profile.CampusID.Valid {
						b := profile.CampusID.Bytes
						campusIDStr = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
					}
				}
			}

			if campusIDStr == "" || campusIDStr == "$context.campus_id" {
				return "", nil, fmt.Errorf("campus_id missing for local clubs fetch and could not be recovered from DB")
			}

			var campusUUID pgtype.UUID
			if err := campusUUID.Scan(campusIDStr); err != nil {
				return "", nil, fmt.Errorf("invalid campus_id format: %w (got: %s)", err, campusIDStr)
			}

			clubs, err := queries.GetLocalClubs(ctx, campusUUID)
			if err != nil {
				return "", nil, fmt.Errorf("failed to fetch local clubs: %w", err)
			}
			clubsList = formatLocalClubs(clubs)
		} else {
			clubs, err := queries.GetGlobalClubs(ctx)
			if err != nil {
				return "", nil, fmt.Errorf("failed to fetch global clubs: %w", err)
			}
			clubsList = formatGlobalClubs(clubs)
		}

		if clubsList == "" {
			clubsList = "😔 Пока нет активных клубов в этой категории."
		}

		return "", map[string]any{
			"clubs_list": clubsList,
		}, nil
	})
}

func formatLocalClubs(clubs []db.GetLocalClubsRow) string {
	var sb strings.Builder
	for _, c := range clubs {
		sb.WriteString(fmt.Sprintf("🔹 *%s* [%s]\n", c.Name, c.CategoryName))
		if c.Description.Valid && c.Description.String != "" {
			sb.WriteString(fmt.Sprintf("_%s_\n", c.Description.String))
		}
		if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
			sb.WriteString(fmt.Sprintf("👤 Leader: %s\n", c.LeaderLogin.String))
		}
		if c.ExternalLink.Valid && c.ExternalLink.String != "" {
			sb.WriteString(fmt.Sprintf("🔗 [Join](%s)\n", c.ExternalLink.String))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGlobalClubs(clubs []db.GetGlobalClubsRow) string {
	var sb strings.Builder
	for _, c := range clubs {
		sb.WriteString(fmt.Sprintf("🌍 *%s* [%s]\n", c.Name, c.CategoryName))
		if c.Description.Valid && c.Description.String != "" {
			sb.WriteString(fmt.Sprintf("_%s_\n", c.Description.String))
		}
		if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
			sb.WriteString(fmt.Sprintf("👤 Leader: %s\n", c.LeaderLogin.String))
		}
		if c.ExternalLink.Valid && c.ExternalLink.String != "" {
			sb.WriteString(fmt.Sprintf("🔗 [Join](%s)\n", c.ExternalLink.String))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
