package common

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const (
	reviewsPageSize = 5
	campusFilterPageSize = 7

	projectFilterModeProject = "project"
	projectFilterModeCourse  = "course"
	projectFilterModeNode    = "node"
)

var telegramUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

type reviewProject struct {
	ID   string
	Name string
	Type string
}

type reviewProjectGroup struct {
	ID    string
	Name  string
	Type  string
	Count int
}

type projectFilterCandidate struct {
	ButtonID   string
	Label      string
	ProjectIDs []int64
}

type campusFilterCandidate struct {
	ID    string
	Label string
}

func registerReviewActions(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	log *slog.Logger,
) {
	registry.Register("none", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", nil, nil
	})

	registry.Register("school_api_get_participant_projects_in_reviews", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", defaultReviewContext(payload), nil
		}

		projects := make([]reviewProject, 0, 8)
		if s21Client != nil && credService != nil {
			token, tokenErr := getReviewsToken(ctx, credService, acc.S21Login, fallbackSchoolLogin(cfg))
			if tokenErr != nil {
				log.Warn("reviews: failed to get valid token", "user_id", userID, "login", acc.S21Login, "error", tokenErr)
			} else {
				resp, apiErr := s21Client.GetParticipantProjects(ctx, token, acc.S21Login, 1000, 0, "IN_REVIEWS")
				if apiErr != nil {
					log.Warn("reviews: failed to get IN_REVIEWS projects", "user_id", userID, "login", acc.S21Login, "error", apiErr)
				} else if resp != nil {
					for _, p := range resp.Projects {
						if strings.EqualFold(strings.TrimSpace(p.Status), "IN_REVIEWS") {
							projects = append(projects, reviewProject{
								ID:   strconv.FormatInt(p.ID, 10),
								Name: normalizeMarkdownEscapes(strings.TrimSpace(p.Title)),
								Type: nonEmpty(strings.TrimSpace(p.Type), "INDIVIDUAL"),
							})
						}
					}
				}
			}
		}

		if len(projects) == 0 {
			projects = parseAvailableProjects(payload["available_projects"])
		}

		profile, profileErr := queries.GetMyProfile(ctx, acc.S21Login)
		timezoneName := "UTC"
		timezoneOffset := "+00:00"
		campusName := defaultString(payload["my_campus"], "Unknown campus")
		level := defaultString(payload["my_level"], "0")

		if profileErr == nil {
			timezoneName = resolveCampusAwareTimezone(ctx, queries, strings.TrimSpace(profile.Timezone), profile.CampusID)
			timezoneOffset = zoneOffsetString(timezoneName)
			if profile.CampusName.Valid && strings.TrimSpace(profile.CampusName.String) != "" {
				campusName = strings.TrimSpace(profile.CampusName.String)
			}
			if profile.Level.Valid {
				level = strconv.FormatInt(int64(profile.Level.Int32), 10)
			}
		}
		if tzFromPayload := strings.TrimSpace(ToString(payload["user_timezone_name"])); tzFromPayload != "" {
			timezoneName = tzFromPayload
		}
		if offFromPayload := strings.TrimSpace(ToString(payload["user_timezone_formatted"])); offFromPayload != "" {
			timezoneOffset = offFromPayload
		}

		encoded := encodeReviewProjects(projects)
		updates := defaultReviewContext(payload)
		updates["available_projects"] = encoded
		updates["available_projects_count"] = len(projects)
		updates["my_s21login"] = acc.S21Login
		updates["my_campus"] = campusName
		updates["my_level"] = level
		updates["user_timezone_name"] = timezoneName
		updates["user_timezone_formatted"] = timezoneOffset
		return "", updates, nil
	})

	registry.Register("db_get_my_prr_summary", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{
				"my_prr_count":            0,
				"active_prr_notification": "📭 Активных запросов нет.",
			}, nil
		}
		count, err := queries.CountOpenReviewRequestsByUser(ctx, acc.ID)
		if err != nil {
			log.Warn("reviews: failed to count open requests", "user_id", userID, "account_id", acc.ID, "error", err)
			count = 0
		}
		notification := "📭 Активных запросов нет."
		if count > 0 {
			notification = fmt.Sprintf("📌 Активных запросов: %d", count)
		}
		return "", map[string]any{
			"my_prr_count":            int(count),
			"active_prr_notification": notification,
		}, nil
	})

	registry.Register("prepare_available_projects_for_prr", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		projects := parseAvailableProjects(payload["available_projects"])
		page := max(ToInt(payload["page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateProjects(projects, page, reviewsPageSize)

		updates := map[string]any{
			"available_projects_count":           len(projects),
			"available_projects_has_prev_page":   hasPrev,
			"available_projects_has_next_page":   hasNext,
			"available_projects_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"available_projects_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"available_projects_list_hint":       fmt.Sprintf("Всего доступно: %d", len(projects)),
			"create_prr_projects_page":           page,
		}

		clearProjectButtonVars(updates)
		for i, p := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("project_id_%d", n)] = p.ID
			updates[fmt.Sprintf("project_name_%d", n)] = p.Name
			updates[fmt.Sprintf("project_type_%d", n)] = p.Type
		}
		return "", updates, nil
	})

	registry.Register("available_projects_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["create_prr_projects_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"create_prr_projects_page": page}, nil
	})

	registry.Register("available_projects_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["create_prr_projects_page"]), 1)
		totalPages := pagesCount(len(parseAvailableProjects(payload["available_projects"])), reviewsPageSize)
		if page < totalPages {
			page++
		}
		return "", map[string]any{"create_prr_projects_page": page}, nil
	})

	registry.Register("select_project", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		selectedID := strings.TrimSpace(ToString(payload["id"]))
		if selectedID == "" {
			return "", nil, nil
		}

		projects := parseAvailableProjects(payload["available_projects"])
		var selected reviewProject
		for _, p := range projects {
			if p.ID == selectedID {
				selected = p
				break
			}
		}
		if selected.ID == "" {
			for i := 1; i <= reviewsPageSize; i++ {
				if strings.TrimSpace(ToString(payload[fmt.Sprintf("project_id_%d", i)])) == selectedID {
					selected = reviewProject{
						ID:   selectedID,
						Name: strings.TrimSpace(ToString(payload[fmt.Sprintf("project_name_%d", i)])),
						Type: nonEmpty(strings.TrimSpace(ToString(payload[fmt.Sprintf("project_type_%d", i)])), "INDIVIDUAL"),
					}
					break
				}
			}
		}
		if selected.ID == "" {
			selected = reviewProject{
				ID:   selectedID,
				Name: defaultString(payload["selected_project_name"], "Unknown project"),
				Type: defaultString(payload["selected_project_type"], "INDIVIDUAL"),
			}
		}

		return "", map[string]any{
			"selected_project_id":   selected.ID,
			"selected_project_name": normalizeMarkdownEscapes(selected.Name),
			"selected_project_type": selected.Type,
		}, nil
	})

	registry.Register("db_check_open_prr_for_selected_project", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{"has_open_prr_for_selected_project": false}, nil
		}
		projectID, ok := toInt64(payload["project_id"])
		if !ok {
			projectID, ok = toInt64(payload["selected_project_id"])
		}
		if !ok {
			return "", map[string]any{"has_open_prr_for_selected_project": false}, nil
		}
		exists, err := queries.ExistsOpenReviewRequestByUserAndProject(ctx, db.ExistsOpenReviewRequestByUserAndProjectParams{
			RequesterUserID: acc.ID,
			ProjectID:       projectID,
		})
		if err != nil {
			log.Warn("reviews: failed to check duplicate project request", "user_id", userID, "account_id", acc.ID, "project_id", projectID, "error", err)
			return "", map[string]any{"has_open_prr_for_selected_project": false}, nil
		}
		return "", map[string]any{"has_open_prr_for_selected_project": exists}, nil
	})

	registry.Register("save_time_input", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		value := strings.TrimSpace(ToString(payload["last_input"]))
		if value == "" {
			value = "Flexible"
		}
		value = TrimRunes(value, 250)
		return "", map[string]any{
			"time_description":            value,
			"create_prr_time_description": value,
		}, nil
	})

	registry.Register("db_save_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{"save_conflict_same_project": false}, nil
		}

		projectID, ok := toInt64(payload["selected_project_id"])
		if !ok {
			return "", map[string]any{"save_conflict_same_project": false}, nil
		}

		duplicate, err := queries.ExistsOpenReviewRequestByUserAndProject(ctx, db.ExistsOpenReviewRequestByUserAndProjectParams{
			RequesterUserID: acc.ID,
			ProjectID:       projectID,
		})
		if err == nil && duplicate {
			return "", map[string]any{"save_conflict_same_project": true}, nil
		}

		profile, profileErr := queries.GetMyProfile(ctx, acc.S21Login)
		campusID := pgtype.UUID{}
		timezoneName := defaultString(payload["user_timezone_name"], "UTC")
		if profileErr == nil {
			if profile.CampusID.Valid {
				campusID = profile.CampusID
			}
			timezoneName = resolveCampusAwareTimezone(ctx, queries, strings.TrimSpace(profile.Timezone), profile.CampusID)
		}
		timezoneOffset := defaultString(payload["user_timezone_formatted"], zoneOffsetString(timezoneName))

		availability := strings.TrimSpace(ToString(payload["create_prr_time_description"]))
		if availability == "" {
			availability = strings.TrimSpace(ToString(payload["time_description"]))
		}
		if availability == "" {
			candidate := strings.TrimSpace(ToString(payload["last_input"]))
			if !isPRRControlInput(candidate) {
				availability = candidate
			}
		}
		availability = nonEmpty(TrimRunes(availability, 250), "Flexible")

		_, err = queries.CreateReviewRequest(ctx, db.CreateReviewRequestParams{
			RequesterUserID:         acc.ID,
			RequesterS21Login:       acc.S21Login,
			RequesterCampusID:       campusID,
			ProjectID:               projectID,
			ProjectName:             normalizeMarkdownEscapes(defaultString(payload["selected_project_name"], "Unknown project")),
			ProjectType:             defaultString(payload["selected_project_type"], "INDIVIDUAL"),
			AvailabilityText:        availability,
			RequesterTimezone:       timezoneName,
			RequesterTimezoneOffset: timezoneOffset,
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "uq_review_requests_open_per_project") {
				return "", map[string]any{"save_conflict_same_project": true}, nil
			}
			log.Warn("reviews: failed to save review request", "user_id", userID, "account_id", acc.ID, "project_id", projectID, "error", err)
			return "", map[string]any{"save_conflict_same_project": false}, nil
		}

		count, _ := queries.CountOpenReviewRequestsByUser(ctx, acc.ID)
		return "", map[string]any{
			"save_conflict_same_project": false,
			"my_prr_count":               int(count),
		}, nil
	})

	registry.Register("db_get_global_prr_project_groups", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		rows, err := queries.GetGlobalReviewProjectGroups(ctx)
		if err != nil {
			log.Warn("reviews: failed to load project groups", "error", err)
			rows = nil
		}

		projectFilters := parseStringSet(payload["filter_project_ids"])
		campusFilters := parseStringSet(payload["filter_campus_ids"])
		groups := make([]reviewProjectGroup, 0, len(rows))
		for _, r := range rows {
			id := strconv.FormatInt(r.ProjectID, 10)
			if len(projectFilters) > 0 && !projectFilters[id] {
				continue
			}
			requestsCount := int(r.RequestsCount)
			if len(campusFilters) > 0 {
				projectRows, projectErr := queries.GetOpenReviewRequestsByProject(ctx, r.ProjectID)
				if projectErr != nil {
					log.Warn("reviews: failed to apply campus filter for project", "project_id", r.ProjectID, "error", projectErr)
					continue
				}
				requestsCount = 0
				for _, prr := range projectRows {
					campusID := uuidToString(prr.RequesterCampusID)
					if campusID != "" && campusFilters[campusID] {
						requestsCount++
					}
				}
				if requestsCount == 0 {
					continue
				}
			}
			groups = append(groups, reviewProjectGroup{
				ID:    id,
				Name:  strings.TrimSpace(r.ProjectName),
				Type:  strings.TrimSpace(r.ProjectType),
				Count: requestsCount,
			})
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
		})

		page := max(ToInt(payload["page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateProjectGroups(groups, page, reviewsPageSize)

		updates := map[string]any{
			"global_prr_board_page":        page,
			"global_board_total_pages":     totalPages,
			"global_board_has_prev_page":   hasPrev,
			"global_board_has_next_page":   hasNext,
			"global_board_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"global_board_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"project_groups_formatted":     formatProjectGroups(pageItems),
			"current_project_filters_text": projectFilterTextWithCatalog(ctx, queries, projectFilters, collectKnownProjects(payload, rows), ToString(payload["language"])),
			"current_campus_filters_text":  campusFilterText(ctx, queries, campusFilters, ToString(payload["language"])),
		}
		clearProjectGroupVars(updates)
		for i, g := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("project_group_id_%d", n)] = g.ID
			updates[fmt.Sprintf("project_group_btn_label_%d", n)] = fmt.Sprintf("📁 %s · %d", g.Name, g.Count)
			updates[fmt.Sprintf("project_group_name_%d", n)] = g.Name
			updates[fmt.Sprintf("project_group_type_%d", n)] = g.Type
		}
		return "", updates, nil
	})

	registry.Register("global_prr_board_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["global_prr_board_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"global_prr_board_page": page}, nil
	})

	registry.Register("global_prr_board_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["global_prr_board_page"]), 1)
		totalPages := max(ToInt(payload["global_board_total_pages"]), 1)
		if page < totalPages {
			page++
		}
		return "", map[string]any{"global_prr_board_page": page}, nil
	})

	registry.Register("select_project_group", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id := strings.TrimSpace(ToString(payload["id"]))
		updates := map[string]any{
			"selected_project_id": id,
			"project_prr_page":    1,
		}
		for i := 1; i <= reviewsPageSize; i++ {
			if strings.TrimSpace(ToString(payload[fmt.Sprintf("project_group_id_%d", i)])) == id {
				updates["selected_project_name"] = defaultString(payload[fmt.Sprintf("project_group_name_%d", i)], "Unknown project")
				updates["selected_project_name"] = normalizeMarkdownEscapes(ToString(updates["selected_project_name"]))
				updates["selected_project_type"] = defaultString(payload[fmt.Sprintf("project_group_type_%d", i)], "INDIVIDUAL")
				return "", updates, nil
			}
		}
		updates["selected_project_name"] = normalizeMarkdownEscapes(defaultString(payload["selected_project_name"], "Unknown project"))
		updates["selected_project_type"] = defaultString(payload["selected_project_type"], "INDIVIDUAL")
		return "", updates, nil
	})

	registry.Register("db_get_prr_requests_for_selected_project", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		projectID, ok := toInt64(payload["project_id"])
		if !ok {
			projectID, ok = toInt64(payload["selected_project_id"])
		}
		if !ok {
			return "", emptyProjectRequestList(payload), nil
		}

		rows, err := queries.GetOpenReviewRequestsByProject(ctx, projectID)
		if err != nil {
			log.Warn("reviews: failed to load project requests", "project_id", projectID, "error", err)
			rows = nil
		}

		page := max(ToInt(payload["page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateProjectRequests(rows, page, reviewsPageSize)

		updates := map[string]any{
			"selected_project_id":         strconv.FormatInt(projectID, 10),
			"project_prr_page":            page,
			"project_prr_total_pages":     totalPages,
			"project_prr_has_prev_page":   hasPrev,
			"project_prr_has_next_page":   hasNext,
			"project_prr_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"project_prr_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"project_prr_list_formatted":  formatProjectRequestRows(pageItems),
		}
		clearProjectRequestVars(updates)
		for i, row := range pageItems {
			n := i + 1
			id := strconv.FormatInt(row.ID, 10)
			updates[fmt.Sprintf("project_prr_id_%d", n)] = id
			updates[fmt.Sprintf("project_prr_btn_label_%d", n)] = fmt.Sprintf("%s %s · %s", statusEmoji(row.Status), row.RequesterS21Login, nonEmpty(row.RequesterCampusName, "Unknown campus"))
		}
		if len(rows) > 0 {
			updates["selected_project_name"] = normalizeMarkdownEscapes(rows[0].ProjectName)
			updates["selected_project_type"] = rows[0].ProjectType
		}
		return "", updates, nil
	})

	registry.Register("selected_project_prr_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["project_prr_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"project_prr_page": page}, nil
	})

	registry.Register("selected_project_prr_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["project_prr_page"]), 1)
		total := max(ToInt(payload["project_prr_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"project_prr_page": page}, nil
	})

	registry.Register("select_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		id, ok := toInt64(payload["id"])
		if !ok {
			return "", nil, nil
		}
		row, err := queries.GetReviewRequestByID(ctx, id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return "", map[string]any{
					"selected_prr_id":          strconv.FormatInt(id, 10),
					"selected_prr_status":      "CLOSED",
					"prr_status_label":         statusLabel("CLOSED", ToString(payload["language"])),
					"project_still_in_reviews": false,
				}, nil
			}
			log.Warn("reviews: failed to load selected prr", "prr_id", id, "error", err)
			return "", nil, nil
		}

		viewerOffset := strings.TrimSpace(ToString(payload["user_timezone_formatted"]))
		if viewerOffset == "" {
			if acc, accErr := getTelegramAccount(ctx, queries, userID); accErr == nil {
				if profile, profileErr := queries.GetMyProfile(ctx, acc.S21Login); profileErr == nil {
					viewerTZ := resolveCampusAwareTimezone(ctx, queries, strings.TrimSpace(profile.Timezone), profile.CampusID)
					viewerOffset = zoneOffsetString(viewerTZ)
				}
			}
		}
		return "", detailUpdatesFromReviewRow(ctx, queries, row, ToString(payload["language"]), viewerOffset), nil
	})

	registry.Register("db_increment_prr_view_counter", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id, ok := toInt64(payload["prr_id"])
		if !ok {
			id, ok = toInt64(payload["selected_prr_id"])
		}
		if !ok {
			return "", nil, nil
		}
		count, err := queries.IncrementReviewRequestViewCount(ctx, id)
		if err != nil {
			log.Warn("reviews: failed to increment view counter", "prr_id", id, "error", err)
			return "", nil, nil
		}
		return "", map[string]any{"view_count": int(count)}, nil
	})

	registry.Register("validate_and_propose_review", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{"is_own_prr": false, "prr_closed": true, "project_still_in_reviews": true}, nil
		}

		id, ok := toInt64(payload["prr_id"])
		if !ok {
			id, ok = toInt64(payload["selected_prr_id"])
		}
		if !ok {
			return "", map[string]any{"is_own_prr": false, "prr_closed": true, "project_still_in_reviews": true}, nil
		}

		row, err := queries.GetReviewRequestByID(ctx, id)
		if err != nil {
			return "", map[string]any{"is_own_prr": false, "prr_closed": true, "project_still_in_reviews": false}, nil
		}

		isOwn := row.RequesterUserID == acc.ID
		status := strings.TrimSpace(string(row.Status))
		prrClosed := status == string(db.EnumReviewStatusCLOSED) || status == string(db.EnumReviewStatusPAUSED) || status == string(db.EnumReviewStatusNEGOTIATING)
		projectStillInReviews := true
		reviewerUsername := ""
		reviewerLevel := strings.TrimSpace(ToString(payload["my_level"]))
		if reviewerLevel == "" {
			if profile, profileErr := queries.GetMyProfile(ctx, acc.S21Login); profileErr == nil && profile.Level.Valid {
				reviewerLevel = strconv.FormatInt(int64(profile.Level.Int32), 10)
			}
		}
		if reviewerLevel == "" {
			reviewerLevel = "0"
		}
		reviewerTelegramUsername := ""
		if !acc.IsSearchable.Valid || acc.IsSearchable.Bool {
			reviewerTelegramUsername = sanitizeTelegramUsername(acc.Username.String)
		}
		reviewerUsername = reviewerTelegramUsername
		reviewerRocketchatID := ""
		reviewerAlternativeContact := ""
		if regUser, regErr := queries.GetRegisteredUserByS21Login(ctx, acc.S21Login); regErr == nil {
			reviewerRocketchatID = strings.TrimSpace(regUser.RocketchatID)
			if regUser.AlternativeContact.Valid {
				reviewerAlternativeContact = strings.TrimSpace(regUser.AlternativeContact.String)
			}
		}
		reviewerTelegramLink := ""
		if reviewerTelegramUsername != "" {
			reviewerTelegramLink = fmt.Sprintf("[@%s](https://t.me/%s)", reviewerTelegramUsername, reviewerTelegramUsername)
		}
		reviewerRocketchatLink := ""
		if reviewerRocketchatID != "" {
			reviewerRocketchatLink = fmt.Sprintf("[открыть диалог](https://rocketchat-student.21-school.ru/direct/%s)", reviewerRocketchatID)
		}

		if !isOwn && !prrClosed && s21Client != nil && credService != nil {
			token, tokenErr := getReviewsToken(ctx, credService, acc.S21Login, fallbackSchoolLogin(cfg))
			if tokenErr != nil {
				log.Warn("reviews: lazy-check token unavailable", "user_id", userID, "login", acc.S21Login, "error", tokenErr)
			} else {
				resp, apiErr := s21Client.GetParticipantProjects(ctx, token, row.RequesterS21Login, 1000, 0, "IN_REVIEWS")
				if apiErr != nil {
					log.Warn("reviews: lazy-check API failed", "prr_id", id, "requester_login", row.RequesterS21Login, "error", apiErr)
				} else if resp != nil {
					projectStillInReviews = containsProject(resp.Projects, row.ProjectID)
				}
			}
		}

		updates := map[string]any{
			"is_own_prr":                         isOwn,
			"prr_closed":                         prrClosed,
			"project_still_in_reviews":           projectStillInReviews,
			"selected_prr_id":                    strconv.FormatInt(id, 10),
			"requester_username":                 sanitizeTelegramUsername(row.RequesterTelegramUsername),
			"reviewer_username":                  reviewerTelegramUsername,
			"reviewer_rocketchat_id":             reviewerRocketchatID,
			"reviewer_alternative_contact":       reviewerAlternativeContact,
			"reviewer_alternative_contact_line":  buildReviewerAlternativeContactLine(ToString(payload["language"]), reviewerAlternativeContact),
			"reviewer_telegram_link":             reviewerTelegramLink,
			"reviewer_rocketchat_link":           reviewerRocketchatLink,
			"requester_rocketchat_id":            "",
			"requester_alternative_contact":      "",
			"requester_alternative_contact_line": "",
			"my_selected_error_line":             "",
		}
		attachRequesterContacts(ctx, queries, row.RequesterUserID, row.RequesterS21Login, ToString(payload["language"]), updates)

		if !isOwn && !prrClosed && !projectStillInReviews {
			_ = queries.CloseReviewRequestByID(ctx, id)
			return "", updates, nil
		}

		if !isOwn && !prrClosed && projectStillInReviews {
			resp, incErr := queries.MarkReviewRequestNegotiatingAndIncrementResponses(ctx, db.MarkReviewRequestNegotiatingAndIncrementResponsesParams{
				ID:                        id,
				NegotiatingReviewerUserID: pgtype.Int8{Int64: acc.ID, Valid: true},
				NegotiatingReviewerS21Login: pgtype.Text{
					String: acc.S21Login,
					Valid:  strings.TrimSpace(acc.S21Login) != "",
				},
				NegotiatingReviewerTelegramUsername: pgtype.Text{
					String: reviewerTelegramUsername,
					Valid:  strings.TrimSpace(reviewerTelegramUsername) != "",
				},
				NegotiatingReviewerRocketchatID: pgtype.Text{
					String: reviewerRocketchatID,
					Valid:  strings.TrimSpace(reviewerRocketchatID) != "",
				},
				NegotiatingReviewerAlternativeContact: pgtype.Text{
					String: reviewerAlternativeContact,
					Valid:  strings.TrimSpace(reviewerAlternativeContact) != "",
				},
			})
				switch incErr {
				case nil:
					updates["response_count"] = int(resp.ResponseCount)
					updates["selected_prr_status"] = string(resp.Status)
					updates["prr_status_label"] = statusLabel(string(resp.Status), ToString(payload["language"]))
					notifyReviewRequestOwner(
					ctx,
					queries,
					log,
					row.RequesterUserID,
					id,
					acc.S21Login,
					reviewerUsername,
					reviewerRocketchatID,
					reviewerAlternativeContact,
						reviewerLevel,
						row.ProjectName,
					)
				case pgx.ErrNoRows:
					updates["prr_closed"] = true
				default:
					log.Warn("reviews: failed to mark request negotiating", "prr_id", id, "error", incErr)
				}
			}

		return "", updates, nil
	})

	registry.Register("db_get_my_active_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", emptyMyRequestList(), nil
		}

		rows, err := queries.GetMyOpenReviewRequests(ctx, acc.ID)
		if err != nil {
			log.Warn("reviews: failed to load my open requests", "user_id", userID, "account_id", acc.ID, "error", err)
			return "", emptyMyRequestList(), nil
		}

		page := max(ToInt(payload["page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateMyRequests(rows, page, reviewsPageSize)
		updates := map[string]any{
			"my_prr_count":            len(rows),
			"my_prr_page":             page,
			"my_prr_total_pages":      totalPages,
			"my_prr_has_prev_page":    hasPrev,
			"my_prr_has_next_page":    hasNext,
			"my_prr_page_caption_ru":  fmt.Sprintf("%d/%d", page, totalPages),
			"my_prr_page_caption_en":  fmt.Sprintf("%d/%d", page, totalPages),
			"my_prr_list_formatted":   formatMyRequestRows(pageItems),
			"my_prr_total_items_text": fmt.Sprintf("%d", len(rows)),
		}
		clearMyRequestVars(updates)
		for i, row := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("my_prr_id_%d", n)] = strconv.FormatInt(row.ID, 10)
			updates[fmt.Sprintf("my_prr_btn_label_%d", n)] = fmt.Sprintf("%s %s", statusEmoji(row.Status), row.ProjectName)
		}
		return "", updates, nil
	})

	registry.Register("my_prr_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["my_prr_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"my_prr_page": page}, nil
	})

	registry.Register("my_prr_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["my_prr_page"]), 1)
		total := max(ToInt(payload["my_prr_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"my_prr_page": page}, nil
	})

	registry.Register("reset_my_prr_page", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{"my_prr_page": 1}, nil
	})

	registry.Register("select_my_prr", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"selected_my_prr_id": strings.TrimSpace(ToString(payload["id"])),
		}, nil
	})

	registry.Register("db_get_selected_my_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{
				"my_selected_status":                    "CLOSED",
				"my_selected_status_label":              statusLabel("CLOSED", ToString(payload["language"])),
				"my_selected_negotiating_contact_block": "",
			}, nil
		}

		id, ok := toInt64(payload["prr_id"])
		if !ok {
			id, ok = toInt64(payload["selected_my_prr_id"])
		}
		if !ok {
			return "", nil, nil
		}

		row, err := queries.GetMyReviewRequestByID(ctx, db.GetMyReviewRequestByIDParams{
			ID:              id,
			RequesterUserID: acc.ID,
		})
		if err != nil {
			if err != pgx.ErrNoRows {
				legacyRow, legacyErr := queries.GetReviewRequestByID(ctx, id)
				if legacyErr == nil && legacyRow.RequesterUserID == acc.ID {
					return "", map[string]any{
						"selected_my_prr_id":                    strconv.FormatInt(legacyRow.ID, 10),
						"my_selected_project_name":              normalizeMarkdownEscapes(legacyRow.ProjectName),
						"my_selected_project_type":              legacyRow.ProjectType,
						"my_selected_time_description":          legacyRow.AvailabilityText,
						"my_selected_view_count":                int(legacyRow.ViewCount),
						"my_selected_response_count":            int(legacyRow.ResponseCount),
						"my_selected_status":                    string(legacyRow.Status),
						"my_selected_status_label":              statusLabel(string(legacyRow.Status), ToString(payload["language"])),
						"my_selected_negotiating_contact_block": "",
						"my_selected_error_line":                detailsLoadErrorLine(ToString(payload["language"])),
					}, nil
				}
				log.Warn("reviews: failed to load my selected prr", "user_id", userID, "account_id", acc.ID, "prr_id", id, "error", err)
				return "", map[string]any{
					"my_selected_error_line": detailsLoadErrorLine(ToString(payload["language"])),
				}, nil
			}
			return "", map[string]any{
				"my_selected_status":                    "CLOSED",
				"my_selected_status_label":              statusLabel("CLOSED", ToString(payload["language"])),
				"my_selected_negotiating_contact_block": "",
				"my_selected_error_line":                "",
			}, nil
		}

		return "", map[string]any{
			"selected_my_prr_id":           strconv.FormatInt(row.ID, 10),
			"my_selected_project_name":     normalizeMarkdownEscapes(row.ProjectName),
			"my_selected_project_type":     row.ProjectType,
			"my_selected_time_description": row.AvailabilityText,
			"my_selected_view_count":       int(row.ViewCount),
			"my_selected_response_count":   int(row.ResponseCount),
			"my_selected_status":           string(row.Status),
			"my_selected_status_label":     statusLabel(string(row.Status), ToString(payload["language"])),
			"my_selected_negotiating_contact_block": buildNegotiatingContactBlock(
				ToString(payload["language"]),
				strings.TrimSpace(row.NegotiatingReviewerS21Login),
				sanitizeTelegramUsername(row.NegotiatingReviewerTelegramUsername),
				strings.TrimSpace(row.NegotiatingReviewerRocketchatID),
				strings.TrimSpace(row.NegotiatingReviewerAlternativeContact),
			),
			"my_selected_error_line": "",
		}, nil
	})

	registry.Register("pause_my_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusPAUSED, log), nil
	})

	registry.Register("resume_my_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusSEARCHING, log), nil
	})

	registry.Register("close_my_prr", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusCLOSED, log), nil
	})

	registry.Register("prepare_prr_project_filters", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		knownProjects := parseAvailableProjects(payload["available_projects"])
		selected := parseStringSet(payload["filter_project_ids"])
		selectedCampuses := parseStringSet(payload["filter_campus_ids"])
		lang := ToString(payload["language"])
		mode := normalizeProjectFilterMode(ToString(payload["project_filter_mode"]))
		searchQuery := strings.TrimSpace(ToString(payload["project_filter_query"]))
		page := max(ToInt(payload["page"]), 1)

		var (
			candidates []projectFilterCandidate
			totalPages int
			hasPrev    bool
			hasNext    bool
			loadErr    error
		)

		switch mode {
		case projectFilterModeCourse:
			candidates, page, totalPages, hasPrev, hasNext, loadErr = loadCourseFilterCandidates(ctx, queries, searchQuery, selected, page)
		case projectFilterModeNode:
			candidates, page, totalPages, hasPrev, hasNext, loadErr = loadNodeFilterCandidates(ctx, queries, searchQuery, selected, page)
		default:
			candidates, page, totalPages, hasPrev, hasNext, loadErr = loadProjectFilterCandidates(ctx, queries, searchQuery, selected, page)
		}
		if loadErr != nil {
			log.Warn("reviews: failed to load catalog filter candidates", "mode", mode, "query", searchQuery, "error", loadErr)
		}

		updates := map[string]any{
			"project_filter_page":            page,
			"project_filter_total_pages":     totalPages,
			"project_filter_has_prev_page":   hasPrev,
			"project_filter_has_next_page":   hasNext,
			"project_filter_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"project_filter_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"project_filter_list_formatted":  formatProjectFilterCandidates(candidates, selected, lang),
			"current_project_filters_text":   projectFilterTextWithCatalog(ctx, queries, selected, knownProjects, lang),
			"project_filter_mode":            mode,
			"project_filter_mode_text":       modeTitle(mode, lang),
			"project_filter_query":           searchQuery,
			"project_filter_query_text":      nonEmpty(searchQuery, "—"),
			"project_filter_mode_projects":   modeButtonLabel(projectFilterModeProject, mode, lang),
			"project_filter_mode_courses":    modeButtonLabel(projectFilterModeCourse, mode, lang),
			"project_filter_mode_nodes":      modeButtonLabel(projectFilterModeNode, mode, lang),
			"current_campus_filters_text":    campusFilterText(ctx, queries, selectedCampuses, lang),
		}
		clearProjectFilterVars(updates)
		for i, candidate := range candidates {
			n := i + 1
			check := "⬜"
			if isFilterCandidateSelected(selected, candidate.ProjectIDs) {
				check = "✅"
			}
			updates[fmt.Sprintf("project_filter_id_%d", n)] = candidate.ButtonID
			updates[fmt.Sprintf("project_filter_btn_label_%d", n)] = fmt.Sprintf("%s %s", check, candidate.Label)
		}
		return "", updates, nil
	})

	registry.Register("toggle_project_filter", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id := strings.TrimSpace(ToString(payload["id"]))
		if id == "" {
			return "", nil, nil
		}

		projectIDs, err := resolveFilterSelectionProjectIDs(ctx, queries, id)
		if err != nil {
			log.Warn("reviews: failed to resolve filter candidate", "id", id, "error", err)
			return "", nil, nil
		}
		if len(projectIDs) == 0 {
			return "", nil, nil
		}

		set := parseStringSet(payload["filter_project_ids"])
		allSelected := true
		for _, projectID := range projectIDs {
			key := strconv.FormatInt(projectID, 10)
			if !set[key] {
				allSelected = false
				break
			}
		}

		if allSelected {
			for _, projectID := range projectIDs {
				delete(set, strconv.FormatInt(projectID, 10))
			}
		} else {
			for _, projectID := range projectIDs {
				set[strconv.FormatInt(projectID, 10)] = true
			}
		}
		return "", map[string]any{
			"filter_project_ids": encodeStringSet(set),
		}, nil
	})

	registry.Register("prr_project_filter_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["project_filter_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"project_filter_page": page}, nil
	})

	registry.Register("prr_project_filter_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["project_filter_page"]), 1)
		total := max(ToInt(payload["project_filter_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"project_filter_page": page}, nil
	})

	registry.Register("set_project_filter_mode_projects", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"project_filter_mode": projectFilterModeProject,
			"project_filter_page": 1,
		}, nil
	})

	registry.Register("set_project_filter_mode_courses", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"project_filter_mode": projectFilterModeCourse,
			"project_filter_page": 1,
		}, nil
	})

	registry.Register("set_project_filter_mode_nodes", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"project_filter_mode": projectFilterModeNode,
			"project_filter_page": 1,
		}, nil
	})

	registry.Register("prepare_prr_campus_filters", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		selected := parseStringSet(payload["filter_campus_ids"])
		lang := ToString(payload["language"])
		page := max(ToInt(payload["page"]), 1)

		items, page, totalPages, hasPrev, hasNext, err := loadCampusFilterCandidates(ctx, queries, selected, page)
		if err != nil {
			log.Warn("reviews: failed to load campus filter candidates", "error", err)
		}

		updates := map[string]any{
			"campus_filter_page":            page,
			"campus_filter_total_pages":     totalPages,
			"campus_filter_has_prev_page":   hasPrev,
			"campus_filter_has_next_page":   hasNext,
			"campus_filter_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"campus_filter_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"campus_filter_list_formatted":  formatCampusFilterCandidates(items, selected, lang),
			"current_campus_filters_text":   campusFilterText(ctx, queries, selected, lang),
		}
		clearCampusFilterVars(updates)
		for i, item := range items {
			n := i + 1
			check := "⬜"
			if selected[item.ID] {
				check = "✅"
			}
			updates[fmt.Sprintf("campus_filter_id_%d", n)] = item.ID
			updates[fmt.Sprintf("campus_filter_btn_label_%d", n)] = fmt.Sprintf("%s %s", check, item.Label)
		}
		return "", updates, nil
	})

	registry.Register("toggle_campus_filter", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id := strings.TrimSpace(ToString(payload["id"]))
		if id == "" {
			return "", nil, nil
		}

		set := parseStringSet(payload["filter_campus_ids"])
		if set[id] {
			delete(set, id)
		} else {
			set[id] = true
		}
		return "", map[string]any{
			"filter_campus_ids": encodeStringSet(set),
		}, nil
	})

	registry.Register("prr_campus_filter_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["campus_filter_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"campus_filter_page": page}, nil
	})

	registry.Register("prr_campus_filter_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["campus_filter_page"]), 1)
		total := max(ToInt(payload["campus_filter_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"campus_filter_page": page}, nil
	})

	registry.Register("clear_campus_filters", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"filter_campus_ids":  []any{},
			"campus_filter_page": 1,
		}, nil
	})

	registry.Register("save_project_filter_query", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		query := strings.TrimSpace(ToString(payload["last_input"]))
		query = TrimRunes(query, 120)
		return "", map[string]any{
			"project_filter_query": query,
			"project_filter_page":  1,
		}, nil
	})

	registry.Register("clear_project_filter_query", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"project_filter_query": "",
			"project_filter_page":  1,
		}, nil
	})

	registry.Register("reset_project_filters", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"filter_project_ids":   []any{},
			"filter_campus_ids":    []any{},
			"project_filter_page":  1,
			"campus_filter_page":   1,
			"project_filter_query": "",
			"project_filter_mode":  projectFilterModeProject,
		}, nil
	})
}

func getTelegramAccount(ctx context.Context, queries db.Querier, userID int64) (db.UserAccount, error) {
	return queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
}

func defaultReviewContext(payload map[string]any) map[string]any {
	return map[string]any{
		"available_projects_count": len(parseAvailableProjects(payload["available_projects"])),
	}
}

func parseAvailableProjects(raw any) []reviewProject {
	switch v := raw.(type) {
	case []reviewProject:
		return append([]reviewProject{}, v...)
	case []any:
		out := make([]reviewProject, 0, len(v))
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(ToString(obj["id"]))
			if id == "" {
				continue
			}
			out = append(out, reviewProject{
				ID:   id,
				Name: defaultString(obj["name"], id),
				Type: defaultString(obj["type"], "INDIVIDUAL"),
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	default:
		return nil
	}
}

func encodeReviewProjects(items []reviewProject) []any {
	out := make([]any, 0, len(items))
	for _, p := range items {
		out = append(out, map[string]any{
			"id":   p.ID,
			"name": p.Name,
			"type": p.Type,
		})
	}
	return out
}

func paginateProjects(items []reviewProject, page, size int) ([]reviewProject, int, int, bool, bool) {
	totalPages := pagesCount(len(items), size)
	page = clampPage(page, totalPages)
	start := (page - 1) * size
	end := min(start+size, len(items))
	if start >= len(items) {
		return []reviewProject{}, page, totalPages, page > 1, false
	}
	return items[start:end], page, totalPages, page > 1, page < totalPages
}

func paginateProjectGroups(items []reviewProjectGroup, page, size int) ([]reviewProjectGroup, int, int, bool, bool) {
	totalPages := pagesCount(len(items), size)
	page = clampPage(page, totalPages)
	start := (page - 1) * size
	end := min(start+size, len(items))
	if start >= len(items) {
		return []reviewProjectGroup{}, page, totalPages, page > 1, false
	}
	return items[start:end], page, totalPages, page > 1, page < totalPages
}

func paginateProjectRequests(items []db.GetOpenReviewRequestsByProjectRow, page, size int) ([]db.GetOpenReviewRequestsByProjectRow, int, int, bool, bool) {
	totalPages := pagesCount(len(items), size)
	page = clampPage(page, totalPages)
	start := (page - 1) * size
	end := min(start+size, len(items))
	if start >= len(items) {
		return []db.GetOpenReviewRequestsByProjectRow{}, page, totalPages, page > 1, false
	}
	return items[start:end], page, totalPages, page > 1, page < totalPages
}

func paginateMyRequests(items []db.GetMyOpenReviewRequestsRow, page, size int) ([]db.GetMyOpenReviewRequestsRow, int, int, bool, bool) {
	totalPages := pagesCount(len(items), size)
	page = clampPage(page, totalPages)
	start := (page - 1) * size
	end := min(start+size, len(items))
	if start >= len(items) {
		return []db.GetMyOpenReviewRequestsRow{}, page, totalPages, page > 1, false
	}
	return items[start:end], page, totalPages, page > 1, page < totalPages
}

func paginateSlice[T any](items []T, page, size int) ([]T, int, int, bool, bool) {
	totalPages := pagesCount(len(items), size)
	page = clampPage(page, totalPages)
	start := (page - 1) * size
	end := min(start+size, len(items))
	if start >= len(items) {
		return []T{}, page, totalPages, page > 1, false
	}
	return items[start:end], page, totalPages, page > 1, page < totalPages
}

func pagesCount(total, size int) int {
	if size <= 0 {
		return 1
	}
	pages := total / size
	if total%size != 0 {
		pages++
	}
	return max(pages, 1)
}

func clampPage(page, totalPages int) int {
	if page < 1 {
		return 1
	}
	if page > totalPages {
		return totalPages
	}
	return page
}

func clearProjectButtonVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("project_id_%d", i)] = ""
		updates[fmt.Sprintf("project_name_%d", i)] = ""
		updates[fmt.Sprintf("project_type_%d", i)] = ""
	}
}

func clearProjectGroupVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("project_group_id_%d", i)] = ""
		updates[fmt.Sprintf("project_group_btn_label_%d", i)] = ""
		updates[fmt.Sprintf("project_group_name_%d", i)] = ""
		updates[fmt.Sprintf("project_group_type_%d", i)] = ""
	}
}

func clearProjectRequestVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("project_prr_id_%d", i)] = ""
		updates[fmt.Sprintf("project_prr_btn_label_%d", i)] = ""
	}
}

func clearMyRequestVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("my_prr_id_%d", i)] = ""
		updates[fmt.Sprintf("my_prr_btn_label_%d", i)] = ""
	}
}

func clearProjectFilterVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("project_filter_id_%d", i)] = ""
		updates[fmt.Sprintf("project_filter_btn_label_%d", i)] = ""
	}
}

func clearCampusFilterVars(updates map[string]any) {
	for i := 1; i <= campusFilterPageSize; i++ {
		updates[fmt.Sprintf("campus_filter_id_%d", i)] = ""
		updates[fmt.Sprintf("campus_filter_btn_label_%d", i)] = ""
	}
}

func emptyProjectRequestList(payload map[string]any) map[string]any {
	updates := map[string]any{
		"project_prr_page":            1,
		"project_prr_total_pages":     1,
		"project_prr_has_prev_page":   false,
		"project_prr_has_next_page":   false,
		"project_prr_page_caption_ru": "1/1",
		"project_prr_page_caption_en": "1/1",
		"project_prr_list_formatted":  "Нет активных заявок для этого проекта.",
		"selected_project_name":       defaultString(payload["selected_project_name"], "Unknown project"),
		"selected_project_type":       defaultString(payload["selected_project_type"], "INDIVIDUAL"),
	}
	clearProjectRequestVars(updates)
	return updates
}

func emptyMyRequestList() map[string]any {
	updates := map[string]any{
		"my_prr_count":           0,
		"my_prr_page":            1,
		"my_prr_total_pages":     1,
		"my_prr_has_prev_page":   false,
		"my_prr_has_next_page":   false,
		"my_prr_page_caption_ru": "1/1",
		"my_prr_page_caption_en": "1/1",
		"my_prr_list_formatted":  "Активных заявок нет.",
	}
	clearMyRequestVars(updates)
	return updates
}

func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int64:
		return val, true
	case int32:
		return int64(val), true
	case int:
		return int64(val), true
	case float64:
		return int64(val), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func formatProjectGroups(groups []reviewProjectGroup) string {
	if len(groups) == 0 {
		return "Заявок по проектам пока не поступало."
	}
	var b strings.Builder
	for i, g := range groups {
		b.WriteString(fmt.Sprintf("%d. %s (%s) - %d заявок\n", i+1, g.Name, g.Type, g.Count))
	}
	return strings.TrimSpace(b.String())
}

func formatProjectRequestRows(rows []db.GetOpenReviewRequestsByProjectRow) string {
	if len(rows) == 0 {
		return "Нет активных заявок для этого проекта."
	}
	var b strings.Builder
	for i, r := range rows {
		b.WriteString(fmt.Sprintf("%s %d. %s, %s, %s\n", statusEmoji(r.Status), i+1, r.RequesterS21Login, nonEmpty(r.RequesterCampusName, "Unknown campus"), nonEmpty(r.AvailabilityText, "Flexible")))
	}
	return strings.TrimSpace(b.String())
}

func formatMyRequestRows(rows []db.GetMyOpenReviewRequestsRow) string {
	if len(rows) == 0 {
		return "Активных заявок нет."
	}
	var b strings.Builder
	for i, r := range rows {
		b.WriteString(fmt.Sprintf("%s %d. %s (%s)\n", statusEmoji(r.Status), i+1, r.ProjectName, nonEmpty(r.AvailabilityText, "Flexible")))
	}
	return strings.TrimSpace(b.String())
}

func detailUpdatesFromReviewRow(ctx context.Context, queries db.Querier, row db.GetReviewRequestByIDRow, lang, viewerOffset string) map[string]any {
	requesterOffset := normalizeUTCOffset(row.RequesterTimezoneOffset)
	updates := map[string]any{
		"selected_prr_id":                    strconv.FormatInt(row.ID, 10),
		"selected_prr_owner_user_id":         row.RequesterUserID,
		"selected_prr_status":                string(row.Status),
		"selected_project_id":                strconv.FormatInt(row.ProjectID, 10),
		"selected_project_name":              normalizeMarkdownEscapes(row.ProjectName),
		"selected_project_type":              row.ProjectType,
		"project_name":                       normalizeMarkdownEscapes(row.ProjectName),
		"project_type":                       row.ProjectType,
		"nickname":                           row.RequesterS21Login,
		"requester_username":                 sanitizeTelegramUsername(row.RequesterTelegramUsername),
		"requester_rocketchat_id":            "",
		"requester_alternative_contact":      "",
		"requester_alternative_contact_line": "",
		"peer_campus":                        nonEmpty(row.RequesterCampusName, "Unknown campus"),
		"peer_level":                         nonEmpty(strings.TrimSpace(ToString(row.RequesterLevel)), "0"),
		"time_description":                   nonEmpty(row.AvailabilityText, "Flexible"),
		"requester_timezone_utc_offset":      requesterOffset,
		"viewer_local_time_hint":             viewerTimezoneHint(requesterOffset, viewerOffset, lang),
		"prr_opened_at":                      formatTS(row.CreatedAt),
		"view_count":                         int(row.ViewCount),
		"response_count":                     int(row.ResponseCount),
		"prr_status_label":                   statusLabel(string(row.Status), lang),
	}
	attachRequesterContacts(ctx, queries, row.RequesterUserID, row.RequesterS21Login, lang, updates)
	return updates
}

func attachRequesterContacts(ctx context.Context, queries db.Querier, requesterUserID int64, requesterLogin, lang string, updates map[string]any) {
	if updates == nil {
		return
	}
	if requesterUserID > 0 {
		if account, accErr := queries.GetUserAccountByID(ctx, requesterUserID); accErr == nil {
			isSearchable := !account.IsSearchable.Valid || account.IsSearchable.Bool
			if account.Platform == db.EnumPlatformTelegram && isSearchable {
				updates["requester_username"] = sanitizeTelegramUsername(account.Username.String)
			}
		}
	}
	if strings.TrimSpace(requesterLogin) == "" {
		return
	}
	regUser, err := queries.GetRegisteredUserByS21Login(ctx, requesterLogin)
	if err != nil {
		return
	}
	updates["requester_rocketchat_id"] = strings.TrimSpace(regUser.RocketchatID)
	if regUser.AlternativeContact.Valid {
		contact := strings.TrimSpace(regUser.AlternativeContact.String)
		updates["requester_alternative_contact"] = contact
		updates["requester_alternative_contact_line"] = buildRequesterAlternativeContactLine(lang, contact)
	}
}

func buildRequesterAlternativeContactLine(lang, contact string) string {
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return ""
	}
	if lang == fsm.LangEn {
		return "Additional contact: " + normalizeMarkdownEscapes(contact)
	}
	return "Дополнительный контакт: " + normalizeMarkdownEscapes(contact)
}

func buildReviewerAlternativeContactLine(lang, contact string) string {
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return ""
	}
	if lang == fsm.LangEn {
		return "Additional contact: " + normalizeMarkdownEscapes(contact)
	}
	return "Дополнительный контакт: " + normalizeMarkdownEscapes(contact)
}

func detailsLoadErrorLine(lang string) string {
	if lang == fsm.LangEn {
		return "⚠️ Failed to load full request details. Please try refresh or reopen."
	}
	return "⚠️ Не удалось загрузить полные детали заявки. Попробуй обновить или открыть снова."
}

func buildNegotiatingContactBlock(lang, reviewerLogin, tgUsername, rcID, altContact string) string {
	reviewerLogin = strings.TrimSpace(reviewerLogin)
	tgUsername = sanitizeTelegramUsername(tgUsername)
	rcID = strings.TrimSpace(rcID)
	altContact = strings.TrimSpace(altContact)
	if reviewerLogin == "" && tgUsername == "" && rcID == "" && altContact == "" {
		return ""
	}

	lines := []string{}
	if lang == fsm.LangEn {
		lines = append(lines, "🤝 *In negotiation now*")
		if reviewerLogin != "" {
			lines = append(lines, "Peer: `"+normalizeMarkdownEscapes(reviewerLogin)+"`")
		}
		if tgUsername != "" {
			lines = append(lines, fmt.Sprintf("Telegram: [@%s](https://t.me/%s)", tgUsername, tgUsername))
		}
		if rcID != "" {
			lines = append(lines, fmt.Sprintf("Rocket.Chat: [open dialog](https://rocketchat-student.21-school.ru/direct/%s)", normalizeMarkdownEscapes(rcID)))
		}
		if altContact != "" {
			lines = append(lines, "Additional contact: "+normalizeMarkdownEscapes(altContact))
		}
		return "\n\n" + strings.Join(lines, "\n")
	}

	lines = append(lines, "🤝 *Сейчас в переговорах*")
	if reviewerLogin != "" {
		lines = append(lines, "Пир: `"+normalizeMarkdownEscapes(reviewerLogin)+"`")
	}
	if tgUsername != "" {
		lines = append(lines, fmt.Sprintf("Telegram: [@%s](https://t.me/%s)", tgUsername, tgUsername))
	}
	if rcID != "" {
		lines = append(lines, fmt.Sprintf("Rocket.Chat: [открыть диалог](https://rocketchat-student.21-school.ru/direct/%s)", normalizeMarkdownEscapes(rcID)))
	}
	if altContact != "" {
		lines = append(lines, "Доп. контакт: "+normalizeMarkdownEscapes(altContact))
	}
	return "\n\n" + strings.Join(lines, "\n")
}

func viewerTimezoneHint(requesterOffset, viewerOffset, lang string) string {
	requesterOffset = normalizeUTCOffset(requesterOffset)
	viewerOffset = normalizeUTCOffset(viewerOffset)

	requesterMinutes, requesterOK := parseUTCOffsetMinutes(requesterOffset)
	viewerMinutes, viewerOK := parseUTCOffsetMinutes(viewerOffset)
	if !requesterOK || !viewerOK {
		return "UTC" + viewerOffset
	}

	delta := viewerMinutes - requesterMinutes
	if delta == 0 {
		if lang == fsm.LangEn {
			return fmt.Sprintf("UTC%s (same offset)", viewerOffset)
		}
		return fmt.Sprintf("UTC%s (тот же UTC-офсет)", viewerOffset)
	}

	deltaText := formatDeltaMinutes(delta, lang)
	if lang == fsm.LangEn {
		if delta > 0 {
			return fmt.Sprintf("UTC%s (%s ahead)", viewerOffset, deltaText)
		}
		return fmt.Sprintf("UTC%s (%s behind)", viewerOffset, deltaText)
	}
	if delta > 0 {
		return fmt.Sprintf("UTC%s (вперёд на %s)", viewerOffset, deltaText)
	}
	return fmt.Sprintf("UTC%s (позади на %s)", viewerOffset, deltaText)
}

func formatTS(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return "n/a"
	}
	return ts.Time.Format("2006-01-02 15:04")
}

func notifyReviewRequestOwner(
	ctx context.Context,
	queries db.Querier,
	log *slog.Logger,
	requesterUserID int64,
	prrID int64,
	reviewerLogin string,
	reviewerUsername string,
	reviewerRocketchatID string,
	reviewerAlternativeContact string,
	reviewerLevel string,
	projectName string,
) {
	notifier, ok := fsm.NotifierFromContext(ctx)
	if !ok {
		return
	}

	account, err := queries.GetUserAccountByID(ctx, requesterUserID)
	if err != nil {
		log.Warn("reviews: cannot notify requester, account lookup failed", "requester_user_id", requesterUserID, "error", err)
		return
	}
	if account.Platform != db.EnumPlatformTelegram {
		return
	}

	chatID, err := strconv.ParseInt(strings.TrimSpace(account.ExternalID), 10, 64)
	if err != nil {
		log.Warn("reviews: cannot notify requester, invalid telegram external id", "requester_user_id", requesterUserID, "external_id", account.ExternalID, "error", err)
		return
	}

	reviewer := nonEmpty(strings.TrimSpace(reviewerLogin), "reviewer")
	reviewerUsername = normalizeTelegramUsername(reviewerUsername)
	reviewerRocketchatID = strings.TrimSpace(reviewerRocketchatID)
	reviewerAlternativeContact = strings.TrimSpace(reviewerAlternativeContact)
	project := nonEmpty(strings.TrimSpace(projectName), "project")
	displayReviewer := reviewer
	contactLabel := "💬 Написать в Telegram"
	contactURL := ""
	rcContactLabel := "💬 Написать в Rocket.Chat"
	rcContactURL := ""
	if reviewerUsername != "" {
		displayReviewer = "@" + reviewerUsername
		contactLabel = fmt.Sprintf("💬 Написать @%s", reviewerUsername)
		contactURL = "https://t.me/" + reviewerUsername
	}
	if reviewerRocketchatID != "" {
		rcContactURL = "https://rocketchat-student.21-school.ru/direct/" + reviewerRocketchatID
	}
	if strings.TrimSpace(reviewerLevel) == "" {
		reviewerLevel = "0"
	}
	safeReviewer := normalizeMarkdownEscapes(displayReviewer)
	safeReviewerLevel := normalizeMarkdownEscapes(reviewerLevel)
	contactLines := []string{}
	if reviewerUsername != "" {
		contactLines = append(contactLines, fmt.Sprintf("Telegram: [@%s](https://t.me/%s)", reviewerUsername, reviewerUsername))
	}
	if reviewerRocketchatID != "" {
		contactLines = append(contactLines, fmt.Sprintf("Rocket.Chat: [открыть диалог](https://rocketchat-student.21-school.ru/direct/%s)", normalizeMarkdownEscapes(reviewerRocketchatID)))
	}
	if reviewerAlternativeContact != "" {
		contactLines = append(contactLines, "Доп. контакт: "+normalizeMarkdownEscapes(reviewerAlternativeContact))
	}
	contactsBlock := ""
	if len(contactLines) > 0 {
		contactsBlock = "\n\n" + strings.Join(contactLines, "\n")
	}

	text := fmt.Sprintf(
		"🔔 *Твой запрос взял в работу пир!*\n\nПользователь %s (уровень %s) откликнулся на твою заявку по проекту *%s*.\n\nТвоя заявка временно *скрыта с доски* (статус: `🟡 В переговорах`), чтобы тебе не писали другие.%s\nЕсли не договоритесь, нажми «🔄 Вернуть на доску».",
		safeReviewer,
		safeReviewerLevel,
		normalizeMarkdownEscapes(project),
		contactsBlock,
	)

	buttons := [][]fsm.ButtonRender{}
	if contactURL != "" {
		buttons = append(buttons, []fsm.ButtonRender{{
			Text: contactLabel,
			URL:  contactURL,
		}})
	}
	if rcContactURL != "" {
		buttons = append(buttons, []fsm.ButtonRender{{
			Text: rcContactLabel,
			URL:  rcContactURL,
		}})
	}
	buttons = append(buttons, []fsm.ButtonRender{{
		Text: "🔄 Вернуть на доску",
		Data: fsm.BuildPRRNotifyResumeCallback(prrID),
	}})
	buttons = append(buttons, []fsm.ButtonRender{{
		Text: "❌ Закрыть заявку",
		Data: fsm.BuildPRRNotifyCloseCallback(prrID),
	}})
	buttons = append(buttons, []fsm.ButtonRender{{
		Text: "🏠 Главное меню",
		Data: fsm.BuildPRRNotifyMenuCallback(),
	}})

	if renderNotifier, richOK := fsm.RenderNotifierFromContext(ctx); richOK {
		render := &fsm.RenderObject{
			Text:    text,
			Buttons: buttons,
		}
		if err := renderNotifier.NotifyUserRender(ctx, chatID, render); err != nil {
			log.Warn("reviews: failed to send rich proposal notification", "requester_user_id", requesterUserID, "chat_id", chatID, "error", err)
		} else {
			return
		}
	}

	if err := notifier.NotifyUser(ctx, chatID, text); err != nil {
		log.Warn("reviews: failed to send proposal notification", "requester_user_id", requesterUserID, "chat_id", chatID, "error", err)
	}
}

func normalizeTelegramUsername(raw string) string {
	return sanitizeTelegramUsername(raw)
}

func sanitizeTelegramUsername(raw any) string {
	username := strings.TrimSpace(ToString(raw))
	username = strings.TrimPrefix(username, "@")
	if username == "" {
		return ""
	}

	switch strings.ToLower(username) {
	case "none", "null", "n/a", "na", "-", "_":
		return ""
	}

	if !telegramUsernamePattern.MatchString(username) {
		return ""
	}
	return username
}

func setMyRequestStatus(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
	status db.EnumReviewStatus,
	log *slog.Logger,
) map[string]any {
	acc, err := getTelegramAccount(ctx, queries, userID)
	if err != nil {
		return nil
	}
	id, ok := toInt64(payload["selected_my_prr_id"])
	if !ok {
		id, ok = toInt64(payload["id"])
	}
	if !ok {
		return nil
	}

	row, err := queries.SetReviewRequestStatus(ctx, db.SetReviewRequestStatusParams{
		ID:              id,
		RequesterUserID: acc.ID,
		Status:          status,
	})
	if err != nil {
		if err != pgx.ErrNoRows {
			log.Warn("reviews: failed to set request status", "user_id", userID, "account_id", acc.ID, "prr_id", id, "status", status, "error", err)
		}
		return nil
	}

	count, _ := queries.CountOpenReviewRequestsByUser(ctx, acc.ID)
	return map[string]any{
		"selected_my_prr_id":       strconv.FormatInt(row.ID, 10),
		"my_selected_status":       string(row.Status),
		"my_selected_status_label": statusLabel(string(row.Status), ToString(payload["language"])),
		"my_prr_count":             int(count),
	}
}

func statusEmoji(status db.EnumReviewStatus) string {
	switch status {
	case db.EnumReviewStatusSEARCHING:
		return "🟢"
	case db.EnumReviewStatusNEGOTIATING:
		return "🟡"
	case db.EnumReviewStatusPAUSED:
		return "⏸"
	case db.EnumReviewStatusCLOSED:
		return "⚫"
	default:
		return "🟢"
	}
}

func statusLabel(status, lang string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SEARCHING":
		if lang == fsm.LangEn {
			return "🟢 Searching"
		}
		return "🟢 Ищет ревьюера"
	case "NEGOTIATING":
		if lang == fsm.LangEn {
			return "🟡 Negotiating"
		}
		return "🟡 В переговорах"
	case "PAUSED":
		if lang == fsm.LangEn {
			return "⏸ Paused"
		}
		return "⏸ Приостановлен"
	case "CLOSED":
		if lang == fsm.LangEn {
			return "⚫ Closed"
		}
		return "⚫ Закрыт"
	default:
		if lang == fsm.LangEn {
			return "🟢 Searching"
		}
		return "🟢 Ищет ревьюера"
	}
}

func containsProject(items []s21.ParticipantProjectV1DTO, projectID int64) bool {
	for _, p := range items {
		if p.ID == projectID && strings.EqualFold(strings.TrimSpace(p.Status), "IN_REVIEWS") {
			return true
		}
	}
	return false
}

func zoneOffsetString(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "+00:00"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		if strings.HasPrefix(name, "+") || strings.HasPrefix(name, "-") {
			if len(name) == 5 {
				return name[:3] + ":" + name[3:]
			}
			return name
		}
		return "+00:00"
	}
	_, offset := time.Now().In(loc).Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	h := offset / 3600
	m := (offset % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, h, m)
}

func normalizeUTCOffset(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "+00:00"
	}
	upper := strings.ToUpper(raw)
	if strings.HasPrefix(upper, "UTC") {
		raw = strings.TrimSpace(raw[3:])
	}
	if strings.Contains(raw, "/") {
		return zoneOffsetString(raw)
	}
	if len(raw) == 5 && (strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-")) {
		return raw[:3] + ":" + raw[3:]
	}
	if len(raw) == 6 && (strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-")) && raw[3] == ':' {
		return raw
	}
	return zoneOffsetString(raw)
}

func parseUTCOffsetMinutes(offset string) (int, bool) {
	offset = normalizeUTCOffset(offset)
	if len(offset) != 6 || (offset[0] != '+' && offset[0] != '-') || offset[3] != ':' {
		return 0, false
	}
	hours, err := strconv.Atoi(offset[1:3])
	if err != nil {
		return 0, false
	}
	minutes, err := strconv.Atoi(offset[4:6])
	if err != nil {
		return 0, false
	}
	if hours > 14 || minutes > 59 {
		return 0, false
	}
	total := hours*60 + minutes
	if offset[0] == '-' {
		total = -total
	}
	return total, true
}

func formatDeltaMinutes(delta int, lang string) string {
	if delta < 0 {
		delta = -delta
	}
	hours := delta / 60
	minutes := delta % 60
	hoursSuffix := "ч"
	minutesSuffix := "м"
	if lang == fsm.LangEn {
		hoursSuffix = "h"
		minutesSuffix = "m"
	}
	if minutes == 0 {
		return fmt.Sprintf("%d%s", hours, hoursSuffix)
	}
	return fmt.Sprintf("%d%s %d%s", hours, hoursSuffix, minutes, minutesSuffix)
}

func parseStringSet(v any) map[string]bool {
	out := map[string]bool{}
	switch raw := v.(type) {
	case []any:
		for _, item := range raw {
			s := strings.TrimSpace(ToString(item))
			if s != "" {
				out[s] = true
			}
		}
	case []string:
		for _, item := range raw {
			s := strings.TrimSpace(item)
			if s != "" {
				out[s] = true
			}
		}
	}
	return out
}

func encodeStringSet(set map[string]bool) []any {
	keys := make([]string, 0, len(set))
	for key, enabled := range set {
		if enabled {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, key)
	}
	return out
}

func collectKnownProjects(payload map[string]any, groups []db.GetGlobalReviewProjectGroupsRow) []reviewProject {
	uniq := map[string]reviewProject{}
	for _, p := range parseAvailableProjects(payload["available_projects"]) {
		uniq[p.ID] = p
	}
	for _, g := range groups {
		id := strconv.FormatInt(g.ProjectID, 10)
		uniq[id] = reviewProject{
			ID:   id,
			Name: g.ProjectName,
			Type: g.ProjectType,
		}
	}
	out := make([]reviewProject, 0, len(uniq))
	for _, p := range uniq {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func loadProjectFilterCandidates(
	ctx context.Context,
	queries db.Querier,
	searchQuery string,
	selected map[string]bool,
	page int,
) ([]projectFilterCandidate, int, int, bool, bool, error) {
	rows, err := queries.SearchCatalogProjectsAll(ctx, searchQuery)
	if err != nil {
		return nil, 1, 1, false, false, err
	}
	if strings.TrimSpace(searchQuery) != "" && len(rows) == 0 {
		allRows, allErr := queries.SearchCatalogProjectsAll(ctx, "")
		if allErr == nil {
			filtered := make([]db.SearchCatalogProjectsAllRow, 0, len(allRows))
			for _, row := range allRows {
				if projectSearchRowMatches(row, searchQuery) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
	}

	out := make([]projectFilterCandidate, 0, len(rows))
	for _, row := range rows {
		title := nonEmpty(strings.TrimSpace(row.ProjectTitle), fmt.Sprintf("Project %d", row.ProjectID))
		details := make([]string, 0, 3)
		if strings.TrimSpace(row.ProjectCode) != "" {
			details = append(details, row.ProjectCode)
		}
		if strings.TrimSpace(row.CourseTitle) != "" {
			details = append(details, row.CourseTitle)
		}
		if strings.TrimSpace(ToString(row.NodeNames)) != "" {
			details = append(details, ToString(row.NodeNames))
		}
		label := title
		if len(details) > 0 {
			label = fmt.Sprintf("%s · %s", title, strings.Join(details, " | "))
		}

		out = append(out, projectFilterCandidate{
			ButtonID:   fmt.Sprintf("project:%d", row.ProjectID),
			Label:      label,
			ProjectIDs: []int64{row.ProjectID},
		})
	}

	sortProjectCandidatesSelectedFirst(out, selected)
	pageItems, page, totalPages, hasPrev, hasNext := paginateSlice(out, page, reviewsPageSize)
	return pageItems, page, totalPages, hasPrev, hasNext, nil
}

func loadCourseFilterCandidates(
	ctx context.Context,
	queries db.Querier,
	searchQuery string,
	selected map[string]bool,
	page int,
) ([]projectFilterCandidate, int, int, bool, bool, error) {
	rows, err := queries.SearchCatalogCourses(ctx, searchQuery)
	if err != nil {
		return nil, 1, 1, false, false, err
	}
	if strings.TrimSpace(searchQuery) != "" && len(rows) == 0 {
		allRows, allErr := queries.SearchCatalogCourses(ctx, "")
		if allErr == nil {
			filtered := make([]db.SearchCatalogCoursesRow, 0, len(allRows))
			for _, row := range allRows {
				if courseSearchRowMatches(row, searchQuery) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
	}

	out := make([]projectFilterCandidate, 0, len(rows))
	for _, row := range rows {
		projectIDs, idsErr := queries.GetCatalogProjectIDsByCourse(ctx, pgtype.Int8{
			Int64: row.ID,
			Valid: true,
		})
		if idsErr != nil {
			return nil, 1, 1, false, false, idsErr
		}

		title := nonEmpty(strings.TrimSpace(row.Title), fmt.Sprintf("Course %d", row.ID))
		label := fmt.Sprintf("%s (%d)", title, row.ProjectCount)
		if strings.TrimSpace(row.Code) != "" {
			label = fmt.Sprintf("%s · %s", title, row.Code)
		}

		out = append(out, projectFilterCandidate{
			ButtonID:   fmt.Sprintf("course:%d", row.ID),
			Label:      label,
			ProjectIDs: projectIDs,
		})
	}

	sortProjectCandidatesSelectedFirst(out, selected)
	pageItems, page, totalPages, hasPrev, hasNext := paginateSlice(out, page, reviewsPageSize)
	return pageItems, page, totalPages, hasPrev, hasNext, nil
}

func loadNodeFilterCandidates(
	ctx context.Context,
	queries db.Querier,
	searchQuery string,
	selected map[string]bool,
	page int,
) ([]projectFilterCandidate, int, int, bool, bool, error) {
	rows, err := queries.SearchCatalogNodes(ctx, searchQuery)
	if err != nil {
		return nil, 1, 1, false, false, err
	}
	if strings.TrimSpace(searchQuery) != "" && len(rows) == 0 {
		allRows, allErr := queries.SearchCatalogNodes(ctx, "")
		if allErr == nil {
			filtered := make([]db.SearchCatalogNodesRow, 0, len(allRows))
			for _, row := range allRows {
				if nodeSearchRowMatches(row, searchQuery) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
	}

	out := make([]projectFilterCandidate, 0, len(rows))
	for _, row := range rows {
		projectIDs, idsErr := queries.GetCatalogProjectIDsByNodeRecursive(ctx, row.ID)
		if idsErr != nil {
			return nil, 1, 1, false, false, idsErr
		}

		label := nonEmpty(strings.TrimSpace(row.Path), strings.TrimSpace(row.Name))
		if row.ProjectCount > 0 {
			label = fmt.Sprintf("%s (%d)", label, row.ProjectCount)
		}

		out = append(out, projectFilterCandidate{
			ButtonID:   fmt.Sprintf("node:%d", row.ID),
			Label:      label,
			ProjectIDs: projectIDs,
		})
	}

	sortProjectCandidatesSelectedFirst(out, selected)
	pageItems, page, totalPages, hasPrev, hasNext := paginateSlice(out, page, reviewsPageSize)
	return pageItems, page, totalPages, hasPrev, hasNext, nil
}

func sortProjectCandidatesSelectedFirst(items []projectFilterCandidate, selected map[string]bool) {
	sort.SliceStable(items, func(i, j int) bool {
		iSelected := isFilterCandidateSelected(selected, items[i].ProjectIDs)
		jSelected := isFilterCandidateSelected(selected, items[j].ProjectIDs)
		if iSelected != jSelected {
			return iSelected
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
}

func projectSearchRowMatches(row db.SearchCatalogProjectsAllRow, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	parts := []string{
		row.ProjectTitle,
		row.ProjectCode,
		row.CourseTitle,
		row.CourseCode,
		ToString(row.NodeNames),
	}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(strings.TrimSpace(part)), q) {
			return true
		}
	}
	return false
}

func courseSearchRowMatches(row db.SearchCatalogCoursesRow, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	parts := []string{
		row.Title,
		row.Code,
		strconv.FormatInt(row.ID, 10),
	}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(strings.TrimSpace(part)), q) {
			return true
		}
	}
	return false
}

func nodeSearchRowMatches(row db.SearchCatalogNodesRow, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	parts := []string{
		row.Name,
		row.Path,
		strconv.FormatInt(row.ID, 10),
	}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(strings.TrimSpace(part)), q) {
			return true
		}
	}
	return false
}

func isPRRControlInput(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "confirm", "retry", "cancel":
		return true
	default:
		return false
	}
}

func resolveCampusAwareTimezone(ctx context.Context, queries db.Querier, timezone string, campusID pgtype.UUID) string {
	tz := strings.TrimSpace(timezone)
	// Keep user-selected non-UTC timezone; use campus timezone only for default/empty values.
	if tz != "" && !strings.EqualFold(tz, "UTC") {
		return tz
	}
	if !campusID.Valid {
		if tz == "" {
			return "UTC"
		}
		return tz
	}

	campus, err := queries.GetCampusByID(ctx, campusID)
	if err == nil {
		campusTZ := strings.TrimSpace(campus.Timezone.String)
		if campus.Timezone.Valid && campusTZ != "" {
			return campusTZ
		}
	}

	if tz == "" {
		return "UTC"
	}
	return tz
}

func normalizeProjectFilterMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case projectFilterModeCourse:
		return projectFilterModeCourse
	case projectFilterModeNode:
		return projectFilterModeNode
	default:
		return projectFilterModeProject
	}
}

func resolveFilterSelectionProjectIDs(ctx context.Context, queries db.Querier, id string) ([]int64, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}

	switch {
	case strings.HasPrefix(id, "project:"):
		projectID, err := strconv.ParseInt(strings.TrimPrefix(id, "project:"), 10, 64)
		if err != nil {
			return nil, err
		}
		return []int64{projectID}, nil
	case strings.HasPrefix(id, "course:"):
		courseID, err := strconv.ParseInt(strings.TrimPrefix(id, "course:"), 10, 64)
		if err != nil {
			return nil, err
		}
		return queries.GetCatalogProjectIDsByCourse(ctx, pgtype.Int8{
			Int64: courseID,
			Valid: true,
		})
	case strings.HasPrefix(id, "node:"):
		nodeID, err := strconv.ParseInt(strings.TrimPrefix(id, "node:"), 10, 64)
		if err != nil {
			return nil, err
		}
		return queries.GetCatalogProjectIDsByNodeRecursive(ctx, nodeID)
	default:
		projectID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil, err
		}
		return []int64{projectID}, nil
	}
}

func isFilterCandidateSelected(selected map[string]bool, projectIDs []int64) bool {
	if len(projectIDs) == 0 {
		return false
	}
	for _, projectID := range projectIDs {
		if !selected[strconv.FormatInt(projectID, 10)] {
			return false
		}
	}
	return true
}

func formatProjectFilterCandidates(items []projectFilterCandidate, selected map[string]bool, lang string) string {
	if len(items) == 0 {
		if lang == fsm.LangEn {
			return "No matches."
		}
		return "Ничего не найдено."
	}

	var b strings.Builder
	for i, item := range items {
		mark := "⬜"
		if isFilterCandidateSelected(selected, item.ProjectIDs) {
			mark = "✅"
		}
		b.WriteString(fmt.Sprintf("%s %d. %s\n", mark, i+1, item.Label))
	}
	return strings.TrimSpace(b.String())
}

func modeButtonLabel(mode, current, lang string) string {
	check := "⬜"
	if mode == current {
		check = "✅"
	}

	label := "Projects"
	switch mode {
	case projectFilterModeCourse:
		label = "Courses"
	case projectFilterModeNode:
		label = "Nodes"
	}

	if lang != fsm.LangEn {
		switch mode {
		case projectFilterModeCourse:
			label = "Курсы"
		case projectFilterModeNode:
			label = "Узлы"
		default:
			label = "Проекты"
		}
	}

	return fmt.Sprintf("%s %s", check, label)
}

func modeTitle(mode, lang string) string {
	switch mode {
	case projectFilterModeCourse:
		if lang == fsm.LangEn {
			return "Courses"
		}
		return "Курсы"
	case projectFilterModeNode:
		if lang == fsm.LangEn {
			return "Nodes"
		}
		return "Узлы"
	default:
		if lang == fsm.LangEn {
			return "Projects"
		}
		return "Проекты"
	}
}

func loadCampusFilterCandidates(
	ctx context.Context,
	queries db.Querier,
	selected map[string]bool,
	page int,
) ([]campusFilterCandidate, int, int, bool, bool, error) {
	rows, err := queries.GetAllActiveCampuses(ctx)
	if err != nil {
		return nil, 1, 1, false, false, err
	}

	items := make([]campusFilterCandidate, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		if id == "" {
			continue
		}
		shortName := strings.TrimSpace(row.ShortName)
		fullName := strings.TrimSpace(row.FullName)
		label := nonEmpty(shortName, id)
		if fullName != "" && !strings.EqualFold(fullName, shortName) {
			label = fmt.Sprintf("%s · %s", label, fullName)
		}
		items = append(items, campusFilterCandidate{
			ID:    id,
			Label: label,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		iSelected := selected[items[i].ID]
		jSelected := selected[items[j].ID]
		if iSelected != jSelected {
			return iSelected
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})

	pageItems, page, totalPages, hasPrev, hasNext := paginateSlice(items, page, campusFilterPageSize)
	return pageItems, page, totalPages, hasPrev, hasNext, nil
}

func formatCampusFilterCandidates(items []campusFilterCandidate, selected map[string]bool, lang string) string {
	if len(items) == 0 {
		if lang == fsm.LangEn {
			return "No campuses found."
		}
		return "Кампусы не найдены."
	}

	var b strings.Builder
	for i, item := range items {
		mark := "⬜"
		if selected[item.ID] {
			mark = "✅"
		}
		b.WriteString(fmt.Sprintf("%s %d. %s\n", mark, i+1, item.Label))
	}
	return strings.TrimSpace(b.String())
}

func projectFilterTextWithCatalog(
	ctx context.Context,
	queries db.Querier,
	selected map[string]bool,
	projects []reviewProject,
	lang string,
) string {
	if len(selected) == 0 {
		if lang == fsm.LangEn {
			return "All projects"
		}
		return "Все проекты"
	}

	lookup := map[string]string{}
	for _, p := range projects {
		lookup[p.ID] = p.Name
	}

	missingIDs := make([]int64, 0)
	for id := range selected {
		if lookup[id] != "" {
			continue
		}
		if parsed, err := strconv.ParseInt(id, 10, 64); err == nil {
			missingIDs = append(missingIDs, parsed)
		}
	}

	if len(missingIDs) > 0 {
		rows, err := queries.GetCatalogProjectTitlesByIDs(ctx, missingIDs)
		if err == nil {
			for _, row := range rows {
				lookup[strconv.FormatInt(row.ID, 10)] = row.Title
			}
		}
	}

	names := make([]string, 0, len(selected))
	for id := range selected {
		name := lookup[id]
		if name == "" {
			name = id
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 3 {
		return safeInlineCodeText(fmt.Sprintf("%s +%d", strings.Join(names[:3], ", "), len(names)-3))
	}
	return safeInlineCodeText(strings.Join(names, ", "))
}

func campusFilterText(ctx context.Context, queries db.Querier, selected map[string]bool, lang string) string {
	if len(selected) == 0 {
		if lang == fsm.LangEn {
			return "All campuses"
		}
		return "Все кампусы"
	}

	rows, err := queries.GetAllActiveCampuses(ctx)
	lookup := map[string]string{}
	if err == nil {
		for _, row := range rows {
			id := uuidToString(row.ID)
			if id == "" {
				continue
			}
			lookup[id] = nonEmpty(strings.TrimSpace(row.ShortName), id)
		}
	}

	names := make([]string, 0, len(selected))
	for id := range selected {
		name := lookup[id]
		if name == "" {
			name = id
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 4 {
		return safeInlineCodeText(fmt.Sprintf("%s +%d", strings.Join(names[:4], ", "), len(names)-4))
	}
	return safeInlineCodeText(strings.Join(names, ", "))
}

func uuidToString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func safeInlineCodeText(s string) string {
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func defaultString(v any, fallback string) string {
	s := strings.TrimSpace(ToString(v))
	if s == "" {
		return fallback
	}
	return s
}

func nonEmpty(v, fallback string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return fallback
	}
	return s
}

func normalizeMarkdownEscapes(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	replacer := strings.NewReplacer(
		`\\_`, "_",
		`\\*`, "*",
		`\\[`, "[",
		"\\_", "_",
		"\\*", "*",
		"\\[", "[",
		"\\`", "`",
	)
	return replacer.Replace(s)
}

func fallbackSchoolLogin(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Init.SchoolLogin)
}

func getReviewsToken(ctx context.Context, credService *service.CredentialService, preferredLogin, fallbackLogin string) (string, error) {
	preferredLogin = strings.TrimSpace(preferredLogin)
	fallbackLogin = strings.TrimSpace(fallbackLogin)

	if preferredLogin != "" {
		token, err := credService.GetValidToken(ctx, preferredLogin)
		if err == nil {
			return token, nil
		}
		if fallbackLogin == "" || fallbackLogin == preferredLogin {
			return "", err
		}
	}

	if fallbackLogin != "" {
		return credService.GetValidToken(ctx, fallbackLogin)
	}

	return "", fmt.Errorf("no login provided for token retrieval")
}
