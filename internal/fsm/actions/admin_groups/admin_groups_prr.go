package admin_groups

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/campuslabel"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	groupPRRWithdrawnStub   = "stub"
	groupPRRWithdrawnDelete = "delete"
)

func registerPRRActions(registry *fsm.LogicRegistry, log *slog.Logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	registry.Register("load_group_prr_notifications_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", loadGroupPRRContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("set_group_prr_notifications_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "prr_enable_on"
		_, err = queries.UpdateTelegramGroupPRRNotificationsEnabledByOwner(ctx, db.UpdateTelegramGroupPRRNotificationsEnabledByOwnerParams{
			ChatID:                  chatID,
			OwnerTelegramUserID:     userID,
			PrrNotificationsEnabled: enable,
		})
		if err != nil {
			log.Warn("admin groups: failed to update PRR notifications enabled", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupPRRContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("set_group_prr_withdrawn_behavior", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		mode := groupPRRWithdrawnStub
		if strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "prr_withdrawn_delete" {
			mode = groupPRRWithdrawnDelete
		}
		_, err = queries.UpdateTelegramGroupPRRWithdrawnBehaviorByOwner(ctx, db.UpdateTelegramGroupPRRWithdrawnBehaviorByOwnerParams{
			ChatID:               chatID,
			OwnerTelegramUserID:  userID,
			PrrWithdrawnBehavior: mode,
		})
		if err != nil {
			log.Warn("admin groups: failed to update PRR withdrawn behavior", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupPRRContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("set_group_prr_destination_general", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		_, err = queries.UpdateTelegramGroupPRRNotificationDestinationByOwner(ctx, db.UpdateTelegramGroupPRRNotificationDestinationByOwnerParams{
			ChatID:                      chatID,
			OwnerTelegramUserID:         userID,
			PrrNotificationsThreadID:    0,
			PrrNotificationsThreadLabel: "Общий чат",
		})
		if err != nil {
			log.Warn("admin groups: failed to reset PRR destination", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", loadGroupPRRContext(ctx, queries, userID, payload, log), nil
	})

	registry.Register("load_group_prr_filter_draft", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		if toBool(payload["group_prr_filter_draft_loaded"]) {
			return "", nil, nil
		}

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", nil, nil
		}

		projectFilters, _ := queries.ListTelegramGroupPRRProjectFilters(ctx, chatID)
		campusFilters, _ := queries.ListTelegramGroupPRRCampusFilters(ctx, chatID)

		projectSet := map[string]bool{}
		for _, row := range projectFilters {
			projectSet[strconv.FormatInt(row.ProjectID, 10)] = true
		}
		campusSet := map[string]bool{}
		for _, row := range campusFilters {
			campusSet[uuidToString(row.CampusID)] = true
		}

		return "", map[string]any{
			"filter_project_ids":            encodeStringSet(projectSet),
			"filter_campus_ids":             encodeStringSet(campusSet),
			"project_filter_page":           1,
			"campus_filter_page":            1,
			"project_filter_mode":           "project",
			"project_filter_query":          "",
			"group_prr_filter_draft_loaded": true,
		}, nil
	})

	registry.Register("clear_group_prr_filter_draft", func(_ context.Context, _ int64, _ map[string]any) (string, map[string]any, error) {
		return "", map[string]any{"group_prr_filter_draft_loaded": false}, nil
	})

	registry.Register("save_group_prr_filters", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", nil, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", nil, nil
		}

		projectSet := parseStringSet(payload["filter_project_ids"])
		campusSet := parseStringSet(payload["filter_campus_ids"])

		_, _ = queries.ClearTelegramGroupPRRProjectFiltersByOwner(ctx, db.ClearTelegramGroupPRRProjectFiltersByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
		})
		for rawID := range projectSet {
			projectID, convErr := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
			if convErr != nil || projectID <= 0 {
				continue
			}
			_, _ = queries.UpsertTelegramGroupPRRProjectFilterByOwner(ctx, db.UpsertTelegramGroupPRRProjectFilterByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				ProjectID:           projectID,
			})
		}

		_, _ = queries.ClearTelegramGroupPRRCampusFiltersByOwner(ctx, db.ClearTelegramGroupPRRCampusFiltersByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
		})
		for rawID := range campusSet {
			var campusID pgtype.UUID
			if err := campusID.Scan(strings.TrimSpace(rawID)); err != nil || !campusID.Valid {
				continue
			}
			_, _ = queries.UpsertTelegramGroupPRRCampusFilterByOwner(ctx, db.UpsertTelegramGroupPRRCampusFilterByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				CampusID:            campusID,
			})
		}

		updates := loadGroupPRRContext(ctx, queries, userID, payload, log)
		updates["group_prr_filter_draft_loaded"] = false
		return "", updates, nil
	})
}

func loadGroupPRRContext(ctx context.Context, queries db.Querier, userID int64, payload map[string]any, log *slog.Logger) map[string]any {
	updates := map[string]any{
		"can_manage_selected_group":              false,
		"prr_notifications_enabled":              false,
		"prr_notifications_enabled_label_ru":     "❌ Выключено",
		"prr_notifications_enabled_label_en":     "❌ Disabled",
		"prr_notifications_destination_label":    "Общий чат",
		"prr_notifications_destination_label_en": "General chat",
		"prr_withdrawn_behavior":                 groupPRRWithdrawnStub,
		"prr_withdrawn_behavior_label_ru":        "Заглушка",
		"prr_withdrawn_behavior_label_en":        "Stub",
		"group_prr_project_filters_label_ru":     "Все проекты",
		"group_prr_project_filters_label_en":     "All projects",
		"group_prr_campus_filters_label_ru":      "Все кампусы",
		"group_prr_campus_filters_label_en":      "All campuses",
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

	if group.PrrNotificationsEnabled {
		updates["prr_notifications_enabled"] = true
		updates["prr_notifications_enabled_label_ru"] = "✅ Включено"
		updates["prr_notifications_enabled_label_en"] = "✅ Enabled"
	}

	destinationRU := strings.TrimSpace(group.PrrNotificationsThreadLabel)
	if destinationRU == "" {
		destinationRU = "Общий чат"
	}
	destinationEN := destinationRU
	if group.PrrNotificationsThreadID == 0 {
		destinationEN = "General chat"
	}
	updates["prr_notifications_destination_label"] = destinationRU
	updates["prr_notifications_destination_label_en"] = destinationEN

	mode := strings.TrimSpace(group.PrrWithdrawnBehavior)
	if mode != groupPRRWithdrawnDelete {
		mode = groupPRRWithdrawnStub
	}
	updates["prr_withdrawn_behavior"] = mode
	if mode == groupPRRWithdrawnDelete {
		updates["prr_withdrawn_behavior_label_ru"] = "Удалять сообщение"
		updates["prr_withdrawn_behavior_label_en"] = "Delete message"
	} else {
		updates["prr_withdrawn_behavior_label_ru"] = "Заглушка"
		updates["prr_withdrawn_behavior_label_en"] = "Stub"
	}

	projectFilters, err := queries.ListTelegramGroupPRRProjectFilters(ctx, chatID)
	if err != nil {
		log.Warn("admin groups: failed to load PRR project filters", "chat_id", chatID, "error", err)
	}
	campusFilters, err := queries.ListTelegramGroupPRRCampusFilters(ctx, chatID)
	if err != nil {
		log.Warn("admin groups: failed to load PRR campus filters", "chat_id", chatID, "error", err)
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
			log.Warn("admin groups: failed to resolve PRR project labels", "chat_id", chatID, "error", labelsErr)
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
			updates["group_prr_project_filters_label_ru"] = joined
			updates["group_prr_project_filters_label_en"] = joined
		}
	}

	campusSet := map[string]bool{}
	if len(campusFilters) > 0 {
		labelsRU := make([]string, 0, len(campusFilters))
		labelsEN := make([]string, 0, len(campusFilters))
		for _, row := range campusFilters {
			id := uuidToString(row.CampusID)
			campusSet[id] = true
			labelRU := id
			labelEN := id
			if campus, cErr := queries.GetCampusByID(ctx, row.CampusID); cErr == nil {
				if resolved := campuslabel.Pick(campus.NameEn.String, campus.NameRu.String, campus.ShortName, campus.FullName, fsm.LangRu); resolved != "" {
					labelRU = resolved
				}
				if resolved := campuslabel.Pick(campus.NameEn.String, campus.NameRu.String, campus.ShortName, campus.FullName, fsm.LangEn); resolved != "" {
					labelEN = resolved
				}
			}
			labelsRU = append(labelsRU, labelRU)
			labelsEN = append(labelsEN, labelEN)
		}
		sort.Strings(labelsRU)
		sort.Strings(labelsEN)
		updates["group_prr_campus_filters_label_ru"] = strings.Join(labelsRU, ", ")
		updates["group_prr_campus_filters_label_en"] = strings.Join(labelsEN, ", ")
	}

	updates["filter_project_ids"] = encodeStringSet(projectSet)
	updates["filter_campus_ids"] = encodeStringSet(campusSet)
	return updates
}

func parseStringSet(v any) map[string]bool {
	set := map[string]bool{}
	switch values := v.(type) {
	case []any:
		for _, item := range values {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				set[s] = true
			}
		}
	case []string:
		for _, item := range values {
			s := strings.TrimSpace(item)
			if s != "" {
				set[s] = true
			}
		}
	}
	return set
}

func encodeStringSet(set map[string]bool) []any {
	if len(set) == 0 {
		return []any{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, key)
	}
	return out
}

func toBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	default:
		return false
	}
}
