package admin_groups

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func registerTeamActions(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	registry.Register("load_group_team_notifications_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", loadGroupTeamContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("set_group_team_notifications_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "team_enable_on"
		_, err = queries.UpdateTelegramGroupTeamNotificationsEnabledByOwner(ctx, db.UpdateTelegramGroupTeamNotificationsEnabledByOwnerParams{
			ChatID:                   chatID,
			OwnerTelegramUserID:      userID,
			TeamNotificationsEnabled: enable,
		})
		if err != nil {
			log.Warn("admin groups: failed to update team notifications enabled", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupTeamContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("set_group_team_withdrawn_behavior", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		mode := groupPRRWithdrawnStub
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "team_withdrawn_delete" {
			mode = groupPRRWithdrawnDelete
		}
		_, err = queries.UpdateTelegramGroupTeamWithdrawnBehaviorByOwner(ctx, db.UpdateTelegramGroupTeamWithdrawnBehaviorByOwnerParams{
			ChatID:                chatID,
			OwnerTelegramUserID:   userID,
			TeamWithdrawnBehavior: mode,
		})
		if err != nil {
			log.Warn("admin groups: failed to update team withdrawn behavior", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupTeamContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("set_group_team_destination_general", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		_, err = queries.UpdateTelegramGroupTeamNotificationDestinationByOwner(ctx, db.UpdateTelegramGroupTeamNotificationDestinationByOwnerParams{
			ChatID:                       chatID,
			OwnerTelegramUserID:          userID,
			TeamNotificationsThreadID:    0,
			TeamNotificationsThreadLabel: "Общий чат",
		})
		if err != nil {
			log.Warn("admin groups: failed to reset team destination", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupTeamContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("load_group_team_filter_draft", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		if toBool(payload["group_team_filter_draft_loaded"]) {
			return "", nil, nil
		}

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", nil, nil
		}

		projectFilters, _ := queries.ListTelegramGroupTeamProjectFilters(ctx, chatID)
		campusFilters, _ := queries.ListTelegramGroupTeamCampusFilters(ctx, chatID)

		projectSet := map[string]bool{}
		for _, row := range projectFilters {
			projectSet[strconv.FormatInt(row.ProjectID, 10)] = true
		}
		campusSet := map[string]bool{}
		for _, row := range campusFilters {
			campusSet[uuidToString(row.CampusID)] = true
		}

		return "", map[string]any{
			"filter_project_ids":             encodeStringSet(projectSet),
			"filter_campus_ids":              encodeStringSet(campusSet),
			"project_filter_page":            1,
			"campus_filter_page":             1,
			"project_filter_mode":            "project",
			"project_filter_query":           "",
			"group_team_filter_draft_loaded": true,
		}, nil
	})

	registry.Register("clear_group_team_filter_draft", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{"group_team_filter_draft_loaded": false}, nil
	})

	registry.Register("save_group_team_filters", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", nil, nil
		}

		projectSet := parseStringSet(payload["filter_project_ids"])
		campusSet := parseStringSet(payload["filter_campus_ids"])

		_, _ = queries.ClearTelegramGroupTeamProjectFiltersByOwner(ctx, db.ClearTelegramGroupTeamProjectFiltersByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
		})
		for rawID := range projectSet {
			projectID, convErr := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
			if convErr != nil || projectID <= 0 {
				continue
			}
			_, _ = queries.UpsertTelegramGroupTeamProjectFilterByOwner(ctx, db.UpsertTelegramGroupTeamProjectFilterByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				ProjectID:           projectID,
			})
		}

		_, _ = queries.ClearTelegramGroupTeamCampusFiltersByOwner(ctx, db.ClearTelegramGroupTeamCampusFiltersByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
		})
		for rawID := range campusSet {
			var campusID pgtype.UUID
			if err := campusID.Scan(strings.TrimSpace(rawID)); err != nil || !campusID.Valid {
				continue
			}
			_, _ = queries.UpsertTelegramGroupTeamCampusFilterByOwner(ctx, db.UpsertTelegramGroupTeamCampusFilterByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				CampusID:            campusID,
			})
		}

		updates := loadGroupTeamContext(ctx, queries, userID, payload, log)
		updates["group_team_filter_draft_loaded"] = false
		return "", updates, nil
	})
}

func loadGroupTeamContext(ctx context.Context, queries db.Querier, userID int64, payload map[string]any, log *slog.Logger) map[string]any {
	updates := map[string]any{
		"can_manage_selected_group":               false,
		"team_notifications_enabled":              false,
		"team_notifications_enabled_label_ru":     "❌ Выключено",
		"team_notifications_enabled_label_en":     "❌ Disabled",
		"team_notifications_destination_label":    "Общий чат",
		"team_notifications_destination_label_en": "General chat",
		"team_withdrawn_behavior":                 groupPRRWithdrawnStub,
		"team_withdrawn_behavior_label_ru":        "Заглушка",
		"team_withdrawn_behavior_label_en":        "Stub",
		"group_team_project_filters_label_ru":     "Все проекты",
		"group_team_project_filters_label_en":     "All projects",
		"group_team_campus_filters_label_ru":      "Все кампусы",
		"group_team_campus_filters_label_en":      "All campuses",
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

	if group.TeamNotificationsEnabled {
		updates["team_notifications_enabled"] = true
		updates["team_notifications_enabled_label_ru"] = "✅ Включено"
		updates["team_notifications_enabled_label_en"] = "✅ Enabled"
	}

	destinationRU := strings.TrimSpace(group.TeamNotificationsThreadLabel)
	if destinationRU == "" {
		destinationRU = "Общий чат"
	}
	destinationEN := destinationRU
	if group.TeamNotificationsThreadID == 0 {
		destinationEN = "General chat"
	}
	updates["team_notifications_destination_label"] = destinationRU
	updates["team_notifications_destination_label_en"] = destinationEN

	mode := strings.TrimSpace(group.TeamWithdrawnBehavior)
	if mode != groupPRRWithdrawnDelete {
		mode = groupPRRWithdrawnStub
	}
	updates["team_withdrawn_behavior"] = mode
	if mode == groupPRRWithdrawnDelete {
		updates["team_withdrawn_behavior_label_ru"] = "Удалять сообщение"
		updates["team_withdrawn_behavior_label_en"] = "Delete message"
	} else {
		updates["team_withdrawn_behavior_label_ru"] = "Заглушка"
		updates["team_withdrawn_behavior_label_en"] = "Stub"
	}

	projectFilters, err := queries.ListTelegramGroupTeamProjectFilters(ctx, chatID)
	if err != nil {
		log.Warn("admin groups: failed to load team project filters", "chat_id", chatID, "error", err)
	}
	campusFilters, err := queries.ListTelegramGroupTeamCampusFilters(ctx, chatID)
	if err != nil {
		log.Warn("admin groups: failed to load team campus filters", "chat_id", chatID, "error", err)
	}

	projectSet := map[string]bool{}
	projectIDs := make([]int64, 0, len(projectFilters))
	for _, row := range projectFilters {
		projectIDs = append(projectIDs, row.ProjectID)
		projectSet[strconv.FormatInt(row.ProjectID, 10)] = true
	}
	if len(projectIDs) > 0 {
		labels := make([]string, 0, len(projectIDs))
		rows, labelsErr := queries.GetCatalogProjectTitlesByIDs(ctx, projectIDs)
		if labelsErr != nil {
			log.Warn("admin groups: failed to resolve team project labels", "chat_id", chatID, "error", labelsErr)
		}
		for _, row := range rows {
			title := strings.TrimSpace(row.Title)
			if title == "" {
				title = strconv.FormatInt(row.ID, 10)
			}
			labels = append(labels, title)
		}
		sort.Strings(labels)
		if len(labels) > 0 {
			joined := strings.Join(labels, ", ")
			updates["group_team_project_filters_label_ru"] = joined
			updates["group_team_project_filters_label_en"] = joined
		}
	}

	campusSet := map[string]bool{}
	if len(campusFilters) > 0 {
		labels := make([]string, 0, len(campusFilters))
		for _, row := range campusFilters {
			campusSet[uuidToString(row.CampusID)] = true
			label := uuidToString(row.CampusID)
			if campus, cErr := queries.GetCampusByID(ctx, row.CampusID); cErr == nil {
				label = strings.TrimSpace(campus.ShortName)
				if label == "" {
					label = strings.TrimSpace(campus.FullName)
				}
			}
			labels = append(labels, label)
		}
		sort.Strings(labels)
		joined := strings.Join(labels, ", ")
		updates["group_team_campus_filters_label_ru"] = joined
		updates["group_team_campus_filters_label_en"] = joined
	}

	updates["filter_project_ids"] = encodeStringSet(projectSet)
	updates["filter_campus_ids"] = encodeStringSet(campusSet)
	return updates
}
