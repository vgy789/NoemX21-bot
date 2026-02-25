package common

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const (
	reviewsPageSize = 5
)

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

func registerReviewActions(
	registry *fsm.LogicRegistry,
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
			token, tokenErr := credService.GetValidToken(ctx, acc.S21Login)
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
								Name: strings.TrimSpace(p.Title),
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
			if strings.TrimSpace(profile.Timezone) != "" {
				timezoneName = strings.TrimSpace(profile.Timezone)
			}
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
			"selected_project_name": selected.Name,
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
			"time_description": value,
			"last_input":       value,
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
			if strings.TrimSpace(profile.Timezone) != "" {
				timezoneName = strings.TrimSpace(profile.Timezone)
			}
		}
		timezoneOffset := defaultString(payload["user_timezone_formatted"], zoneOffsetString(timezoneName))

		availability := strings.TrimSpace(ToString(payload["last_input"]))
		if availability == "" {
			availability = strings.TrimSpace(ToString(payload["time_description"]))
		}
		availability = nonEmpty(TrimRunes(availability, 250), "Flexible")

		_, err = queries.CreateReviewRequest(ctx, db.CreateReviewRequestParams{
			RequesterUserID:         acc.ID,
			RequesterS21Login:       acc.S21Login,
			RequesterCampusID:       campusID,
			ProjectID:               projectID,
			ProjectName:             defaultString(payload["selected_project_name"], "Unknown project"),
			ProjectType:             defaultString(payload["selected_project_type"], "INDIVIDUAL"),
			AvailabilityText:        availability,
			RequesterTimezone:       timezoneName,
			RequesterTimezoneOffset: timezoneOffset,
			ReviewsProgressText:     defaultString(payload["reviews_progress_text"], "n/a (school API does not provide this)"),
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

		filters := parseStringSet(payload["filter_project_ids"])
		groups := make([]reviewProjectGroup, 0, len(rows))
		for _, r := range rows {
			id := strconv.FormatInt(r.ProjectID, 10)
			if len(filters) > 0 && !filters[id] {
				continue
			}
			groups = append(groups, reviewProjectGroup{
				ID:    id,
				Name:  strings.TrimSpace(r.ProjectName),
				Type:  strings.TrimSpace(r.ProjectType),
				Count: int(r.RequestsCount),
			})
		}

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
			"current_project_filters_text": projectFilterText(filters, collectKnownProjects(payload, rows), ToString(payload["language"])),
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
				updates["selected_project_type"] = defaultString(payload[fmt.Sprintf("project_group_type_%d", i)], "INDIVIDUAL")
				return "", updates, nil
			}
		}
		updates["selected_project_name"] = defaultString(payload["selected_project_name"], "Unknown project")
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
			updates["selected_project_name"] = rows[0].ProjectName
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
				if profile, profileErr := queries.GetMyProfile(ctx, acc.S21Login); profileErr == nil && strings.TrimSpace(profile.Timezone) != "" {
					viewerOffset = zoneOffsetString(strings.TrimSpace(profile.Timezone))
				}
			}
		}
		return "", detailUpdatesFromReviewRow(row, ToString(payload["language"]), viewerOffset), nil
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
		prrClosed := status == string(db.EnumReviewStatusCLOSED) || status == string(db.EnumReviewStatusPAUSED)
		projectStillInReviews := true

		if !isOwn && !prrClosed && s21Client != nil && credService != nil {
			token, tokenErr := credService.GetValidToken(ctx, acc.S21Login)
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
			"is_own_prr":               isOwn,
			"prr_closed":               prrClosed,
			"project_still_in_reviews": projectStillInReviews,
			"selected_prr_id":          strconv.FormatInt(id, 10),
		}

		if !isOwn && !prrClosed && !projectStillInReviews {
			_ = queries.CloseReviewRequestByID(ctx, id)
			return "", updates, nil
		}

		if !isOwn && !prrClosed && projectStillInReviews {
			resp, incErr := queries.MarkReviewRequestNegotiatingAndIncrementResponses(ctx, id)
			if incErr == nil {
				updates["response_count"] = int(resp.ResponseCount)
				updates["selected_prr_status"] = string(resp.Status)
				updates["prr_status_label"] = statusLabel(string(resp.Status), ToString(payload["language"]))
				notifyReviewRequestOwner(ctx, queries, log, row.RequesterUserID, acc.S21Login, row.ProjectName)
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
				"my_selected_status":       "CLOSED",
				"my_selected_status_label": statusLabel("CLOSED", ToString(payload["language"])),
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
			return "", map[string]any{
				"my_selected_status":       "CLOSED",
				"my_selected_status_label": statusLabel("CLOSED", ToString(payload["language"])),
			}, nil
		}

		return "", map[string]any{
			"selected_my_prr_id":           strconv.FormatInt(row.ID, 10),
			"my_selected_project_name":     row.ProjectName,
			"my_selected_project_type":     row.ProjectType,
			"my_selected_time_description": row.AvailabilityText,
			"my_selected_view_count":       int(row.ViewCount),
			"my_selected_response_count":   int(row.ResponseCount),
			"my_selected_status":           string(row.Status),
			"my_selected_status_label":     statusLabel(string(row.Status), ToString(payload["language"])),
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
		rows, err := queries.GetGlobalReviewProjectGroups(ctx)
		if err != nil {
			log.Warn("reviews: failed to load filter project groups", "error", err)
			rows = nil
		}

		projects := collectKnownProjects(payload, rows)
		selected := parseStringSet(payload["filter_project_ids"])

		page := max(ToInt(payload["page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateProjects(projects, page, reviewsPageSize)

		updates := map[string]any{
			"project_filter_page":            page,
			"project_filter_total_pages":     totalPages,
			"project_filter_has_prev_page":   hasPrev,
			"project_filter_has_next_page":   hasNext,
			"project_filter_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"project_filter_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"project_filter_list_formatted":  formatProjectFilterList(pageItems, selected),
			"current_project_filters_text":   projectFilterText(selected, projects, ToString(payload["language"])),
		}
		clearProjectFilterVars(updates)
		for i, project := range pageItems {
			n := i + 1
			check := "⬜"
			if selected[project.ID] {
				check = "✅"
			}
			updates[fmt.Sprintf("project_filter_id_%d", n)] = project.ID
			updates[fmt.Sprintf("project_filter_btn_label_%d", n)] = fmt.Sprintf("%s %s", check, project.Name)
		}
		return "", updates, nil
	})

	registry.Register("toggle_project_filter", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id := strings.TrimSpace(ToString(payload["id"]))
		if id == "" {
			return "", nil, nil
		}
		set := parseStringSet(payload["filter_project_ids"])
		if set[id] {
			delete(set, id)
		} else {
			set[id] = true
		}
		return "", map[string]any{"filter_project_ids": encodeStringSet(set)}, nil
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

	registry.Register("reset_project_filters", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{"filter_project_ids": []any{}}, nil
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
		"reviews_progress_text":    defaultString(payload["reviews_progress_text"], "n/a (school API does not provide this)"),
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
		return "Пока нет активных запросов."
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
		b.WriteString(fmt.Sprintf("%d. %s @%s, %s, %s\n", i+1, statusEmoji(r.Status), r.RequesterS21Login, nonEmpty(r.RequesterCampusName, "Unknown campus"), nonEmpty(r.AvailabilityText, "Flexible")))
	}
	return strings.TrimSpace(b.String())
}

func formatMyRequestRows(rows []db.GetMyOpenReviewRequestsRow) string {
	if len(rows) == 0 {
		return "Активных заявок нет."
	}
	var b strings.Builder
	for i, r := range rows {
		b.WriteString(fmt.Sprintf("%d. %s %s (%s)\n", i+1, statusEmoji(r.Status), r.ProjectName, nonEmpty(r.AvailabilityText, "Flexible")))
	}
	return strings.TrimSpace(b.String())
}

func detailUpdatesFromReviewRow(row db.GetReviewRequestByIDRow, lang, viewerOffset string) map[string]any {
	requesterOffset := normalizeUTCOffset(row.RequesterTimezoneOffset)
	return map[string]any{
		"selected_prr_id":               strconv.FormatInt(row.ID, 10),
		"selected_prr_owner_user_id":    row.RequesterUserID,
		"selected_prr_status":           string(row.Status),
		"selected_project_id":           strconv.FormatInt(row.ProjectID, 10),
		"selected_project_name":         row.ProjectName,
		"selected_project_type":         row.ProjectType,
		"project_name":                  row.ProjectName,
		"project_type":                  row.ProjectType,
		"nickname":                      row.RequesterS21Login,
		"peer_campus":                   nonEmpty(row.RequesterCampusName, "Unknown campus"),
		"peer_level":                    nonEmpty(strings.TrimSpace(ToString(row.RequesterLevel)), "0"),
		"time_description":              nonEmpty(row.AvailabilityText, "Flexible"),
		"requester_timezone_utc_offset": requesterOffset,
		"viewer_local_time_hint":        viewerTimezoneHint(requesterOffset, viewerOffset, lang),
		"prr_opened_at":                 formatTS(row.CreatedAt),
		"view_count":                    int(row.ViewCount),
		"response_count":                int(row.ResponseCount),
		"prr_status_label":              statusLabel(string(row.Status), lang),
		"reviews_progress_text":         nonEmpty(row.ReviewsProgressText, "n/a (school API does not provide this)"),
	}
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
	reviewerLogin string,
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
	project := nonEmpty(strings.TrimSpace(projectName), "project")
	text := fmt.Sprintf("💬 Новый отклик на запрос PRR.\nПроверяющий: @%s\nПроект: %s", reviewer, project)
	if err := notifier.NotifyUser(ctx, chatID, text); err != nil {
		log.Warn("reviews: failed to send proposal notification", "requester_user_id", requesterUserID, "chat_id", chatID, "error", err)
	}
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

func formatProjectFilterList(items []reviewProject, selected map[string]bool) string {
	if len(items) == 0 {
		return "Нет доступных проектов."
	}
	var b strings.Builder
	for i, p := range items {
		mark := "⬜"
		if selected[p.ID] {
			mark = "✅"
		}
		b.WriteString(fmt.Sprintf("%d. %s %s (%s)\n", i+1, mark, p.Name, p.Type))
	}
	return strings.TrimSpace(b.String())
}

func projectFilterText(selected map[string]bool, projects []reviewProject, lang string) string {
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
		return fmt.Sprintf("%s +%d", strings.Join(names[:3], ", "), len(names)-3)
	}
	return strings.Join(names, ", ")
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
