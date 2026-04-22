package common

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const teamRequestNoteMaxLen = 400

func registerTeamActions(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	log *slog.Logger,
) {
	registry.Register("db_get_my_team_summary", func(ctx context.Context, userID int64, _ map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{
				"my_team_count":            0,
				"active_team_notification": "📭 Активных запросов на поиск команды нет.",
			}, nil
		}

		count, err := queries.CountOpenTeamSearchRequestsByUser(ctx, acc.ID)
		if err != nil {
			log.Warn("team finder: failed to count open requests", "user_id", userID, "account_id", acc.ID, "error", err)
			count = 0
		}

		notification := "📭 Активных запросов на поиск команды нет."
		if count > 0 {
			notification = fmt.Sprintf("📌 Активных запросов на поиск команды: %d", count)
		}
		return "", map[string]any{
			"my_team_count":            int(count),
			"active_team_notification": notification,
		}, nil
	})

	registry.Register("school_api_get_participant_registered_group_projects", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", defaultTeamContext(payload), nil
		}

		projects := make([]reviewProject, 0, 8)
		if s21Client != nil && credService != nil {
			token, tokenErr := getReviewsToken(ctx, credService, acc.S21Login, fallbackSchoolLogin(cfg))
			if tokenErr != nil {
				log.Warn("team finder: failed to get valid token", "user_id", userID, "login", acc.S21Login, "error", tokenErr)
			} else {
				resp, apiErr := s21Client.GetParticipantProjects(ctx, token, acc.S21Login, 1000, 0, "REGISTERED")
				if apiErr != nil {
					log.Warn("team finder: failed to get REGISTERED projects", "user_id", userID, "login", acc.S21Login, "error", apiErr)
				} else if resp != nil {
					for _, p := range resp.Projects {
						if strings.EqualFold(strings.TrimSpace(p.Status), "REGISTERED") && strings.EqualFold(strings.TrimSpace(p.Type), "GROUP") {
							projects = append(projects, reviewProject{
								ID:   strconv.FormatInt(p.ID, 10),
								Name: normalizeMarkdownEscapes(strings.TrimSpace(p.Title)),
								Type: nonEmpty(strings.TrimSpace(p.Type), "GROUP"),
							})
						}
					}
				}
			}
		}

		if len(projects) == 0 {
			projects = parseAvailableProjects(payload["team_available_projects"])
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

		updates := defaultTeamContext(payload)
		updates["team_available_projects"] = encodeReviewProjects(projects)
		updates["team_available_projects_count"] = len(projects)
		updates["my_s21login"] = acc.S21Login
		updates["my_campus"] = campusName
		updates["my_level"] = level
		updates["user_timezone_name"] = timezoneName
		updates["user_timezone_formatted"] = timezoneOffset
		return "", updates, nil
	})

	registry.Register("prepare_available_projects_for_team", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		projects := parseAvailableProjects(payload["team_available_projects"])
		page := max(ToInt(payload["create_team_projects_page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateProjects(projects, page, reviewsPageSize)

		updates := map[string]any{
			"team_available_projects_count":           len(projects),
			"team_available_projects_has_prev_page":   hasPrev,
			"team_available_projects_has_next_page":   hasNext,
			"team_available_projects_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"team_available_projects_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"team_available_projects_list_hint":       fmt.Sprintf("Всего доступно: %d", len(projects)),
			"create_team_projects_page":               page,
		}

		clearTeamProjectButtonVars(updates)
		for i, p := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("team_project_id_%d", n)] = p.ID
			updates[fmt.Sprintf("team_project_name_%d", n)] = p.Name
			updates[fmt.Sprintf("team_project_type_%d", n)] = p.Type
		}
		return "", updates, nil
	})

	registry.Register("team_available_projects_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["create_team_projects_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"create_team_projects_page": page}, nil
	})

	registry.Register("team_available_projects_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["create_team_projects_page"]), 1)
		totalPages := pagesCount(len(parseAvailableProjects(payload["team_available_projects"])), reviewsPageSize)
		if page < totalPages {
			page++
		}
		return "", map[string]any{"create_team_projects_page": page}, nil
	})

	registry.Register("select_team_create_project", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		selectedID := strings.TrimSpace(ToString(payload["id"]))
		if selectedID == "" {
			return "", nil, nil
		}

		projects := parseAvailableProjects(payload["team_available_projects"])
		var selected reviewProject
		for _, p := range projects {
			if p.ID == selectedID {
				selected = p
				break
			}
		}
		if selected.ID == "" {
			for i := 1; i <= reviewsPageSize; i++ {
				if strings.TrimSpace(ToString(payload[fmt.Sprintf("team_project_id_%d", i)])) == selectedID {
					selected = reviewProject{
						ID:   selectedID,
						Name: strings.TrimSpace(ToString(payload[fmt.Sprintf("team_project_name_%d", i)])),
						Type: nonEmpty(strings.TrimSpace(ToString(payload[fmt.Sprintf("team_project_type_%d", i)])), "GROUP"),
					}
					break
				}
			}
		}
		if selected.ID == "" {
			selected = reviewProject{
				ID:   selectedID,
				Name: defaultString(payload["selected_team_project_name"], "Unknown project"),
				Type: defaultString(payload["selected_team_project_type"], "GROUP"),
			}
		}

		return "", map[string]any{
			"selected_team_project_id":   selected.ID,
			"selected_team_project_name": normalizeMarkdownEscapes(selected.Name),
			"selected_team_project_type": selected.Type,
		}, nil
	})

	registry.Register("db_check_open_team_for_selected_project", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{"has_open_team_for_selected_project": false}, nil
		}
		projectID, ok := toInt64(payload["selected_team_project_id"])
		if !ok {
			return "", map[string]any{"has_open_team_for_selected_project": false}, nil
		}
		exists, err := queries.ExistsOpenTeamSearchRequestByUserAndProject(ctx, db.ExistsOpenTeamSearchRequestByUserAndProjectParams{
			RequesterUserID: acc.ID,
			ProjectID:       projectID,
		})
		if err != nil {
			log.Warn("team finder: failed to check duplicate request", "user_id", userID, "account_id", acc.ID, "project_id", projectID, "error", err)
			return "", map[string]any{"has_open_team_for_selected_project": false}, nil
		}
		return "", map[string]any{"has_open_team_for_selected_project": exists}, nil
	})

	registry.Register("save_team_start_input", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		value := strings.TrimSpace(ToString(payload["last_input"]))
		if value == "" {
			value = "Flexible"
		}
		value = TrimRunes(value, 250)
		return "", map[string]any{
			"team_planned_start_text": value,
		}, nil
	})

	registry.Register("save_team_note_input", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		value := strings.TrimSpace(ToString(payload["last_input"]))
		if value == "" {
			value = "Ищу команду"
		}
		value = TrimRunes(value, teamRequestNoteMaxLen)
		return "", map[string]any{
			"team_request_note_text": value,
		}, nil
	})

	registry.Register("db_save_team_search_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{"save_team_conflict_same_project": false}, nil
		}

		projectID, ok := toInt64(payload["selected_team_project_id"])
		if !ok {
			return "", map[string]any{"save_team_conflict_same_project": false}, nil
		}

		duplicate, err := queries.ExistsOpenTeamSearchRequestByUserAndProject(ctx, db.ExistsOpenTeamSearchRequestByUserAndProjectParams{
			RequesterUserID: acc.ID,
			ProjectID:       projectID,
		})
		if err == nil && duplicate {
			return "", map[string]any{"save_team_conflict_same_project": true}, nil
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

		plannedStart := strings.TrimSpace(ToString(payload["team_planned_start_text"]))
		if plannedStart == "" {
			candidate := strings.TrimSpace(ToString(payload["last_input"]))
			if !isPRRControlInput(candidate) {
				plannedStart = candidate
			}
		}
		plannedStart = nonEmpty(TrimRunes(plannedStart, 250), "Flexible")

		note := strings.TrimSpace(ToString(payload["team_request_note_text"]))
		note = nonEmpty(TrimRunes(note, teamRequestNoteMaxLen), "Ищу команду")

		created, err := queries.CreateTeamSearchRequest(ctx, db.CreateTeamSearchRequestParams{
			RequesterUserID:         acc.ID,
			RequesterS21Login:       acc.S21Login,
			RequesterCampusID:       campusID,
			ProjectID:               projectID,
			ProjectName:             normalizeMarkdownEscapes(defaultString(payload["selected_team_project_name"], "Unknown project")),
			ProjectType:             defaultString(payload["selected_team_project_type"], "GROUP"),
			PlannedStartText:        plannedStart,
			RequestNoteText:         note,
			RequesterTimezone:       timezoneName,
			RequesterTimezoneOffset: timezoneOffset,
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "uq_team_search_requests_open_per_project") {
				return "", map[string]any{"save_team_conflict_same_project": true}, nil
			}
			log.Warn("team finder: failed to save team request", "user_id", userID, "account_id", acc.ID, "project_id", projectID, "error", err)
			return "", map[string]any{"save_team_conflict_same_project": false}, nil
		}

		count, _ := queries.CountOpenTeamSearchRequestsByUser(ctx, acc.ID)
		if broadcaster, ok := fsm.TeamGroupBroadcasterFromContext(ctx); ok {
			if notifyErr := broadcaster.PublishTeamSearchRequest(ctx, created.ID); notifyErr != nil {
				log.Warn("team finder: failed to publish request in groups", "request_id", created.ID, "error", notifyErr)
			}
		}

		return "", map[string]any{
			"save_team_conflict_same_project": false,
			"my_team_count":                   int(count),
		}, nil
	})

	registry.Register("db_get_global_team_project_groups", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		rows, err := queries.GetGlobalTeamProjectGroups(ctx)
		if err != nil {
			log.Warn("team finder: failed to load project groups", "error", err)
			rows = nil
		}

		groups := make([]reviewProjectGroup, 0, len(rows))
		for _, row := range rows {
			groups = append(groups, reviewProjectGroup{
				ID:    strconv.FormatInt(row.ProjectID, 10),
				Name:  strings.TrimSpace(row.ProjectName),
				Type:  strings.TrimSpace(row.ProjectType),
				Count: int(row.RequestsCount),
			})
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
		})

		page := max(ToInt(payload["team_board_page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateProjectGroups(groups, page, reviewsPageSize)
		updates := map[string]any{
			"team_board_page":               page,
			"team_board_total_pages":        totalPages,
			"team_board_has_prev_page":      hasPrev,
			"team_board_has_next_page":      hasNext,
			"team_board_page_caption_ru":    fmt.Sprintf("%d/%d", page, totalPages),
			"team_board_page_caption_en":    fmt.Sprintf("%d/%d", page, totalPages),
			"team_project_groups_formatted": formatTeamProjectGroups(pageItems),
		}
		clearTeamProjectGroupVars(updates)
		for i, g := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("team_project_group_id_%d", n)] = g.ID
			updates[fmt.Sprintf("team_project_group_btn_label_%d", n)] = fmt.Sprintf("👥 %s · %d", g.Name, g.Count)
			updates[fmt.Sprintf("team_project_group_name_%d", n)] = g.Name
			updates[fmt.Sprintf("team_project_group_type_%d", n)] = g.Type
		}
		return "", updates, nil
	})

	registry.Register("global_team_board_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["team_board_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"team_board_page": page}, nil
	})

	registry.Register("global_team_board_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["team_board_page"]), 1)
		total := max(ToInt(payload["team_board_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"team_board_page": page}, nil
	})

	registry.Register("select_team_project_group", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		selectedID := strings.TrimSpace(ToString(payload["id"]))
		if selectedID == "" {
			return "", nil, nil
		}
		updates := map[string]any{
			"selected_team_project_id":   selectedID,
			"selected_team_project_name": defaultString(payload["selected_team_project_name"], "Unknown project"),
			"selected_team_project_type": defaultString(payload["selected_team_project_type"], "GROUP"),
		}
		for i := 1; i <= reviewsPageSize; i++ {
			if strings.TrimSpace(ToString(payload[fmt.Sprintf("team_project_group_id_%d", i)])) == selectedID {
				updates["selected_team_project_name"] = defaultString(payload[fmt.Sprintf("team_project_group_name_%d", i)], updates["selected_team_project_name"].(string))
				updates["selected_team_project_type"] = defaultString(payload[fmt.Sprintf("team_project_group_type_%d", i)], "GROUP")
				break
			}
		}
		updates["selected_team_project_name"] = normalizeMarkdownEscapes(ToString(updates["selected_team_project_name"]))
		updates["selected_team_project_name_md"] = projectNameMarkdown(ToString(updates["selected_team_project_name"]))
		return "", updates, nil
	})

	registry.Register("db_get_team_requests_for_selected_project", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		projectID, ok := toInt64(payload["selected_team_project_id"])
		if !ok {
			return "", emptyTeamProjectRequestList(payload), nil
		}

		rows, err := queries.GetOpenTeamSearchRequestsByProject(ctx, projectID)
		if err != nil {
			log.Warn("team finder: failed to load requests for project", "project_id", projectID, "error", err)
			return "", emptyTeamProjectRequestList(payload), nil
		}

		page := max(ToInt(payload["project_team_page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateSlice(rows, page, reviewsPageSize)
		updates := map[string]any{
			"project_team_page":            page,
			"project_team_total_pages":     totalPages,
			"project_team_has_prev_page":   hasPrev,
			"project_team_has_next_page":   hasNext,
			"project_team_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"project_team_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"project_team_list_formatted":  formatProjectTeamRequestRows(pageItems),
			"selected_team_project_name":   normalizeMarkdownEscapes(defaultString(payload["selected_team_project_name"], "Unknown project")),
			"selected_team_project_type":   defaultString(payload["selected_team_project_type"], "GROUP"),
		}
		updates["selected_team_project_name_md"] = projectNameMarkdown(ToString(updates["selected_team_project_name"]))
		clearTeamProjectRequestVars(updates)
		for i, row := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("project_team_id_%d", n)] = strconv.FormatInt(row.ID, 10)
			updates[fmt.Sprintf("project_team_btn_label_%d", n)] = fmt.Sprintf("%s %s", statusEmoji(row.Status), row.RequesterS21Login)
		}
		return "", updates, nil
	})

	registry.Register("project_team_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["project_team_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"project_team_page": page}, nil
	})

	registry.Register("project_team_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["project_team_page"]), 1)
		total := max(ToInt(payload["project_team_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"project_team_page": page}, nil
	})

	registry.Register("select_team_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		id, ok := toInt64(payload["id"])
		if !ok {
			return "", nil, nil
		}
		row, err := queries.GetTeamSearchRequestByID(ctx, id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return "", map[string]any{
					"selected_team_request_id":       strconv.FormatInt(id, 10),
					"selected_team_request_status":   "CLOSED",
					"team_status_label":              teamStatusLabel("CLOSED", ToString(payload["language"])),
					"project_still_registered_group": false,
				}, nil
			}
			log.Warn("team finder: failed to load selected request", "request_id", id, "error", err)
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
		return "", detailUpdatesFromTeamRow(ctx, queries, row, ToString(payload["language"]), viewerOffset), nil
	})

	registry.Register("db_increment_team_view_counter", func(ctx context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id, ok := toInt64(payload["team_request_id"])
		if !ok {
			id, ok = toInt64(payload["selected_team_request_id"])
		}
		if !ok {
			return "", nil, nil
		}
		count, err := queries.IncrementTeamSearchRequestViewCount(ctx, id)
		if err != nil {
			log.Warn("team finder: failed to increment view counter", "request_id", id, "error", err)
			return "", nil, nil
		}
		return "", map[string]any{"view_count": int(count)}, nil
	})

	registry.Register("validate_and_propose_team_join", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{"is_own_team_request": false, "team_request_closed": true, "project_still_registered_group": true}, nil
		}

		id, ok := toInt64(payload["team_request_id"])
		if !ok {
			id, ok = toInt64(payload["selected_team_request_id"])
		}
		if !ok {
			return "", map[string]any{"is_own_team_request": false, "team_request_closed": true, "project_still_registered_group": true}, nil
		}

		row, err := queries.GetTeamSearchRequestByID(ctx, id)
		if err != nil {
			return "", map[string]any{"is_own_team_request": false, "team_request_closed": true, "project_still_registered_group": false}, nil
		}

		isOwn := row.RequesterUserID == acc.ID
		status := strings.TrimSpace(string(row.Status))
		requestClosed := status == string(db.EnumReviewStatusCLOSED) ||
			status == string(db.EnumReviewStatusWITHDRAWN) ||
			status == string(db.EnumReviewStatusPAUSED) ||
			status == string(db.EnumReviewStatusNEGOTIATING)
		projectStillRegisteredGroup := true

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
		reviewerRocketchatID := ""
		reviewerAlternativeContact := ""
		if regUser, regErr := queries.GetRegisteredUserByS21Login(ctx, acc.S21Login); regErr == nil {
			reviewerRocketchatID = strings.TrimSpace(regUser.RocketchatID)
			if regUser.AlternativeContact.Valid {
				reviewerAlternativeContact = strings.TrimSpace(regUser.AlternativeContact.String)
			}
		}

		if !isOwn && !requestClosed && s21Client != nil && credService != nil {
			token, tokenErr := getReviewsToken(ctx, credService, acc.S21Login, fallbackSchoolLogin(cfg))
			if tokenErr != nil {
				log.Warn("team finder: lazy-check token unavailable", "user_id", userID, "login", acc.S21Login, "error", tokenErr)
			} else {
				resp, apiErr := s21Client.GetParticipantProjects(ctx, token, row.RequesterS21Login, 1000, 0, "REGISTERED")
				if apiErr != nil {
					log.Warn("team finder: lazy-check API failed", "request_id", id, "requester_login", row.RequesterS21Login, "error", apiErr)
				} else if resp != nil {
					projectStillRegisteredGroup = containsRegisteredGroupProject(resp.Projects, row.ProjectID)
				}
			}
		}

		updates := map[string]any{
			"is_own_team_request":                isOwn,
			"team_request_closed":                requestClosed,
			"project_still_registered_group":     projectStillRegisteredGroup,
			"selected_team_request_id":           strconv.FormatInt(id, 10),
			"requester_username":                 sanitizeTelegramUsername(row.RequesterTelegramUsername),
			"requester_rocketchat_id":            "",
			"requester_alternative_contact":      "",
			"requester_alternative_contact_line": "",
		}
		attachRequesterContacts(ctx, queries, row.RequesterUserID, row.RequesterS21Login, ToString(payload["language"]), updates)

		if !isOwn && !requestClosed && !projectStillRegisteredGroup {
			_ = queries.CloseTeamSearchRequestByID(ctx, id)
			if broadcaster, ok := fsm.TeamGroupBroadcasterFromContext(ctx); ok {
				if syncErr := broadcaster.SyncTeamSearchRequestStatus(ctx, id, string(db.EnumReviewStatusCLOSED)); syncErr != nil {
					log.Warn("team finder: failed to sync closed status after lazy close", "request_id", id, "status", db.EnumReviewStatusCLOSED, "error", syncErr)
				}
			}
			return "", updates, nil
		}

		if !isOwn && !requestClosed && projectStillRegisteredGroup {
			resp, incErr := queries.MarkTeamSearchRequestNegotiatingAndIncrementResponses(ctx, db.MarkTeamSearchRequestNegotiatingAndIncrementResponsesParams{
				ID:                    id,
				NegotiatingPeerUserID: pgtype.Int8{Int64: acc.ID, Valid: true},
				NegotiatingPeerS21Login: pgtype.Text{
					String: acc.S21Login,
					Valid:  strings.TrimSpace(acc.S21Login) != "",
				},
				NegotiatingPeerTelegramUsername: pgtype.Text{
					String: reviewerTelegramUsername,
					Valid:  strings.TrimSpace(reviewerTelegramUsername) != "",
				},
				NegotiatingPeerRocketchatID: pgtype.Text{
					String: reviewerRocketchatID,
					Valid:  strings.TrimSpace(reviewerRocketchatID) != "",
				},
				NegotiatingPeerAlternativeContact: pgtype.Text{
					String: reviewerAlternativeContact,
					Valid:  strings.TrimSpace(reviewerAlternativeContact) != "",
				},
			})
			switch incErr {
			case nil:
				updates["response_count"] = int(resp.ResponseCount)
				updates["selected_team_request_status"] = string(resp.Status)
				updates["team_status_label"] = teamStatusLabel(string(resp.Status), ToString(payload["language"]))
				if broadcaster, ok := fsm.TeamGroupBroadcasterFromContext(ctx); ok {
					if syncErr := broadcaster.SyncTeamSearchRequestStatus(ctx, id, string(resp.Status)); syncErr != nil {
						log.Warn("team finder: failed to sync status in groups", "request_id", id, "status", resp.Status, "error", syncErr)
					}
				}
				notifyTeamSearchRequestOwner(
					ctx,
					queries,
					log,
					row.RequesterUserID,
					acc.S21Login,
					reviewerTelegramUsername,
					reviewerRocketchatID,
					reviewerAlternativeContact,
					reviewerLevel,
					row.ProjectName,
				)
			case pgx.ErrNoRows:
				updates["team_request_closed"] = true
			default:
				log.Warn("team finder: failed to mark request negotiating", "request_id", id, "error", incErr)
			}
		}

		return "", updates, nil
	})

	registry.Register("db_get_my_active_team_requests", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", emptyMyTeamRequestList(), nil
		}

		rows, err := queries.GetMyOpenTeamSearchRequests(ctx, acc.ID)
		if err != nil {
			log.Warn("team finder: failed to load my open requests", "user_id", userID, "account_id", acc.ID, "error", err)
			return "", emptyMyTeamRequestList(), nil
		}

		page := max(ToInt(payload["my_team_page"]), 1)
		pageItems, page, totalPages, hasPrev, hasNext := paginateSlice(rows, page, reviewsPageSize)
		updates := map[string]any{
			"my_team_count":           len(rows),
			"my_team_page":            page,
			"my_team_total_pages":     totalPages,
			"my_team_has_prev_page":   hasPrev,
			"my_team_has_next_page":   hasNext,
			"my_team_page_caption_ru": fmt.Sprintf("%d/%d", page, totalPages),
			"my_team_page_caption_en": fmt.Sprintf("%d/%d", page, totalPages),
			"my_team_list_formatted":  formatMyTeamRequestRows(pageItems),
		}
		clearMyTeamRequestVars(updates)
		for i, row := range pageItems {
			n := i + 1
			updates[fmt.Sprintf("my_team_id_%d", n)] = strconv.FormatInt(row.ID, 10)
			updates[fmt.Sprintf("my_team_btn_label_%d", n)] = fmt.Sprintf("%s %s", statusEmoji(row.Status), row.ProjectName)
		}
		return "", updates, nil
	})

	registry.Register("my_team_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["my_team_page"]), 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"my_team_page": page}, nil
	})

	registry.Register("my_team_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(ToInt(payload["my_team_page"]), 1)
		total := max(ToInt(payload["my_team_total_pages"]), 1)
		if page < total {
			page++
		}
		return "", map[string]any{"my_team_page": page}, nil
	})

	registry.Register("reset_my_team_page", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{"my_team_page": 1}, nil
	})

	registry.Register("select_my_team_request", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"selected_my_team_request_id": strings.TrimSpace(ToString(payload["id"])),
		}, nil
	})

	registry.Register("db_get_selected_my_team_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := getTelegramAccount(ctx, queries, userID)
		if err != nil {
			return "", map[string]any{
				"my_team_selected_status":                    "CLOSED",
				"my_team_selected_status_label":              teamStatusLabel("CLOSED", ToString(payload["language"])),
				"my_team_selected_negotiating_contact_block": "",
			}, nil
		}

		id, ok := toInt64(payload["selected_my_team_request_id"])
		if !ok {
			id, ok = toInt64(payload["id"])
		}
		if !ok {
			return "", nil, nil
		}

		row, err := queries.GetMyTeamSearchRequestByID(ctx, db.GetMyTeamSearchRequestByIDParams{
			ID:              id,
			RequesterUserID: acc.ID,
		})
		if err != nil {
			if err != pgx.ErrNoRows {
				log.Warn("team finder: failed to load my selected request", "user_id", userID, "account_id", acc.ID, "request_id", id, "error", err)
				return "", map[string]any{
					"my_team_selected_error_line": detailsLoadErrorLine(ToString(payload["language"])),
				}, nil
			}
			return "", map[string]any{
				"my_team_selected_status":                    "CLOSED",
				"my_team_selected_status_label":              teamStatusLabel("CLOSED", ToString(payload["language"])),
				"my_team_selected_negotiating_contact_block": "",
				"my_team_selected_error_line":                "",
			}, nil
		}

		return "", map[string]any{
			"selected_my_team_request_id":         strconv.FormatInt(row.ID, 10),
			"my_team_selected_project_name":       normalizeMarkdownEscapes(row.ProjectName),
			"my_team_selected_project_type":       row.ProjectType,
			"my_team_selected_planned_start_text": row.PlannedStartText,
			"my_team_selected_request_note_text":  row.RequestNoteText,
			"my_team_selected_view_count":         int(row.ViewCount),
			"my_team_selected_response_count":     int(row.ResponseCount),
			"my_team_selected_status":             string(row.Status),
			"my_team_selected_status_label":       teamStatusLabel(string(row.Status), ToString(payload["language"])),
			"my_team_selected_negotiating_contact_block": buildNegotiatingContactBlock(
				ToString(payload["language"]),
				strings.TrimSpace(row.NegotiatingPeerS21Login),
				sanitizeTelegramUsername(row.NegotiatingPeerTelegramUsername),
				strings.TrimSpace(row.NegotiatingPeerRocketchatID),
				strings.TrimSpace(row.NegotiatingPeerAlternativeContact),
			),
			"my_team_selected_error_line": "",
		}, nil
	})

	registry.Register("pause_my_team_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyTeamRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusPAUSED, log), nil
	})

	registry.Register("resume_my_team_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyTeamRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusSEARCHING, log), nil
	})

	registry.Register("close_my_team_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyTeamRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusCLOSED, log), nil
	})

	registry.Register("withdraw_my_team_request", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", setMyTeamRequestStatus(ctx, queries, userID, payload, db.EnumReviewStatusWITHDRAWN, log), nil
	})
}

func defaultTeamContext(payload map[string]any) map[string]any {
	return map[string]any{
		"team_available_projects_count": len(parseAvailableProjects(payload["team_available_projects"])),
	}
}

func clearTeamProjectButtonVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("team_project_id_%d", i)] = ""
		updates[fmt.Sprintf("team_project_name_%d", i)] = ""
		updates[fmt.Sprintf("team_project_type_%d", i)] = ""
	}
}

func clearTeamProjectGroupVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("team_project_group_id_%d", i)] = ""
		updates[fmt.Sprintf("team_project_group_btn_label_%d", i)] = ""
		updates[fmt.Sprintf("team_project_group_name_%d", i)] = ""
		updates[fmt.Sprintf("team_project_group_type_%d", i)] = ""
	}
}

func clearTeamProjectRequestVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("project_team_id_%d", i)] = ""
		updates[fmt.Sprintf("project_team_btn_label_%d", i)] = ""
	}
}

func clearMyTeamRequestVars(updates map[string]any) {
	for i := 1; i <= reviewsPageSize; i++ {
		updates[fmt.Sprintf("my_team_id_%d", i)] = ""
		updates[fmt.Sprintf("my_team_btn_label_%d", i)] = ""
	}
}

func emptyTeamProjectRequestList(payload map[string]any) map[string]any {
	selectedProjectName := normalizeMarkdownEscapes(defaultString(payload["selected_team_project_name"], "Unknown project"))
	updates := map[string]any{
		"project_team_page":             1,
		"project_team_total_pages":      1,
		"project_team_has_prev_page":    false,
		"project_team_has_next_page":    false,
		"project_team_page_caption_ru":  "1/1",
		"project_team_page_caption_en":  "1/1",
		"project_team_list_formatted":   "Нет активных запросов по этому проекту.",
		"selected_team_project_name":    selectedProjectName,
		"selected_team_project_name_md": projectNameMarkdown(selectedProjectName),
		"selected_team_project_type":    defaultString(payload["selected_team_project_type"], "GROUP"),
	}
	clearTeamProjectRequestVars(updates)
	return updates
}

func emptyMyTeamRequestList() map[string]any {
	updates := map[string]any{
		"my_team_count":           0,
		"my_team_page":            1,
		"my_team_total_pages":     1,
		"my_team_has_prev_page":   false,
		"my_team_has_next_page":   false,
		"my_team_page_caption_ru": "1/1",
		"my_team_page_caption_en": "1/1",
		"my_team_list_formatted":  "Активных запросов на поиск команды нет.",
	}
	clearMyTeamRequestVars(updates)
	return updates
}

func formatTeamProjectGroups(groups []reviewProjectGroup) string {
	if len(groups) == 0 {
		return "Запросов на поиск команды пока нет."
	}
	var b strings.Builder
	for i, g := range groups {
		_, _ = fmt.Fprintf(&b, "%d. %s (%s) - %d запросов\n", i+1, g.Name, g.Type, g.Count)
	}
	return strings.TrimSpace(b.String())
}

func formatProjectTeamRequestRows(rows []db.GetOpenTeamSearchRequestsByProjectRow) string {
	if len(rows) == 0 {
		return "Нет активных запросов по этому проекту."
	}
	var b strings.Builder
	for i, row := range rows {
		_, _ = fmt.Fprintf(
			&b,
			"%s %d. %s, %s, старт: %s, %s\n",
			statusEmoji(row.Status),
			i+1,
			row.RequesterS21Login,
			nonEmpty(row.RequesterCampusName, "Unknown campus"),
			nonEmpty(row.PlannedStartText, "Flexible"),
			truncateForList(nonEmpty(row.RequestNoteText, "Ищу команду"), 48),
		)
	}
	return strings.TrimSpace(b.String())
}

func formatMyTeamRequestRows(rows []db.GetMyOpenTeamSearchRequestsRow) string {
	if len(rows) == 0 {
		return "Активных запросов на поиск команды нет."
	}
	var b strings.Builder
	for i, row := range rows {
		_, _ = fmt.Fprintf(
			&b,
			"%s %d. %s (%s)\n",
			statusEmoji(row.Status),
			i+1,
			row.ProjectName,
			nonEmpty(row.PlannedStartText, "Flexible"),
		)
	}
	return strings.TrimSpace(b.String())
}

func setMyTeamRequestStatus(
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
	id, ok := toInt64(payload["selected_my_team_request_id"])
	if !ok {
		id, ok = toInt64(payload["id"])
	}
	if !ok {
		return nil
	}

	row, err := queries.SetTeamSearchRequestStatus(ctx, db.SetTeamSearchRequestStatusParams{
		ID:              id,
		RequesterUserID: acc.ID,
		Status:          status,
	})
	if err != nil {
		if err != pgx.ErrNoRows {
			log.Warn("team finder: failed to set request status", "user_id", userID, "account_id", acc.ID, "request_id", id, "status", status, "error", err)
		}
		return nil
	}

	count, _ := queries.CountOpenTeamSearchRequestsByUser(ctx, acc.ID)
	if broadcaster, ok := fsm.TeamGroupBroadcasterFromContext(ctx); ok {
		if syncErr := broadcaster.SyncTeamSearchRequestStatus(ctx, row.ID, string(row.Status)); syncErr != nil {
			log.Warn("team finder: failed to sync status in groups", "request_id", row.ID, "status", row.Status, "error", syncErr)
		}
	}
	return map[string]any{
		"selected_my_team_request_id":   strconv.FormatInt(row.ID, 10),
		"my_team_selected_status":       string(row.Status),
		"my_team_selected_status_label": teamStatusLabel(string(row.Status), ToString(payload["language"])),
		"my_team_count":                 int(count),
	}
}

func detailUpdatesFromTeamRow(ctx context.Context, queries db.Querier, row db.GetTeamSearchRequestByIDRow, lang, viewerOffset string) map[string]any {
	requesterOffset := normalizeUTCOffset(row.RequesterTimezoneOffset)
	updates := map[string]any{
		"selected_team_request_id":           strconv.FormatInt(row.ID, 10),
		"selected_team_request_status":       string(row.Status),
		"selected_team_project_id":           strconv.FormatInt(row.ProjectID, 10),
		"selected_team_project_name":         normalizeMarkdownEscapes(row.ProjectName),
		"selected_team_project_name_md":      projectNameMarkdown(normalizeMarkdownEscapes(row.ProjectName)),
		"selected_team_project_type":         row.ProjectType,
		"project_name":                       normalizeMarkdownEscapes(row.ProjectName),
		"project_type":                       row.ProjectType,
		"nickname":                           row.RequesterS21Login,
		"requester_username":                 sanitizeTelegramUsername(row.RequesterTelegramUsername),
		"requester_rocketchat_id":            "",
		"requester_alternative_contact":      "",
		"requester_alternative_contact_line": "",
		"peer_campus":                        nonEmpty(row.RequesterCampusName, "Unknown campus"),
		"peer_level":                         nonEmpty(strings.TrimSpace(ToString(row.RequesterLevel)), "0"),
		"team_planned_start_text":            nonEmpty(row.PlannedStartText, "Flexible"),
		"team_request_note_text":             nonEmpty(row.RequestNoteText, "Ищу команду"),
		"requester_timezone_utc_offset":      requesterOffset,
		"viewer_local_time_hint":             viewerTimezoneHint(requesterOffset, viewerOffset, lang),
		"team_opened_at":                     formatTS(row.CreatedAt),
		"view_count":                         int(row.ViewCount),
		"response_count":                     int(row.ResponseCount),
		"team_status_label":                  teamStatusLabel(string(row.Status), lang),
	}
	attachRequesterContacts(ctx, queries, row.RequesterUserID, row.RequesterS21Login, lang, updates)
	return updates
}

func notifyTeamSearchRequestOwner(
	ctx context.Context,
	queries db.Querier,
	log *slog.Logger,
	requesterUserID int64,
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
		log.Warn("team finder: cannot notify requester, account lookup failed", "requester_user_id", requesterUserID, "error", err)
		return
	}
	if account.Platform != db.EnumPlatformTelegram {
		return
	}

	chatID, err := strconv.ParseInt(strings.TrimSpace(account.ExternalID), 10, 64)
	if err != nil {
		log.Warn("team finder: cannot notify requester, invalid telegram external id", "requester_user_id", requesterUserID, "external_id", account.ExternalID, "error", err)
		return
	}

	reviewer := nonEmpty(strings.TrimSpace(reviewerLogin), "peer")
	reviewerUsername = normalizeTelegramUsername(reviewerUsername)
	reviewerRocketchatID = strings.TrimSpace(reviewerRocketchatID)
	reviewerAlternativeContact = strings.TrimSpace(reviewerAlternativeContact)
	project := nonEmpty(strings.TrimSpace(projectName), "project")
	displayReviewer := reviewer
	if reviewerUsername != "" {
		displayReviewer = "@" + reviewerUsername
	}
	if strings.TrimSpace(reviewerLevel) == "" {
		reviewerLevel = "0"
	}

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
		"🔔 *На твой поиск команды откликнулись!*\n\nПользователь %s (уровень %s) откликнулся на заявку по проекту *%s*.\n\nЗаявка временно переведена в статус `🟡 В переговорах`, чтобы не собирать лишний шум.%s\nЕсли не договоритесь, верни её на доску в разделе «Мои запросы команды».",
		normalizeMarkdownEscapes(displayReviewer),
		normalizeMarkdownEscapes(reviewerLevel),
		normalizeMarkdownEscapes(project),
		contactsBlock,
	)

	buttons := [][]fsm.ButtonRender{}
	if reviewerUsername != "" {
		buttons = append(buttons, []fsm.ButtonRender{{
			Text: "💬 Написать в Telegram",
			URL:  "https://t.me/" + reviewerUsername,
		}})
	}
	if reviewerRocketchatID != "" {
		buttons = append(buttons, []fsm.ButtonRender{{
			Text: "💬 Написать в Rocket.Chat",
			URL:  "https://rocketchat-student.21-school.ru/direct/" + reviewerRocketchatID,
		}})
	}

	if renderNotifier, richOK := fsm.RenderNotifierFromContext(ctx); richOK {
		render := &fsm.RenderObject{
			Text:    text,
			Buttons: buttons,
		}
		if err := renderNotifier.NotifyUserRender(ctx, chatID, render); err != nil {
			log.Warn("team finder: failed to send rich proposal notification", "requester_user_id", requesterUserID, "chat_id", chatID, "error", err)
		} else {
			return
		}
	}

	if err := notifier.NotifyUser(ctx, chatID, text); err != nil {
		log.Warn("team finder: failed to send proposal notification", "requester_user_id", requesterUserID, "chat_id", chatID, "error", err)
	}
}

func containsRegisteredGroupProject(items []s21.ParticipantProjectV1DTO, projectID int64) bool {
	for _, p := range items {
		if p.ID == projectID &&
			strings.EqualFold(strings.TrimSpace(p.Status), "REGISTERED") &&
			strings.EqualFold(strings.TrimSpace(p.Type), "GROUP") {
			return true
		}
	}
	return false
}

func teamStatusLabel(status, lang string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SEARCHING":
		if lang == fsm.LangEn {
			return "🟢 Searching for team"
		}
		return "🟢 Ищет команду"
	case "NEGOTIATING":
		if lang == fsm.LangEn {
			return "🟡 Negotiating"
		}
		return "🟡 В переговорах"
	case "PAUSED":
		if lang == fsm.LangEn {
			return "⏸ Paused"
		}
		return "⏸ На паузе"
	case "CLOSED":
		if lang == fsm.LangEn {
			return "⚫ Closed"
		}
		return "⚫ Закрыт"
	case "WITHDRAWN":
		if lang == fsm.LangEn {
			return "⚪ Withdrawn"
		}
		return "⚪ Отозван"
	default:
		if lang == fsm.LangEn {
			return "🟢 Searching for team"
		}
		return "🟢 Ищет команду"
	}
}

func truncateForList(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
