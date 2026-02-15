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
			"my_campus":        campus.ShortName,
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

	// prepare_clubs_for_buttons: Fetch clubs and expose them for button rendering
	registry.Register("prepare_clubs_for_buttons", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
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

		var clubs any
		var clubList []any

		if isLocal {
			campusIDStr, ok := payload["campus_id"].(string)
			if !ok || campusIDStr == "" || campusIDStr == "$context.campus_id" {
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
				return "", nil, fmt.Errorf("campus_id missing for local clubs")
			}

			var campusUUID pgtype.UUID
			if err := campusUUID.Scan(campusIDStr); err != nil {
				return "", nil, fmt.Errorf("invalid campus_id format: %w", err)
			}

			localClubs, err := queries.GetLocalClubs(ctx, campusUUID)
			if err != nil {
				return "", nil, fmt.Errorf("failed to fetch local clubs: %w", err)
			}
			clubs = localClubs
			for _, c := range localClubs {
				clubList = append(clubList, c)
			}
		} else {
			globalClubs, err := queries.GetGlobalClubs(ctx)
			if err != nil {
				return "", nil, fmt.Errorf("failed to fetch global clubs: %w", err)
			}
			clubs = globalClubs
			for _, c := range globalClubs {
				clubList = append(clubList, c)
			}
		}

		// Populate context with individual club names for button rendering
		// Format: club_name_1, club_name_2, etc. (up to 20 clubs)
		updates := make(map[string]any)
		updates["clubs"] = clubs
		updates["is_local"] = isLocal

		for i, clubAny := range clubList {
			if i >= 20 { // Limit to 20 buttons
				break
			}
			clubNum := i + 1

			// Extract club name from union type (could be local or global)
			var clubName string
			switch c := clubAny.(type) {
			case db.GetLocalClubsRow:
				clubName = c.Name
			case db.GetGlobalClubsRow:
				clubName = c.Name
			}

			updates[fmt.Sprintf("club_name_%d", clubNum)] = clubName
		}

		return "", updates, nil
	})

	// get_categories: Fetch unique categories from clubs
	registry.Register("get_categories", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
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

		// Get all clubs and extract unique categories
		categoryMap := make(map[string]bool)
		var categories []string
		var resolvedCampusID string

		if isLocal {
			campusIDStr, ok := payload["campus_id"].(string)
			if !ok || campusIDStr == "" || campusIDStr == "$context.campus_id" {
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
			if campusIDStr != "" && campusIDStr != "$context.campus_id" {
				resolvedCampusID = campusIDStr
			}

			if campusIDStr != "" && campusIDStr != "$context.campus_id" {
				var campusUUID pgtype.UUID
				if err := campusUUID.Scan(campusIDStr); err == nil {
					clubs, err := queries.GetLocalClubs(ctx, campusUUID)
					if err == nil {
						for _, c := range clubs {
							if !categoryMap[c.CategoryName] {
								categoryMap[c.CategoryName] = true
								categories = append(categories, c.CategoryName)
							}
						}
					}
				}
			}
		} else {
			clubs, err := queries.GetGlobalClubs(ctx)
			if err == nil {
				for _, c := range clubs {
					if !categoryMap[c.CategoryName] {
						categoryMap[c.CategoryName] = true
						categories = append(categories, c.CategoryName)
					}
				}
			}
		}

		// Populate context with category names for button rendering
		updates := make(map[string]any)
		updates["is_local"] = isLocal
		if resolvedCampusID != "" {
			updates["campus_id"] = resolvedCampusID
		}

		// First, fill actual categories (up to 15)
		maxCategories := 15
		for i, cat := range categories {
			if i >= maxCategories {
				break
			}
			catNum := i + 1
			updates[fmt.Sprintf("category_%d", catNum)] = cat
		}
		// Then, explicitly clear the rest so старые значения не утекали между экранами
		for i := len(categories) + 1; i <= maxCategories; i++ {
			updates[fmt.Sprintf("category_%d", i)] = ""
		}

		return "", updates, nil
	})

	// get_clubs_by_category: Fetch clubs filtered by category
	registry.Register("get_clubs_by_category", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		categoryName, ok := payload["category_name"].(string)
		if !ok || categoryName == "" {
			categoryName, ok = payload["last_input"].(string)
		}
		if !ok || categoryName == "" {
			categoryName, ok = payload["_last_input"].(string)
		}
		if !ok || categoryName == "" {
			return "", nil, fmt.Errorf("category_name or last_input required")
		}

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

		var filteredClubs []any
		var resolvedCampusID string

		if isLocal {
			campusIDStr, ok := payload["campus_id"].(string)
			if !ok || campusIDStr == "" || campusIDStr == "$context.campus_id" {
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
			if campusIDStr != "" && campusIDStr != "$context.campus_id" {
				resolvedCampusID = campusIDStr
			}

			if campusIDStr != "" && campusIDStr != "$context.campus_id" {
				var campusUUID pgtype.UUID
				if err := campusUUID.Scan(campusIDStr); err == nil {
					clubs, err := queries.GetLocalClubs(ctx, campusUUID)
					if err == nil {
						for _, c := range clubs {
							if c.CategoryName == categoryName {
								filteredClubs = append(filteredClubs, c)
							}
						}
					}
				}
			}
		} else {
			clubs, err := queries.GetGlobalClubs(ctx)
			if err == nil {
				for _, c := range clubs {
					if c.CategoryName == categoryName {
						filteredClubs = append(filteredClubs, c)
					}
				}
			}
		}

		// Populate context with club names for button rendering
		updates := make(map[string]any)
		updates["clubs"] = filteredClubs
		updates["category_name"] = categoryName
		updates["is_local"] = isLocal
		if resolvedCampusID != "" {
			updates["campus_id"] = resolvedCampusID
		}

		// First, fill actual clubs (up to 20)
		maxClubs := 20
		limit := min(len(filteredClubs), maxClubs)
		for i := range limit {
			clubNum := i + 1
			clubAny := filteredClubs[i]

			var clubName string
			var clubID int16
			switch c := clubAny.(type) {
			case db.GetLocalClubsRow:
				clubName = c.Name
				clubID = c.ID
			case db.GetGlobalClubsRow:
				clubName = c.Name
				clubID = c.ID
			}

			updates[fmt.Sprintf("club_name_%d", clubNum)] = clubName
			updates[fmt.Sprintf("club_id_%d", clubNum)] = clubID
		}
		// Then, explicitly clear оставшиеся club_name_i / club_id_i,
		// чтобы при смене категории не показывались клубы из прошлого выбора.
		for i := limit + 1; i <= maxClubs; i++ {
			updates[fmt.Sprintf("club_name_%d", i)] = ""
			updates[fmt.Sprintf("club_id_%d", i)] = ""
		}

		return "", updates, nil
	})

	// get_club_card: Fetch a specific club by ID and format as a card
	registry.Register("get_club_card", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		var clubID int16

		// Try to get club_id as integer first
		if val, ok := payload["club_id"]; ok {
			switch v := val.(type) {
			case int:
				clubID = int16(v)
			case int16:
				clubID = v
			case int32:
				clubID = int16(v)
			case int64:
				clubID = int16(v)
			case float64:
				clubID = int16(v)
			case string:
				if id, err := fmt.Sscanf(v, "%d", &clubID); err != nil || id != 1 {
					clubID = 0
				}
			}
		}

		// If not found, try last_input which could be a callback like "club_5_callback"
		if clubID == 0 {
			clubIDStr, ok := payload["last_input"].(string)
			if ok && clubIDStr != "" {
				// Clean up any escaped characters
				clubIDStr = strings.ReplaceAll(clubIDStr, "\\", "")
				// Extract club ID from callback format (e.g., "club_5_callback" -> "5")
				if strings.Contains(clubIDStr, "_callback") {
					parts := strings.Split(clubIDStr, "_")
					if len(parts) >= 2 {
						if id, err := fmt.Sscanf(parts[1], "%d", &clubID); err != nil || id != 1 {
							clubID = 0
						}
					}
				}
			}
		}

		if clubID == 0 {
			return "", nil, fmt.Errorf("club_id not found in payload")
		}

		// Query for both local and global clubs
		globalClubs, err := queries.GetGlobalClubs(ctx)
		if err != nil {
			return "", nil, fmt.Errorf("failed to fetch global clubs: %w", err)
		}

		for _, c := range globalClubs {
			if c.ID == clubID {
				card := formatClubCard(c)
				return "", map[string]any{
					"club_card": card,
					"club_name": c.Name,
					"club_link": c.ExternalLink.String,
					"club_id":   clubID,
				}, nil
			}
		}

		// If not found globally, try locally
		campusIDStr, _ := payload["campus_id"].(string)
		if campusIDStr != "" && campusIDStr != "$context.campus_id" {
			var campusUUID pgtype.UUID
			if err := campusUUID.Scan(campusIDStr); err == nil {
				localClubs, err := queries.GetLocalClubs(ctx, campusUUID)
				if err == nil {
					for _, c := range localClubs {
						if c.ID == clubID {
							card := formatLocalClubCard(c)
							return "", map[string]any{
								"club_card": card,
								"club_name": c.Name,
								"club_link": c.ExternalLink.String,
								"club_id":   clubID,
							}, nil
						}
					}
				}
			}
		}

		return "", nil, fmt.Errorf("club not found with id %d", clubID)
	})
}

func formatLocalClubs(clubs []db.GetLocalClubsRow) string {
	var sb strings.Builder
	for _, c := range clubs {
		sb.WriteString(fmt.Sprintf("*%s* [%s]\n", fsm.EscapeMarkdown(c.Name), fsm.EscapeMarkdown(c.CategoryName)))
		if c.Description.Valid && c.Description.String != "" {
			sb.WriteString(fmt.Sprintf("%s\n", fsm.EscapeMarkdown(c.Description.String)))
		}
		if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
			sb.WriteString(fmt.Sprintf("👤 Leader: %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String)))
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
		sb.WriteString(fmt.Sprintf("*%s* [%s]\n", fsm.EscapeMarkdown(c.Name), fsm.EscapeMarkdown(c.CategoryName)))
		if c.Description.Valid && c.Description.String != "" {
			sb.WriteString(fmt.Sprintf("%s\n", fsm.EscapeMarkdown(c.Description.String)))
		}
		if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
			sb.WriteString(fmt.Sprintf("👤 Leader: %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String)))
		}
		if c.ExternalLink.Valid && c.ExternalLink.String != "" {
			sb.WriteString(fmt.Sprintf("🔗 [Join](%s)\n", c.ExternalLink.String))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatClubCard formats a global club as a detailed card
func formatClubCard(c db.GetGlobalClubsRow) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%s*\n\n", fsm.EscapeMarkdown(c.Name)))

	if c.Description.Valid && c.Description.String != "" {
		// Use plain text for description to avoid nested italics issues with underscores
		sb.WriteString(fmt.Sprintf("%s\n\n", fsm.EscapeMarkdown(c.Description.String)))
	}
	if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
		sb.WriteString(fmt.Sprintf("👤 *Лидер:* %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String)))
	}
	sb.WriteString(fmt.Sprintf("📂 *Категория:* %s\n", fsm.EscapeMarkdown(c.CategoryName)))
	if c.CampusName != "" {
		sb.WriteString(fmt.Sprintf("📍 *Кампус:* %s\n", fsm.EscapeMarkdown(c.CampusName)))
	}
	return sb.String()
}

// formatLocalClubCard formats a local club as a detailed card
func formatLocalClubCard(c db.GetLocalClubsRow) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%s*\n\n", fsm.EscapeMarkdown(c.Name)))

	if c.Description.Valid && c.Description.String != "" {
		// Use plain text for description to avoid nested italics issues with underscores
		sb.WriteString(fmt.Sprintf("%s\n\n", fsm.EscapeMarkdown(c.Description.String)))
	}
	if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
		sb.WriteString(fmt.Sprintf("👤 *Лидер:* %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String)))
	}
	sb.WriteString(fmt.Sprintf("📂 *Категория:* %s\n", fsm.EscapeMarkdown(c.CategoryName)))
	sb.WriteString(fmt.Sprintf("📍 *Организовали в:* %s\n", fsm.EscapeMarkdown(c.CampusName)))
	return sb.String()
}
