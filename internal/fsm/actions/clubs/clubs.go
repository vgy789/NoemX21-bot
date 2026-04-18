package clubs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"gopkg.in/yaml.v3"
)

const (
	fallbackVariousPath       = "data_repo/bot_content/various"
	communityLinksFilename    = "community_links.yaml"
	defaultBotTelegramLink    = "https://t.me/my_bot"
	defaultBotContentRepoLink = ""
	defaultBotRepoLink        = ""
)

type communityLinksYAML struct {
	ContentLinks contentLinksSectionYAML `yaml:"content_links"`
}

type contentLinksSectionYAML struct {
	BotLinks  botLinksYAML  `yaml:"bot_links"`
	RepoLinks repoLinksYAML `yaml:"repo_links"`
}

type botLinksYAML struct {
	Telegram string `yaml:"telegram"`
}

type repoLinksYAML struct {
	Bot        string `yaml:"bot"`
	BotContent string `yaml:"bot_content"`
}

// Register registers clubs-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, variousPath string, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("CLUBS_MENU", "clubs.yaml/INIT_CLUBS")
		aliasRegistrar("CLUBS_MAIN", "clubs.yaml/CLUBS_MAIN")
	}

	registry.Register("get_campus_info", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr := ensureCampusID(ctx, queries, userID, payload)
		if campusIDStr == "" {
			return "", nil, fmt.Errorf("campus_id missing for campus info fetch and could not be recovered from DB")
		}

		campusUUID, err := parseCampusUUID(campusIDStr)
		if err != nil {
			return "", nil, fmt.Errorf("invalid campus_id format: %w (got: %s)", err, campusIDStr)
		}

		campus, err := queries.GetCampusByID(ctx, campusUUID)
		if err != nil {
			return "", nil, fmt.Errorf("failed to fetch campus info: %w", err)
		}

		botTelegramLink, botContentRepoLink, botRepoLink := loadBotLinks(variousPath)

		return "", map[string]any{
			"leader_name":           campus.LeaderName.String,
			"leader_form_link":      campus.LeaderFormLink.String,
			"campus_id":             campusIDStr, // Ensure it's back in context
			"my_campus":             campus.ShortName,
			"bot_telegram_link":     botTelegramLink,
			"bot_content_repo_link": botContentRepoLink,
			"bot_repo_link":         botRepoLink,
		}, nil
	})

	registry.Register("get_clubs", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		isLocal := boolFromPayload(payload, "is_local")
		campusIDStr := ensureCampusID(ctx, queries, userID, payload)
		var clubsList string

		if isLocal {
			if campusIDStr == "" {
				return "", nil, fmt.Errorf("campus_id missing for local clubs fetch and could not be recovered from DB")
			}
			campusUUID, err := parseCampusUUID(campusIDStr)
			if err != nil {
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
		isLocal := boolFromPayload(payload, "is_local")
		campusIDStr := ensureCampusID(ctx, queries, userID, payload)

		var clubs any
		var clubList []any

		if isLocal {
			if campusIDStr == "" {
				return "", nil, fmt.Errorf("campus_id missing for local clubs")
			}
			campusUUID, err := parseCampusUUID(campusIDStr)
			if err != nil {
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

		updates := map[string]any{
			"clubs":    clubs,
			"is_local": isLocal,
		}
		if campusIDStr != "" && isLocal {
			updates["campus_id"] = campusIDStr
		}

		writeClubButtons(updates, clubList, 20)
		return "", updates, nil
	})

	// get_categories: Fetch unique categories from clubs
	registry.Register("get_categories", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		isLocal := boolFromPayload(payload, "is_local")

		categoryMap := make(map[string]bool)
		var categories []string
		campusIDStr := ensureCampusID(ctx, queries, userID, payload)

		if isLocal && campusIDStr != "" {
			if campusUUID, err := parseCampusUUID(campusIDStr); err == nil {
				if clubs, err := queries.GetLocalClubs(ctx, campusUUID); err == nil {
					for _, c := range clubs {
						if !categoryMap[c.CategoryName] {
							categoryMap[c.CategoryName] = true
							categories = append(categories, c.CategoryName)
						}
					}
				}
			}
		} else if !isLocal {
			if clubs, err := queries.GetGlobalClubs(ctx); err == nil {
				for _, c := range clubs {
					if !categoryMap[c.CategoryName] {
						categoryMap[c.CategoryName] = true
						categories = append(categories, c.CategoryName)
					}
				}
			}
		}

		updates := map[string]any{
			"is_local": isLocal,
		}
		if campusIDStr != "" {
			updates["campus_id"] = campusIDStr
		}

		writeCategoryButtons(updates, categories, 15)
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

		isLocal := boolFromPayload(payload, "is_local")
		campusIDStr := ensureCampusID(ctx, queries, userID, payload)

		var filteredClubs []any
		if isLocal && campusIDStr != "" {
			if campusUUID, err := parseCampusUUID(campusIDStr); err == nil {
				if clubs, err := queries.GetLocalClubs(ctx, campusUUID); err == nil {
					for _, c := range clubs {
						if c.CategoryName == categoryName {
							filteredClubs = append(filteredClubs, c)
						}
					}
				}
			}
		} else if !isLocal {
			if clubs, err := queries.GetGlobalClubs(ctx); err == nil {
				for _, c := range clubs {
					if c.CategoryName == categoryName {
						filteredClubs = append(filteredClubs, c)
					}
				}
			}
		}

		updates := map[string]any{
			"clubs":         filteredClubs,
			"category_name": categoryName,
			"is_local":      isLocal,
		}
		if campusIDStr != "" {
			updates["campus_id"] = campusIDStr
		}

		writeClubButtons(updates, filteredClubs, 20)
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
		campusIDStr := stringFromPayload(payload, "campus_id")
		if campusIDStr != "" {
			if campusUUID, err := parseCampusUUID(campusIDStr); err == nil {
				if localClubs, err := queries.GetLocalClubs(ctx, campusUUID); err == nil {
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
		_, _ = fmt.Fprintf(&sb, "*%s* [%s]\n", fsm.EscapeMarkdown(c.Name), fsm.EscapeMarkdown(c.CategoryName))
		if c.Description.Valid && c.Description.String != "" {
			_, _ = fmt.Fprintf(&sb, "%s\n", fsm.EscapeMarkdown(c.Description.String))
		}
		if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
			_, _ = fmt.Fprintf(&sb, "👤 Leader: %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String))
		}
		if c.ExternalLink.Valid && c.ExternalLink.String != "" {
			_, _ = fmt.Fprintf(&sb, "🔗 [Join](%s)\n", c.ExternalLink.String)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGlobalClubs(clubs []db.GetGlobalClubsRow) string {
	var sb strings.Builder
	for _, c := range clubs {
		_, _ = fmt.Fprintf(&sb, "*%s* [%s]\n", fsm.EscapeMarkdown(c.Name), fsm.EscapeMarkdown(c.CategoryName))
		if c.Description.Valid && c.Description.String != "" {
			_, _ = fmt.Fprintf(&sb, "%s\n", fsm.EscapeMarkdown(c.Description.String))
		}
		if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
			_, _ = fmt.Fprintf(&sb, "👤 Leader: %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String))
		}
		if c.ExternalLink.Valid && c.ExternalLink.String != "" {
			_, _ = fmt.Fprintf(&sb, "🔗 [Join](%s)\n", c.ExternalLink.String)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatClubCard formats a global club as a detailed card
func formatClubCard(c db.GetGlobalClubsRow) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "*%s*\n\n", fsm.EscapeMarkdown(c.Name))

	if c.Description.Valid && c.Description.String != "" {
		// Use plain text for description to avoid nested italics issues with underscores
		_, _ = fmt.Fprintf(&sb, "%s\n\n", fsm.EscapeMarkdown(c.Description.String))
	}
	if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
		_, _ = fmt.Fprintf(&sb, "👤 *Лидер:* %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String))
	}
	_, _ = fmt.Fprintf(&sb, "📂 *Категория:* %s\n", fsm.EscapeMarkdown(c.CategoryName))
	if c.CampusName != "" {
		_, _ = fmt.Fprintf(&sb, "📍 *Кампус:* %s\n", fsm.EscapeMarkdown(c.CampusName))
	}
	return sb.String()
}

// formatLocalClubCard formats a local club as a detailed card
func formatLocalClubCard(c db.GetLocalClubsRow) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "*%s*\n\n", fsm.EscapeMarkdown(c.Name))

	if c.Description.Valid && c.Description.String != "" {
		// Use plain text for description to avoid nested italics issues with underscores
		_, _ = fmt.Fprintf(&sb, "%s\n\n", fsm.EscapeMarkdown(c.Description.String))
	}
	if c.LeaderLogin.Valid && c.LeaderLogin.String != "" {
		_, _ = fmt.Fprintf(&sb, "👤 *Лидер:* %s\n", fsm.EscapeMarkdown(c.LeaderLogin.String))
	}
	_, _ = fmt.Fprintf(&sb, "📂 *Категория:* %s\n", fsm.EscapeMarkdown(c.CategoryName))
	_, _ = fmt.Fprintf(&sb, "📍 *Организовали в:* %s\n", fsm.EscapeMarkdown(c.CampusName))
	return sb.String()
}

func stringFromPayload(payload map[string]any, key string) string {
	if val, ok := payload[key]; ok {
		if s, ok := val.(string); ok && s != "" && s != "$context.campus_id" {
			return s
		}
	}
	return ""
}

func boolFromPayload(payload map[string]any, key string) bool {
	if val, ok := payload[key]; ok {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			return strings.EqualFold(v, "true")
		}
	}
	return false
}

func ensureCampusID(ctx context.Context, queries db.Querier, userID int64, payload map[string]any) string {
	if campusID := stringFromPayload(payload, "campus_id"); campusID != "" {
		return campusID
	}
	return campusIDFromProfile(ctx, queries, userID)
}

func campusIDFromProfile(ctx context.Context, queries db.Querier, userID int64) string {
	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err != nil {
		return ""
	}
	profile, err := queries.GetMyProfile(ctx, acc.S21Login)
	if err != nil || !profile.CampusID.Valid {
		return ""
	}
	b := profile.CampusID.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseCampusUUID(campusIDStr string) (pgtype.UUID, error) {
	var campusUUID pgtype.UUID
	if err := campusUUID.Scan(campusIDStr); err != nil {
		return pgtype.UUID{}, err
	}
	return campusUUID, nil
}

func writeClubButtons(updates map[string]any, clubs []any, max int) {
	limit := minInt(len(clubs), max)
	for i := range limit {
		name, id := clubData(clubs[i])
		updates[fmt.Sprintf("club_name_%d", i+1)] = name
		updates[fmt.Sprintf("club_id_%d", i+1)] = id
	}
	for i := limit; i < max; i++ {
		updates[fmt.Sprintf("club_name_%d", i+1)] = ""
		updates[fmt.Sprintf("club_id_%d", i+1)] = ""
	}
}

func writeCategoryButtons(updates map[string]any, categories []string, max int) {
	limit := minInt(len(categories), max)
	for i := range limit {
		updates[fmt.Sprintf("category_%d", i+1)] = categories[i]
	}
	for i := limit; i < max; i++ {
		updates[fmt.Sprintf("category_%d", i+1)] = ""
	}
}

func clubData(club any) (string, int16) {
	switch c := club.(type) {
	case db.GetLocalClubsRow:
		return c.Name, c.ID
	case db.GetGlobalClubsRow:
		return c.Name, c.ID
	default:
		return "", 0
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func loadBotLinks(variousPath string) (string, string, string) {
	botTelegramLink := defaultBotTelegramLink
	botContentRepoLink := defaultBotContentRepoLink
	botRepoLink := defaultBotRepoLink

	communityLinksPath, ok := resolveCommunityLinksPath(variousPath)
	if !ok {
		return botTelegramLink, botContentRepoLink, botRepoLink
	}

	data, err := os.ReadFile(communityLinksPath)
	if err != nil {
		return botTelegramLink, botContentRepoLink, botRepoLink
	}

	var links communityLinksYAML
	if err := yaml.Unmarshal(data, &links); err != nil {
		return botTelegramLink, botContentRepoLink, botRepoLink
	}

	if link := strings.TrimSpace(links.ContentLinks.BotLinks.Telegram); link != "" {
		botTelegramLink = link
	}
	if link := strings.TrimSpace(links.ContentLinks.RepoLinks.BotContent); link != "" {
		botContentRepoLink = link
	}
	if link := strings.TrimSpace(links.ContentLinks.RepoLinks.Bot); link != "" {
		botRepoLink = link
	}

	return botTelegramLink, botContentRepoLink, botRepoLink
}

func resolveCommunityLinksPath(variousPath string) (string, bool) {
	configuredPath := strings.TrimSpace(variousPath)

	localBasePath := strings.TrimSpace(os.Getenv("GIT_LOCAL_PATH"))
	if localBasePath == "" {
		localBasePath = "data"
	}

	candidates := make([]string, 0, 4)
	if configuredPath != "" {
		if filepath.IsAbs(configuredPath) {
			candidates = append(candidates, filepath.Join(configuredPath, communityLinksFilename))
		} else {
			candidates = append(candidates, filepath.Join(localBasePath, configuredPath, communityLinksFilename))
			candidates = append(candidates, filepath.Join(configuredPath, communityLinksFilename))
		}
	}
	candidates = append(candidates, filepath.Join(fallbackVariousPath, communityLinksFilename))

	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, true
		}
	}

	return "", false
}
