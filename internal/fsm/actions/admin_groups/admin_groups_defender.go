package admin_groups

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	maxDefenderWhitelistButtons = 10
	maxDefenderCampusButtons    = 8
	maxDefenderTribeButtons     = 10
	maxDefenderTribePageSize    = 10
	defenderWhitelistLoadLimit  = 100
	defenderLogsLoadLimit       = 10
	defenderBanDefaultSec       = int32(24 * 60 * 60)
	defenderBanMinSec           = int32(5 * 60)
	defenderBanMaxSec           = int32(30 * 24 * 60 * 60)

	defenderReasonUnregistered = "unregistered"
	defenderReasonBlocked      = "blocked"
	defenderReasonExpelled     = "expelled"
)

func registerDefenderActions(registry *fsm.LogicRegistry, log logger, queries db.Querier) {
	if registry == nil || queries == nil {
		return
	}

	registry.Register("load_group_defender_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		return "", updates, nil
	})

	registry.Register("set_group_defender_enabled", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "def_enable_on"
		if _, err := queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
			ChatID:              chatID,
			OwnerTelegramUserID: userID,
			DefenderEnabled:     enable,
		}); err != nil {
			log.Warn("admin groups: failed to update defender_enabled", "chat_id", chatID, "user_id", userID, "error", err)
		}
		if !enable {
			if _, err := queries.UpdateTelegramGroupDefenderRemoveBlockedByOwner(ctx, db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
				ChatID:                chatID,
				OwnerTelegramUserID:   userID,
				DefenderRemoveBlocked: false,
			}); err != nil {
				log.Warn("admin groups: failed to auto-disable defender_remove_blocked", "chat_id", chatID, "user_id", userID, "error", err)
			}
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("set_group_defender_remove_blocked", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		enable := strings.TrimSpace(fmt.Sprintf("%v", payload["id"])) == "def_blocked_on"
		if _, err := queries.UpdateTelegramGroupDefenderRemoveBlockedByOwner(ctx, db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
			ChatID:                chatID,
			OwnerTelegramUserID:   userID,
			DefenderRemoveBlocked: enable,
		}); err != nil {
			log.Warn("admin groups: failed to update defender_remove_blocked", "chat_id", chatID, "user_id", userID, "error", err)
		}
		if enable {
			if _, err := queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				DefenderEnabled:     true,
			}); err != nil {
				log.Warn("admin groups: failed to auto-enable defender", "chat_id", chatID, "user_id", userID, "error", err)
			}
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("set_group_defender_ban_duration", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		durationSec, ok := parseDefenderBanDurationButtonID(fmt.Sprintf("%v", payload["id"]))
		if !ok {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		if _, err := queries.UpdateTelegramGroupDefenderBanDurationSecByOwner(ctx, db.UpdateTelegramGroupDefenderBanDurationSecByOwnerParams{
			ChatID:                 chatID,
			OwnerTelegramUserID:    userID,
			DefenderBanDurationSec: normalizeDefenderBanDurationSec(durationSec),
		}); err != nil {
			log.Warn("admin groups: failed to update defender_ban_duration_sec", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("set_group_defender_ban_duration_from_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		raw := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		durationSec, errRU, errEN := parseDefenderBanDurationInput(raw)
		if errRU != "" {
			alert := errRU
			if strings.TrimSpace(fmt.Sprintf("%v", payload["language"])) == fsm.LangEn {
				alert = errEN
			}
			return "GROUP_DEFENDER_BAN_DURATION_INPUT", map[string]any{"_alert": alert}, nil
		}

		if _, err := queries.UpdateTelegramGroupDefenderBanDurationSecByOwner(ctx, db.UpdateTelegramGroupDefenderBanDurationSecByOwnerParams{
			ChatID:                 chatID,
			OwnerTelegramUserID:    userID,
			DefenderBanDurationSec: normalizeDefenderBanDurationSec(durationSec),
		}); err != nil {
			log.Warn("admin groups: failed to update defender_ban_duration_sec from input", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("load_group_defender_campus_filter_options", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", updates, nil
		}

		campuses, err := queries.GetAllActiveCampuses(ctx)
		if err != nil {
			return "", updates, nil
		}
		selectedRows, err := queries.ListTelegramGroupDefenderCampusFilters(ctx, chatID)
		if err != nil {
			return "", updates, nil
		}
		page := parsePositiveInt(payload["defender_campus_page"], 1)
		applyDefenderCampusButtons(updates, campuses, campusFilterSet(selectedRows), page)
		return "", updates, nil
	})

	registry.Register("defender_campus_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_campus_page"], 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"defender_campus_page": page}, nil
	})

	registry.Register("defender_campus_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_campus_page"], 1)
		total := parsePositiveInt(payload["defender_campus_total_pages"], 1)
		if page < total {
			page++
		}
		return "", map[string]any{"defender_campus_page": page}, nil
	})

	registry.Register("set_group_defender_filter_campus", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		adminCampusID, hasAdminCampus := parseUUIDFromPayload(payload, "campus_id")

		rawID := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		campusID, hasCampus := parseDefenderCampusButtonID(rawID)

		switch {
		case rawID == "def_fc_none":
			_, _ = queries.ClearTelegramGroupDefenderCampusFiltersByOwner(ctx, db.ClearTelegramGroupDefenderCampusFiltersByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
			})
			_, _ = queries.ClearTelegramGroupDefenderTribeFiltersByOwner(ctx, db.ClearTelegramGroupDefenderTribeFiltersByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
			})
		case hasCampus:
			selectedRows, err := queries.ListTelegramGroupDefenderCampusFilters(ctx, chatID)
			if err == nil && campusSetHas(campusFilterSet(selectedRows), campusID) {
				_, _ = queries.DeleteTelegramGroupDefenderCampusFilterByOwner(ctx, db.DeleteTelegramGroupDefenderCampusFilterByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
					CampusID:            campusID,
				})
			} else {
				_, _ = queries.UpsertTelegramGroupDefenderCampusFilterByOwner(ctx, db.UpsertTelegramGroupDefenderCampusFilterByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
					CampusID:            campusID,
				})
				_, _ = queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
					DefenderEnabled:     true,
				})
			}

			selectedRowsAfter, err := queries.ListTelegramGroupDefenderCampusFilters(ctx, chatID)
			if err == nil && shouldClearTribeFiltersForCampusSelection(campusFilterSet(selectedRowsAfter), adminCampusID, hasAdminCampus) {
				_, _ = queries.ClearTelegramGroupDefenderTribeFiltersByOwner(ctx, db.ClearTelegramGroupDefenderTribeFiltersByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
				})
			}
		}

		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		campuses, err := queries.GetAllActiveCampuses(ctx)
		if err == nil {
			selectedRows, err := queries.ListTelegramGroupDefenderCampusFilters(ctx, chatID)
			if err == nil {
				page := parsePositiveInt(payload["defender_campus_page"], 1)
				applyDefenderCampusButtons(updates, campuses, campusFilterSet(selectedRows), page)
			}
		}
		return "", updates, nil
	})

	registry.Register("load_group_defender_tribe_filter_options", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", updates, nil
		}

		adminCampusID, ok := parseUUIDFromPayload(payload, "campus_id")
		if !ok {
			updates["defender_filter_tribe_scope_ru"] = "Кампус администратора не определён."
			updates["defender_filter_tribe_scope_en"] = "Admin campus is unavailable."
			return "", updates, nil
		}
		updates["defender_filter_tribe_scope_ru"] = strings.TrimSpace(fmt.Sprintf("%v", payload["my_campus"]))
		updates["defender_filter_tribe_scope_en"] = updates["defender_filter_tribe_scope_ru"]

		tribes, err := queries.ListCoalitionsByCampus(ctx, adminCampusID)
		if err != nil {
			return "", updates, nil
		}
		selectedRows, err := queries.ListTelegramGroupDefenderTribeFilters(ctx, chatID)
		if err != nil {
			return "", updates, nil
		}
		page := parsePositiveInt(payload["defender_tribe_page"], 1)
		applyDefenderTribeButtons(updates, tribes, tribeFilterSet(selectedRows, adminCampusID), page)
		return "", updates, nil
	})

	registry.Register("defender_tribe_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_tribe_page"], 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"defender_tribe_page": page}, nil
	})

	registry.Register("defender_tribe_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_tribe_page"], 1)
		total := parsePositiveInt(payload["defender_tribe_total_pages"], 1)
		if page < total {
			page++
		}
		return "", map[string]any{"defender_tribe_page": page}, nil
	})

	registry.Register("set_group_defender_filter_tribe", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		adminCampusID, ok := parseUUIDFromPayload(payload, "campus_id")
		if !ok {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		rawID := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		tribeID, hasTribe := parseDefenderTribeButtonID(rawID)

		if rawID == "def_ft_none" {
			_, _ = queries.ClearTelegramGroupDefenderTribeFiltersByOwner(ctx, db.ClearTelegramGroupDefenderTribeFiltersByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
			})
		}
		if hasTribe {
			exists, err := queries.ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
				CampusID: adminCampusID,
				ID:       tribeID,
			})
			if err != nil || !exists {
				return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
			}

			_, _ = queries.ClearTelegramGroupDefenderCampusFiltersByOwner(ctx, db.ClearTelegramGroupDefenderCampusFiltersByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
			})
			_, _ = queries.UpsertTelegramGroupDefenderCampusFilterByOwner(ctx, db.UpsertTelegramGroupDefenderCampusFilterByOwnerParams{
				ChatID:              chatID,
				OwnerTelegramUserID: userID,
				CampusID:            adminCampusID,
			})

			selectedRows, err := queries.ListTelegramGroupDefenderTribeFilters(ctx, chatID)
			if err == nil && tribeSetHas(tribeFilterSet(selectedRows, adminCampusID), tribeID) {
				_, _ = queries.DeleteTelegramGroupDefenderTribeFilterByOwner(ctx, db.DeleteTelegramGroupDefenderTribeFilterByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
					CampusID:            adminCampusID,
					CoalitionID:         tribeID,
				})
			} else {
				_, _ = queries.UpsertTelegramGroupDefenderTribeFilterByOwner(ctx, db.UpsertTelegramGroupDefenderTribeFilterByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
					CampusID:            adminCampusID,
					CoalitionID:         tribeID,
				})
				_, _ = queries.UpdateTelegramGroupDefenderEnabledByOwner(ctx, db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
					ChatID:              chatID,
					OwnerTelegramUserID: userID,
					DefenderEnabled:     true,
				})
			}
		}

		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_filter_tribe_scope_ru"] = strings.TrimSpace(fmt.Sprintf("%v", payload["my_campus"]))
		updates["defender_filter_tribe_scope_en"] = updates["defender_filter_tribe_scope_ru"]
		tribes, err := queries.ListCoalitionsByCampus(ctx, adminCampusID)
		if err == nil {
			selectedRows, err := queries.ListTelegramGroupDefenderTribeFilters(ctx, chatID)
			if err == nil {
				page := parsePositiveInt(payload["defender_tribe_page"], 1)
				applyDefenderTribeButtons(updates, tribes, tribeFilterSet(selectedRows, adminCampusID), page)
			}
		}
		return "", updates, nil
	})

	registry.Register("set_defender_cleanup_scope_unregistered", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeUnregistered)
		return "", updates, nil
	})

	registry.Register("set_defender_cleanup_scope_blocked", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeBlocked)
		return "", updates, nil
	})

	registry.Register("set_defender_cleanup_scope_campus", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeCampus)
		return "", updates, nil
	})

	registry.Register("set_defender_cleanup_scope_tribe", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeTribe)
		return "", updates, nil
	})

	registry.Register("load_group_defender_cleanup_campus_options", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeCampus)
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", updates, nil
		}
		campuses, err := queries.GetAllActiveCampuses(ctx)
		if err != nil {
			return "", updates, nil
		}
		selectedCampus, hasSelectedCampus := parseUUIDFromPayload(payload, "defender_cleanup_target_campus_id")
		selected := map[string]struct{}{}
		if hasSelectedCampus {
			selected[uuidToString(selectedCampus)] = struct{}{}
		}
		page := parsePositiveInt(payload["defender_campus_page"], 1)
		applyDefenderCampusButtons(updates, campuses, selected, page)
		return "", updates, nil
	})

	registry.Register("set_group_defender_cleanup_campus_target", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeCampus)
		rawID := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		campusID, ok := parseDefenderCampusButtonID(rawID)
		if !ok {
			return "", updates, nil
		}
		updates["defender_cleanup_target_campus_id"] = uuidToString(campusID)
		if campus, err := queries.GetCampusByID(ctx, campusID); err == nil {
			name := strings.TrimSpace(campus.ShortName)
			if name == "" {
				name = strings.TrimSpace(campus.FullName)
			}
			if name == "" {
				name = uuidToString(campusID)
			}
			updates["defender_cleanup_target_campus_label_ru"] = name
			updates["defender_cleanup_target_campus_label_en"] = name
		}
		return "", updates, nil
	})

	registry.Register("defender_cleanup_campus_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_campus_page"], 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"defender_campus_page": page}, nil
	})

	registry.Register("defender_cleanup_campus_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_campus_page"], 1)
		total := parsePositiveInt(payload["defender_campus_total_pages"], 1)
		if page < total {
			page++
		}
		return "", map[string]any{"defender_campus_page": page}, nil
	})

	registry.Register("load_group_defender_cleanup_tribe_options", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeTribe)
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", updates, nil
		}
		adminCampusID, ok := parseUUIDFromPayload(payload, "campus_id")
		if !ok {
			updates["defender_filter_tribe_scope_ru"] = "Кампус администратора не определён."
			updates["defender_filter_tribe_scope_en"] = "Admin campus is unavailable."
			return "", updates, nil
		}
		updates["defender_filter_tribe_scope_ru"] = strings.TrimSpace(fmt.Sprintf("%v", payload["my_campus"]))
		updates["defender_filter_tribe_scope_en"] = updates["defender_filter_tribe_scope_ru"]
		tribes, err := queries.ListCoalitionsByCampus(ctx, adminCampusID)
		if err != nil {
			return "", updates, nil
		}
		selectedTribeID := int16(parsePositiveInt(payload["defender_cleanup_target_tribe_id"], 0))
		selected := map[int16]struct{}{}
		if selectedTribeID > 0 {
			selected[selectedTribeID] = struct{}{}
		}
		page := parsePositiveInt(payload["defender_tribe_page"], 1)
		applyDefenderTribeButtons(updates, tribes, selected, page)
		return "", updates, nil
	})

	registry.Register("set_group_defender_cleanup_tribe_target", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_cleanup_scope"] = string(fsm.DefenderManualScopeTribe)
		adminCampusID, ok := parseUUIDFromPayload(payload, "campus_id")
		if !ok {
			return "", updates, nil
		}
		rawID := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		tribeID, ok := parseDefenderTribeButtonID(rawID)
		if !ok {
			return "", updates, nil
		}
		exists, err := queries.ExistsCoalitionByID(ctx, db.ExistsCoalitionByIDParams{
			CampusID: adminCampusID,
			ID:       tribeID,
		})
		if err != nil || !exists {
			return "", updates, nil
		}
		updates["defender_cleanup_target_campus_id"] = uuidToString(adminCampusID)
		updates["defender_cleanup_target_tribe_id"] = int64(tribeID)
		updates["defender_cleanup_target_campus_label_ru"] = strings.TrimSpace(fmt.Sprintf("%v", payload["my_campus"]))
		updates["defender_cleanup_target_campus_label_en"] = updates["defender_cleanup_target_campus_label_ru"]
		if tribes, err := queries.ListCoalitionsByCampus(ctx, adminCampusID); err == nil {
			label := strconv.FormatInt(int64(tribeID), 10)
			for _, tribe := range tribes {
				if tribe.ID == tribeID {
					if name := strings.TrimSpace(tribe.Name); name != "" {
						label = name
					}
					break
				}
			}
			updates["defender_cleanup_target_tribe_label_ru"] = label
			updates["defender_cleanup_target_tribe_label_en"] = label
		}
		return "", updates, nil
	})

	registry.Register("defender_cleanup_tribe_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_tribe_page"], 1)
		if page > 1 {
			page--
		}
		return "", map[string]any{"defender_tribe_page": page}, nil
	})

	registry.Register("defender_cleanup_tribe_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := parsePositiveInt(payload["defender_tribe_page"], 1)
		total := parsePositiveInt(payload["defender_tribe_total_pages"], 1)
		if page < total {
			page++
		}
		return "", map[string]any{"defender_tribe_page": page}, nil
	})

	registry.Register("run_group_defender", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", updates, nil
		}

		runner, ok := fsm.DefenderRunnerFromContext(ctx)
		if !ok {
			updates["defender_last_run_summary_ru"] = "Недоступно: transport runner не подключен."
			updates["defender_last_run_summary_en"] = "Unavailable: transport runner is not configured."
			return "", updates, nil
		}
		manualFilter, hasManualFilter, manualErrRU, manualErrEN := buildDefenderManualFilterFromPayload(payload)
		if manualErrRU != "" {
			updates["defender_last_run_summary_ru"] = manualErrRU
			updates["defender_last_run_summary_en"] = manualErrEN
			updates["defender_last_run_notice_ru"] = ""
			updates["defender_last_run_notice_en"] = ""
			return "", updates, nil
		}
		runCtx := ctx
		if hasManualFilter {
			runCtx = context.WithValue(ctx, fsm.ContextKeyDefenderManualFilter, manualFilter)
		}

		result, err := runner.RunGroupDefender(runCtx, userID, chatID)
		if err != nil {
			updates["defender_last_run_summary_ru"] = fmt.Sprintf("Ошибка запуска: %v", err)
			updates["defender_last_run_summary_en"] = fmt.Sprintf("Run failed: %v", err)
			updates["defender_last_run_notice_ru"] = ""
			updates["defender_last_run_notice_en"] = ""
			return "", updates, nil
		}

		summary := fmt.Sprintf(
			"removed=%d, skip_whitelist=%d, skip_not_member=%d, skip_no_rights=%d, hit_unregistered=%d, hit_blocked=%d, errors=%d",
			result.Removed,
			result.SkippedWhitelist,
			result.SkippedNotMember,
			result.SkippedNoRights,
			result.SkippedUnregistered,
			result.SkippedBlocked,
			result.Errors,
		)
		updates["defender_last_run_summary_ru"] = summary
		updates["defender_last_run_summary_en"] = summary
		updates["defender_last_run_notice_ru"] = ""
		updates["defender_last_run_notice_en"] = ""
		if result.SkippedNoRights > 0 {
			updates["defender_last_run_notice_ru"] = "⚠️ Нужно право на ban users. Назначьте боту право бана участников и запустите снова."
			updates["defender_last_run_notice_en"] = "⚠️ Bot needs ban users permission. Grant ban rights and run again."
		}

		return "", updates, nil
	})

	registry.Register("preview_group_defender_violations", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)

		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", updates, nil
		}
		group, err := requireOwnedGroup(ctx, queries, userID, chatID)
		if err != nil {
			return "", updates, nil
		}

		type candidate struct {
			ID     int64
			Name   string
			Uname  string
			Reason string
		}
		candidates := make([]candidate, 0)
		manualFilter, hasManualFilter, manualErrRU, manualErrEN := buildDefenderManualFilterFromPayload(payload)
		if manualErrRU != "" {
			updates["defender_preview_summary_ru"] = manualErrRU
			updates["defender_preview_summary_en"] = manualErrEN
			updates["defender_preview_candidate_ids"] = ""
			return "", updates, nil
		}

		if runner, ok := fsm.DefenderRunnerFromContext(ctx); ok {
			previewCtx := ctx
			if hasManualFilter {
				previewCtx = context.WithValue(ctx, fsm.ContextKeyDefenderManualFilter, manualFilter)
			}
			items, err := runner.PreviewGroupDefenderCandidates(previewCtx, userID, chatID)
			if err != nil {
				updates["defender_preview_summary_ru"] = "Не удалось собрать список участников для предпросмотра."
				updates["defender_preview_summary_en"] = "Failed to build preview from known members."
				return "", updates, nil
			}
			for _, item := range items {
				candidates = append(candidates, candidate{
					ID:     item.TelegramUserID,
					Name:   item.DisplayName,
					Uname:  item.Username,
					Reason: item.Reason,
				})
			}
		} else {
			if hasManualFilter {
				updates["defender_preview_summary_ru"] = "Недоступно: предпросмотр в режиме генеральной уборки требует transport runner."
				updates["defender_preview_summary_en"] = "Unavailable: manual cleanup preview requires transport runner."
				updates["defender_preview_candidate_ids"] = ""
				return "", updates, nil
			}
			knownMembers, err := queries.ListTelegramGroupKnownMembers(ctx, chatID)
			if err != nil {
				updates["defender_preview_summary_ru"] = "Не удалось собрать список участников для предпросмотра."
				updates["defender_preview_summary_en"] = "Failed to build preview from known members."
				return "", updates, nil
			}

			seen := make(map[int64]struct{})
			for _, member := range knownMembers {
				if member.IsBot {
					continue
				}

				whitelisted, err := queries.ExistsTelegramGroupWhitelist(ctx, db.ExistsTelegramGroupWhitelistParams{
					ChatID:         chatID,
					TelegramUserID: member.TelegramUserID,
				})
				if err != nil || whitelisted {
					continue
				}

				tgIDStr := strconv.FormatInt(member.TelegramUserID, 10)
				acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
					Platform:   db.EnumPlatformTelegram,
					ExternalID: tgIDStr,
				})
				if err != nil {
					if _, ok := seen[member.TelegramUserID]; !ok {
						seen[member.TelegramUserID] = struct{}{}
						candidates = append(candidates, candidate{ID: member.TelegramUserID, Reason: defenderReasonUnregistered})
					}
					continue
				}

				if !group.DefenderRemoveBlocked {
					continue
				}

				profile, err := queries.GetMyProfile(ctx, acc.S21Login)
				if err != nil || !profile.Status.Valid {
					continue
				}
				if profile.Status.EnumStudentStatus == db.EnumStudentStatusBLOCKED || profile.Status.EnumStudentStatus == db.EnumStudentStatusEXPELLED {
					if _, ok := seen[member.TelegramUserID]; ok {
						continue
					}
					reason := defenderReasonBlocked
					if profile.Status.EnumStudentStatus == db.EnumStudentStatusEXPELLED {
						reason = defenderReasonExpelled
					}
					seen[member.TelegramUserID] = struct{}{}
					candidates = append(candidates, candidate{ID: member.TelegramUserID, Reason: reason})
				}
			}
		}

		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

		if len(candidates) == 0 {
			updates["defender_preview_candidate_ids"] = ""
			updates["defender_preview_summary_ru"] = "Нарушителей не найдено среди известных участников."
			updates["defender_preview_summary_en"] = "No violators found among known members."
			return "", updates, nil
		}

		ids := make([]string, 0, len(candidates))
		lines := make([]string, 0, len(candidates))
		for _, c := range candidates {
			ids = append(ids, strconv.FormatInt(c.ID, 10))
			display := strings.TrimSpace(c.Name)
			if display == "" {
				display = strconv.FormatInt(c.ID, 10)
			}
			uname := strings.TrimPrefix(strings.TrimSpace(c.Uname), "@")
			mentionURL := fmt.Sprintf("tg://user?id=%d", c.ID)
			if uname != "" {
				mentionURL = "https://t.me/" + uname
			}
			mention := fmt.Sprintf("[%s](%s)", escapeMarkdownLinkText(display), mentionURL)
			usernameSuffix := ""
			if uname != "" {
				usernameSuffix = " @" + escapeMarkdownPlain(uname)
			}
			lines = append(lines, fmt.Sprintf("%s%s `%d` - `%s`", mention, usernameSuffix, c.ID, escapeMarkdownV2(c.Reason)))
		}
		updates["defender_preview_candidate_ids"] = strings.Join(ids, ",")
		previewText := fmt.Sprintf("Найдено: %d\n%s", len(candidates), strings.Join(lines, "\n"))
		updates["defender_preview_summary_ru"] = previewText
		updates["defender_preview_summary_en"] = previewText

		return "", updates, nil
	})

	registry.Register("add_group_defender_preview_to_whitelist", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		addedByID, err := queries.GetUserAccountIDByExternalId(ctx, db.GetUserAccountIDByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Не удалось определить администратора для whitelist."
			updates["defender_preview_summary_en"] = "Failed to resolve admin account for whitelist operation."
			return "", updates, nil
		}

		rawIDs := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_candidate_ids"]))
		ids := splitCSVInt64(rawIDs)
		added := 0
		for _, id := range ids {
			if id <= 0 {
				continue
			}
			if _, err := queries.UpsertTelegramGroupWhitelist(ctx, db.UpsertTelegramGroupWhitelistParams{
				ChatID:           chatID,
				TelegramUserID:   id,
				AddedByAccountID: addedByID,
			}); err == nil {
				added++
			}
		}

		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_preview_candidate_ids"] = ""
		updates["defender_preview_summary_ru"] = fmt.Sprintf("Добавлено в whitelist: %d", added)
		updates["defender_preview_summary_en"] = fmt.Sprintf("Added to whitelist: %d", added)
		return "", updates, nil
	})

	registry.Register("add_group_defender_whitelist_from_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		tgIDRaw := strings.TrimSpace(fmt.Sprintf("%v", payload["id"]))
		tgID, err := strconv.ParseInt(tgIDRaw, 10, 64)
		if err != nil || tgID <= 0 {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Некорректный Telegram ID."
			updates["defender_preview_summary_en"] = "Invalid Telegram ID."
			return "", updates, nil
		}

		addedByID, err := queries.GetUserAccountIDByExternalId(ctx, db.GetUserAccountIDByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Не удалось определить администратора для whitelist."
			updates["defender_preview_summary_en"] = "Failed to resolve admin account for whitelist operation."
			return "", updates, nil
		}

		if _, err := queries.UpsertTelegramGroupWhitelist(ctx, db.UpsertTelegramGroupWhitelistParams{
			ChatID:           chatID,
			TelegramUserID:   tgID,
			AddedByAccountID: addedByID,
		}); err != nil {
			updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
			updates["defender_preview_summary_ru"] = "Не удалось добавить ID в whitelist."
			updates["defender_preview_summary_en"] = "Failed to add ID to whitelist."
			return "", updates, nil
		}

		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		updates["defender_preview_summary_ru"] = fmt.Sprintf("ID `%d` добавлен в whitelist.", tgID)
		updates["defender_preview_summary_en"] = fmt.Sprintf("ID `%d` added to whitelist.", tgID)
		return "", updates, nil
	})

	registry.Register("remove_group_defender_whitelist", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		chatID, err := parseSelectedGroupChatID(payload)
		if err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}
		if _, err := requireOwnedGroup(ctx, queries, userID, chatID); err != nil {
			return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
		}

		wlID, ok := parseWhitelistRemoveID(strings.TrimSpace(fmt.Sprintf("%v", payload["id"])))
		if ok {
			_, _ = queries.DeleteTelegramGroupWhitelistByOwner(ctx, db.DeleteTelegramGroupWhitelistByOwnerParams{
				ChatID:              chatID,
				TelegramUserID:      wlID,
				OwnerTelegramUserID: userID,
			})
		}
		return "", buildDefenderContextUpdates(ctx, userID, payload, log, queries), nil
	})

	registry.Register("load_group_defender_logs", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		updates := buildDefenderContextUpdates(ctx, userID, payload, log, queries)
		return "", updates, nil
	})
}

type logger interface {
	Warn(msg string, args ...any)
	Debug(msg string, args ...any)
}

func buildDefenderContextUpdates(ctx context.Context, userID int64, payload map[string]any, log logger, queries db.Querier) map[string]any {
	updates := map[string]any{
		"can_manage_selected_group": false,
	}
	mergeDefenderDefaults(updates, payload)

	chatID, err := parseSelectedGroupChatID(payload)
	if err != nil {
		return updates
	}
	group, err := requireOwnedGroup(ctx, queries, userID, chatID)
	if err != nil {
		return updates
	}

	updates["can_manage_selected_group"] = true
	if strings.TrimSpace(fmt.Sprintf("%v", payload["selected_group_title"])) == "" {
		updates["selected_group_title"] = group.ChatTitle
	}
	maps.Copy(updates, buildDefenderSettingsUpdates(group))

	campusFilters := make([]db.TelegramGroupDefenderCampusFilter, 0)
	if rows, err := queries.ListTelegramGroupDefenderCampusFilters(ctx, chatID); err == nil {
		campusFilters = rows
	} else if log != nil {
		log.Warn("admin groups: failed to load defender campus filters", "chat_id", chatID, "error", err)
	}
	tribeFilters := make([]db.TelegramGroupDefenderTribeFilter, 0)
	if rows, err := queries.ListTelegramGroupDefenderTribeFilters(ctx, chatID); err == nil {
		tribeFilters = rows
	} else if log != nil {
		log.Warn("admin groups: failed to load defender tribe filters", "chat_id", chatID, "error", err)
	}
	applyDefenderFilterLabels(ctx, queries, updates, campusFilters, tribeFilters)

	whitelistRows, err := queries.ListTelegramGroupWhitelists(ctx, db.ListTelegramGroupWhitelistsParams{
		ChatID:   chatID,
		RowLimit: defenderWhitelistLoadLimit,
	})
	if err != nil {
		if log != nil {
			log.Warn("admin groups: failed to load defender whitelist", "chat_id", chatID, "error", err)
		}
	} else {
		applyWhitelistRows(ctx, queries, userID, chatID, updates, whitelistRows)
	}

	logRows, err := queries.ListTelegramGroupLogs(ctx, db.ListTelegramGroupLogsParams{
		ChatID:   chatID,
		RowLimit: defenderLogsLoadLimit,
	})
	if err != nil {
		if log != nil {
			log.Warn("admin groups: failed to load defender logs", "chat_id", chatID, "error", err)
		}
		updates["defender_logs_list_ru"] = "⚠️ Не удалось загрузить лог. Проверьте миграции и доступ к таблице telegram_group_logs."
		updates["defender_logs_list_en"] = "⚠️ Failed to load logs. Check migrations and access to telegram_group_logs."
	} else {
		updates["defender_logs_list_ru"] = formatDefenderLogs(logRows, fsm.LangRu)
		updates["defender_logs_list_en"] = formatDefenderLogs(logRows, fsm.LangEn)
	}

	return updates
}

func mergeDefenderDefaults(updates map[string]any, payload map[string]any) {
	updates["defender_enabled"] = false
	updates["defender_enabled_label_ru"] = "❌ Выключено"
	updates["defender_enabled_label_en"] = "❌ Disabled"
	updates["defender_remove_blocked"] = false
	updates["defender_remove_blocked_label_ru"] = "❌ Выключено"
	updates["defender_remove_blocked_label_en"] = "❌ Disabled"
	updates["defender_ban_duration_sec"] = defenderBanDefaultSec
	updates["defender_ban_duration_label_ru"] = formatDefenderBanDurationLabel(defenderBanDefaultSec, fsm.LangRu)
	updates["defender_ban_duration_label_en"] = formatDefenderBanDurationLabel(defenderBanDefaultSec, fsm.LangEn)
	updates["defender_whitelist_count"] = 0
	updates["defender_whitelist_list_ru"] = "Пусто"
	updates["defender_whitelist_list_en"] = "Empty"
	updates["defender_logs_list_ru"] = "Лог пуст."
	updates["defender_logs_list_en"] = "Log is empty."
	updates["defender_filter_campus_label_ru"] = "Все кампусы"
	updates["defender_filter_campus_label_en"] = "All campuses"
	updates["defender_filter_tribe_label_ru"] = "Все трайбы"
	updates["defender_filter_tribe_label_en"] = "All tribes"
	updates["defender_filter_tribe_scope_ru"] = ""
	updates["defender_filter_tribe_scope_en"] = ""
	cleanupScope := "configured"
	cleanupCampusID := ""
	cleanupCampusLabelRU := "—"
	cleanupCampusLabelEN := "—"
	cleanupTribeID := int64(0)
	cleanupTribeLabelRU := "—"
	cleanupTribeLabelEN := "—"
	if payload != nil {
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_scope"])); v != "" {
			cleanupScope = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_campus_id"])); v != "" && v != "<nil>" {
			cleanupCampusID = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_campus_label_ru"])); v != "" && v != "<nil>" {
			cleanupCampusLabelRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_campus_label_en"])); v != "" && v != "<nil>" {
			cleanupCampusLabelEN = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_tribe_id"])); v != "" && v != "<nil>" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				cleanupTribeID = n
			}
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_tribe_label_ru"])); v != "" && v != "<nil>" {
			cleanupTribeLabelRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_tribe_label_en"])); v != "" && v != "<nil>" {
			cleanupTribeLabelEN = v
		}
	}
	updates["defender_cleanup_scope"] = cleanupScope
	updates["defender_cleanup_target_campus_id"] = cleanupCampusID
	updates["defender_cleanup_target_campus_label_ru"] = cleanupCampusLabelRU
	updates["defender_cleanup_target_campus_label_en"] = cleanupCampusLabelEN
	updates["defender_cleanup_target_tribe_id"] = cleanupTribeID
	updates["defender_cleanup_target_tribe_label_ru"] = cleanupTribeLabelRU
	updates["defender_cleanup_target_tribe_label_en"] = cleanupTribeLabelEN
	updates["defender_campus_page"] = 1
	updates["defender_campus_total_pages"] = 1
	updates["defender_campus_has_prev_page"] = false
	updates["defender_campus_has_next_page"] = false
	updates["defender_campus_page_caption_ru"] = "1/1"
	updates["defender_campus_page_caption_en"] = "1/1"
	updates["defender_tribe_page"] = 1
	updates["defender_tribe_total_pages"] = 1
	updates["defender_tribe_has_prev_page"] = false
	updates["defender_tribe_has_next_page"] = false
	updates["defender_tribe_page_caption_ru"] = "1/1"
	updates["defender_tribe_page_caption_en"] = "1/1"

	runSummaryRU := "—"
	runSummaryEN := "—"
	runNoticeRU := ""
	runNoticeEN := ""
	if payload != nil {
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_summary_ru"])); v != "" {
			runSummaryRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_summary_en"])); v != "" {
			runSummaryEN = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_notice_ru"])); v != "" {
			runNoticeRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_last_run_notice_en"])); v != "" {
			runNoticeEN = v
		}
	}
	updates["defender_last_run_summary_ru"] = runSummaryRU
	updates["defender_last_run_summary_en"] = runSummaryEN
	updates["defender_last_run_notice_ru"] = runNoticeRU
	updates["defender_last_run_notice_en"] = runNoticeEN

	previewIDs := ""
	previewRU := "Нажми «Предпросмотр», чтобы увидеть потенциальных нарушителей среди известных участников."
	previewEN := "Tap preview to see potential violators among known members."
	if payload != nil {
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_candidate_ids"])); v != "" {
			previewIDs = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_summary_ru"])); v != "" {
			previewRU = v
		}
		if v := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_preview_summary_en"])); v != "" {
			previewEN = v
		}
	}
	updates["defender_preview_candidate_ids"] = previewIDs
	updates["defender_preview_summary_ru"] = previewRU
	updates["defender_preview_summary_en"] = previewEN

	resetDefenderWhitelistSlots(updates)
	resetDefenderCampusSlots(updates)
	resetDefenderTribeSlots(updates)
}

func buildDefenderSettingsUpdates(group db.TelegramGroup) map[string]any {
	enabledRU := "❌ Выключено"
	enabledEN := "❌ Disabled"
	if group.DefenderEnabled {
		enabledRU = "✅ Включено"
		enabledEN = "✅ Enabled"
	}

	blockedRU := "❌ Выключено"
	blockedEN := "❌ Disabled"
	if group.DefenderRemoveBlocked {
		blockedRU = "✅ Включено"
		blockedEN = "✅ Enabled"
	}

	banDurationSec := normalizeDefenderBanDurationSec(group.DefenderBanDurationSec)

	return map[string]any{
		"defender_enabled":                 group.DefenderEnabled,
		"defender_enabled_label_ru":        enabledRU,
		"defender_enabled_label_en":        enabledEN,
		"defender_remove_blocked":          group.DefenderRemoveBlocked,
		"defender_remove_blocked_label_ru": blockedRU,
		"defender_remove_blocked_label_en": blockedEN,
		"defender_ban_duration_sec":        banDurationSec,
		"defender_ban_duration_label_ru":   formatDefenderBanDurationLabel(banDurationSec, fsm.LangRu),
		"defender_ban_duration_label_en":   formatDefenderBanDurationLabel(banDurationSec, fsm.LangEn),
	}
}

func resetDefenderWhitelistSlots(updates map[string]any) {
	for i := 1; i <= maxDefenderWhitelistButtons; i++ {
		updates[fmt.Sprintf("defender_whitelist_remove_button_id_%d", i)] = ""
		updates[fmt.Sprintf("defender_whitelist_remove_label_%d", i)] = ""
	}
}

func resetDefenderCampusSlots(updates map[string]any) {
	for i := 1; i <= maxDefenderCampusButtons; i++ {
		updates[fmt.Sprintf("defender_campus_button_id_%d", i)] = ""
		updates[fmt.Sprintf("defender_campus_button_label_%d", i)] = ""
	}
}

func resetDefenderTribeSlots(updates map[string]any) {
	for i := 1; i <= maxDefenderTribeButtons; i++ {
		updates[fmt.Sprintf("defender_tribe_button_id_%d", i)] = ""
		updates[fmt.Sprintf("defender_tribe_button_label_%d", i)] = ""
	}
}

func applyWhitelistRows(ctx context.Context, queries db.Querier, ownerTelegramUserID, chatID int64, updates map[string]any, rows []db.TelegramGroupWhitelist) {
	updates["defender_whitelist_count"] = len(rows)
	if len(rows) == 0 {
		updates["defender_whitelist_list_ru"] = "Пусто"
		updates["defender_whitelist_list_en"] = "Empty"
		return
	}

	runner, hasRunner := fsm.DefenderRunnerFromContext(ctx)

	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		display := strconv.FormatInt(row.TelegramUserID, 10)
		mentionURL := fmt.Sprintf("tg://user?id=%d", row.TelegramUserID)
		usernameSuffix := ""
		uname := ""

		if hasRunner {
			if resolvedDisplay, resolvedUsername, err := runner.ResolveGroupMemberIdentity(ctx, ownerTelegramUserID, chatID, row.TelegramUserID); err == nil {
				if v := strings.TrimSpace(resolvedDisplay); v != "" {
					display = v
				}
				uname = strings.TrimPrefix(strings.TrimSpace(resolvedUsername), "@")
			}
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: strconv.FormatInt(row.TelegramUserID, 10),
		})
		if err == nil {
			if uname == "" {
				uname = strings.TrimPrefix(strings.TrimSpace(acc.Username.String), "@")
			}
			if login := strings.TrimSpace(acc.S21Login); login != "" && display == strconv.FormatInt(row.TelegramUserID, 10) {
				display = login
			}
		}

		if uname != "" {
			mentionURL = "https://t.me/" + uname
			usernameSuffix = " @" + escapeMarkdownPlain(uname)
		}

		mention := fmt.Sprintf("[%s](%s)", escapeMarkdownLinkText(display), mentionURL)
		lines = append(lines, fmt.Sprintf("%d. %s%s `%d`", i+1, mention, usernameSuffix, row.TelegramUserID))
		if i < maxDefenderWhitelistButtons {
			updates[fmt.Sprintf("defender_whitelist_remove_button_id_%d", i+1)] = fmt.Sprintf("def_wl_rm_%d", row.TelegramUserID)
			updates[fmt.Sprintf("defender_whitelist_remove_label_%d", i+1)] = buildWhitelistRemoveLabel(display, row.TelegramUserID)
		}
	}
	updates["defender_whitelist_list_ru"] = strings.Join(lines, "\n")
	updates["defender_whitelist_list_en"] = updates["defender_whitelist_list_ru"]
}

func buildWhitelistRemoveLabel(display string, telegramUserID int64) string {
	name := strings.TrimSpace(display)
	if name == "" {
		name = strconv.FormatInt(telegramUserID, 10)
	}
	const maxNameRunes = 18
	r := []rune(name)
	if len(r) > maxNameRunes {
		name = string(r[:maxNameRunes-1]) + "…"
	}
	return fmt.Sprintf("❌ %s (%d)", name, telegramUserID)
}

func formatDefenderLogs(rows []db.TelegramGroupLog, language string) string {
	if len(rows) == 0 {
		if language == fsm.LangEn {
			return "Log is empty."
		}
		return "Лог пуст."
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		ts := "—"
		if row.CreatedAt.Valid {
			ts = row.CreatedAt.Time.In(time.Local).Format("02.01 15:04:05")
		}

		sourceLabel := defenderLogSourceLabel(row.Source, language)
		resultLabel := defenderLogResultLabel(row.Action, row.Reason, language)
		line := fmt.Sprintf("• #%d · %s · %s\n  👤 `%d` · %s", row.ID, ts, sourceLabel, row.TelegramUserID, resultLabel)
		if strings.TrimSpace(row.Details) != "" {
			if language == fsm.LangEn {
				line += "\n  ℹ️ " + "Details: " + fsm.EscapeMarkdown(row.Details)
			} else {
				line += "\n  ℹ️ " + "Детали: " + fsm.EscapeMarkdown(row.Details)
			}
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func defenderLogSourceLabel(source, language string) string {
	switch source {
	case "auto_join":
		if language == fsm.LangEn {
			return "Auto-check on join"
		}
		return "Автопроверка при входе"
	case "manual_run":
		if language == fsm.LangEn {
			return "Manual run"
		}
		return "Ручной запуск"
	case "preview":
		if language == fsm.LangEn {
			return "Preview"
		}
		return "Предпросмотр"
	default:
		return fsm.EscapeMarkdown(source)
	}
}

func defenderLogResultLabel(action, reason, language string) string {
	switch action + "/" + reason {
	case "removed/unregistered":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: not registered in bot"
		}
		return "🚫 Временный бан: не зарегистрирован в боте"
	case "removed/blocked":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: student status BLOCKED"
		}
		return "🚫 Временный бан: статус студента BLOCKED"
	case "removed/expelled":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: student status EXPELLED"
		}
		return "🚫 Временный бан: статус студента EXPELLED"
	case "removed/campus_filter":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: campus filter mismatch"
		}
		return "🚫 Временный бан: не прошёл фильтр кампуса"
	case "removed/tribe_filter":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: tribe filter mismatch"
		}
		return "🚫 Временный бан: не прошёл фильтр трайба"
	case "removed/campus_selected":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: matched selected campus"
		}
		return "🚫 Временный бан: из выбранного кампуса"
	case "removed/tribe_selected":
		if language == fsm.LangEn {
			return "🚫 Temporarily banned: matched selected tribe"
		}
		return "🚫 Временный бан: из выбранного трайба"
	case "skipped_whitelist/whitelist":
		if language == fsm.LangEn {
			return "⏭ Skipped: in whitelist"
		}
		return "⏭ Пропущен: в whitelist"
	case "skipped_no_rights/bot_rights":
		if language == fsm.LangEn {
			return "⚠️ Skipped: bot has no ban rights"
		}
		return "⚠️ Пропущен: у бота нет права бана"
	case "skipped_not_member/not_member":
		if language == fsm.LangEn {
			return "⏭ Skipped: user is not a group member"
		}
		return "⏭ Пропущен: пользователь уже не участник группы"
	default:
		return fsm.EscapeMarkdown(action) + "/" + fsm.EscapeMarkdown(reason)
	}
}

func applyDefenderFilterLabels(
	ctx context.Context,
	queries db.Querier,
	updates map[string]any,
	campusFilters []db.TelegramGroupDefenderCampusFilter,
	tribeFilters []db.TelegramGroupDefenderTribeFilter,
) {
	campusLabels := make([]string, 0, len(campusFilters))
	for _, row := range campusFilters {
		label := uuidToString(row.CampusID)
		if campus, err := queries.GetCampusByID(ctx, row.CampusID); err == nil {
			name := strings.TrimSpace(campus.ShortName)
			if name == "" {
				name = strings.TrimSpace(campus.FullName)
			}
			if name != "" {
				label = name
			}
		}
		campusLabels = append(campusLabels, label)
	}
	sort.Strings(campusLabels)

	campusLabelRU := "Все кампусы"
	campusLabelEN := "All campuses"
	if len(campusLabels) > 0 {
		campusLabelRU = strings.Join(campusLabels, ", ")
		campusLabelEN = campusLabelRU
	}

	type coalitionMap map[int16]string
	coalitionsByCampus := map[string]coalitionMap{}
	tribeLabels := make([]string, 0, len(tribeFilters))
	for _, row := range tribeFilters {
		campusKey := uuidToString(row.CampusID)
		if _, ok := coalitionsByCampus[campusKey]; !ok {
			coalitionsByCampus[campusKey] = coalitionMap{}
			if coalitions, err := queries.ListCoalitionsByCampus(ctx, row.CampusID); err == nil {
				for _, c := range coalitions {
					coalitionsByCampus[campusKey][c.ID] = strings.TrimSpace(c.Name)
				}
			}
		}
		label := strings.TrimSpace(coalitionsByCampus[campusKey][row.CoalitionID])
		if label == "" {
			label = strconv.FormatInt(int64(row.CoalitionID), 10)
		}
		tribeLabels = append(tribeLabels, label)
	}
	sort.Strings(tribeLabels)

	tribeLabelRU := "Все трайбы"
	tribeLabelEN := "All tribes"
	if len(tribeLabels) > 0 {
		tribeLabelRU = strings.Join(tribeLabels, ", ")
		tribeLabelEN = tribeLabelRU
	}

	updates["defender_filter_campus_label_ru"] = campusLabelRU
	updates["defender_filter_campus_label_en"] = campusLabelEN
	updates["defender_filter_tribe_label_ru"] = tribeLabelRU
	updates["defender_filter_tribe_label_en"] = tribeLabelEN
}

func applyDefenderCampusButtons(updates map[string]any, campuses []db.GetAllActiveCampusesRow, selectedCampuses map[string]struct{}, page int) {
	resetDefenderCampusSlots(updates)
	orderedCampuses := orderCampusesSelectedFirst(campuses, selectedCampuses)
	totalPages := 1
	if len(orderedCampuses) > 0 {
		totalPages = (len(orderedCampuses) + maxDefenderCampusButtons - 1) / maxDefenderCampusButtons
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * maxDefenderCampusButtons
	end := min(start+maxDefenderCampusButtons, len(orderedCampuses))

	updates["defender_campus_page"] = page
	updates["defender_campus_total_pages"] = totalPages
	updates["defender_campus_has_prev_page"] = page > 1
	updates["defender_campus_has_next_page"] = page < totalPages
	updates["defender_campus_page_caption_ru"] = fmt.Sprintf("%d/%d", page, totalPages)
	updates["defender_campus_page_caption_en"] = updates["defender_campus_page_caption_ru"]

	for i, row := range orderedCampuses[start:end] {
		check := "▫️"
		if row.ID.Valid && campusSetHas(selectedCampuses, row.ID) {
			check = "✅"
		}
		label := strings.TrimSpace(row.ShortName)
		if label == "" {
			label = strings.TrimSpace(row.FullName)
		}
		if label == "" {
			label = uuidToString(row.ID)
		}
		updates[fmt.Sprintf("defender_campus_button_id_%d", i+1)] = "def_fc_" + uuidToString(row.ID)
		updates[fmt.Sprintf("defender_campus_button_label_%d", i+1)] = fmt.Sprintf("%s %s", check, label)
	}
}

func applyDefenderTribeButtons(updates map[string]any, tribes []db.Coalition, selectedTribes map[int16]struct{}, page int) {
	resetDefenderTribeSlots(updates)
	orderedTribes := orderTribesSelectedFirst(tribes, selectedTribes)

	totalPages := 1
	if len(orderedTribes) > 0 {
		totalPages = (len(orderedTribes) + maxDefenderTribePageSize - 1) / maxDefenderTribePageSize
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * maxDefenderTribePageSize
	end := min(start+maxDefenderTribePageSize, len(orderedTribes))

	updates["defender_tribe_page"] = page
	updates["defender_tribe_total_pages"] = totalPages
	updates["defender_tribe_has_prev_page"] = page > 1
	updates["defender_tribe_has_next_page"] = page < totalPages
	updates["defender_tribe_page_caption_ru"] = fmt.Sprintf("%d/%d", page, totalPages)
	updates["defender_tribe_page_caption_en"] = updates["defender_tribe_page_caption_ru"]

	for i, row := range orderedTribes[start:end] {
		check := "▫️"
		if tribeSetHas(selectedTribes, row.ID) {
			check = "✅"
		}
		label := strings.TrimSpace(row.Name)
		if label == "" {
			label = strconv.FormatInt(int64(row.ID), 10)
		}
		updates[fmt.Sprintf("defender_tribe_button_id_%d", i+1)] = fmt.Sprintf("def_ft_%d", row.ID)
		updates[fmt.Sprintf("defender_tribe_button_label_%d", i+1)] = fmt.Sprintf("%s %s", check, label)
	}
}

func orderCampusesSelectedFirst(campuses []db.GetAllActiveCampusesRow, selectedCampuses map[string]struct{}) []db.GetAllActiveCampusesRow {
	if len(campuses) == 0 || len(selectedCampuses) == 0 {
		return campuses
	}
	selected := make([]db.GetAllActiveCampusesRow, 0, len(campuses))
	rest := make([]db.GetAllActiveCampusesRow, 0, len(campuses))
	for _, row := range campuses {
		if row.ID.Valid && campusSetHas(selectedCampuses, row.ID) {
			selected = append(selected, row)
			continue
		}
		rest = append(rest, row)
	}
	return append(selected, rest...)
}

func orderTribesSelectedFirst(tribes []db.Coalition, selectedTribes map[int16]struct{}) []db.Coalition {
	if len(tribes) == 0 || len(selectedTribes) == 0 {
		return tribes
	}
	selected := make([]db.Coalition, 0, len(tribes))
	rest := make([]db.Coalition, 0, len(tribes))
	for _, row := range tribes {
		if tribeSetHas(selectedTribes, row.ID) {
			selected = append(selected, row)
			continue
		}
		rest = append(rest, row)
	}
	return append(selected, rest...)
}

func campusFilterSet(rows []db.TelegramGroupDefenderCampusFilter) map[string]struct{} {
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if !row.CampusID.Valid {
			continue
		}
		set[uuidToString(row.CampusID)] = struct{}{}
	}
	return set
}

func tribeFilterSet(rows []db.TelegramGroupDefenderTribeFilter, campusID pgtype.UUID) map[int16]struct{} {
	set := map[int16]struct{}{}
	for _, row := range rows {
		if campusID.Valid && row.CampusID != campusID {
			continue
		}
		set[row.CoalitionID] = struct{}{}
	}
	return set
}

func campusSetHas(set map[string]struct{}, campusID pgtype.UUID) bool {
	if !campusID.Valid || len(set) == 0 {
		return false
	}
	_, ok := set[uuidToString(campusID)]
	return ok
}

func tribeSetHas(set map[int16]struct{}, tribeID int16) bool {
	if len(set) == 0 {
		return false
	}
	_, ok := set[tribeID]
	return ok
}

func shouldClearTribeFiltersForCampusSelection(selectedCampuses map[string]struct{}, adminCampus pgtype.UUID, hasAdminCampus bool) bool {
	if !hasAdminCampus || !adminCampus.Valid {
		return true
	}
	if len(selectedCampuses) != 1 {
		return true
	}
	_, ok := selectedCampuses[uuidToString(adminCampus)]
	return !ok
}

func parseDefenderCampusButtonID(buttonID string) (pgtype.UUID, bool) {
	raw, ok := strings.CutPrefix(strings.TrimSpace(buttonID), "def_fc_")
	if !ok || strings.TrimSpace(raw) == "" || raw == "none" {
		return pgtype.UUID{}, false
	}
	id := pgtype.UUID{}
	if err := id.Scan(raw); err != nil || !id.Valid {
		return pgtype.UUID{}, false
	}
	return id, true
}

func parseDefenderTribeButtonID(buttonID string) (int16, bool) {
	raw, ok := strings.CutPrefix(strings.TrimSpace(buttonID), "def_ft_")
	if !ok || strings.TrimSpace(raw) == "" || raw == "none" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 16)
	if err != nil || n <= 0 {
		return 0, false
	}
	return int16(n), true
}

func parseUUIDFromPayload(payload map[string]any, key string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(fmt.Sprintf("%v", payload[key]))
	if raw == "" || raw == "<nil>" {
		return pgtype.UUID{}, false
	}
	id := pgtype.UUID{}
	if err := id.Scan(raw); err != nil || !id.Valid {
		return pgtype.UUID{}, false
	}
	return id, true
}

func uuidToString(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	b := v.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parsePositiveInt(v any, fallback int) int {
	raw := strings.TrimSpace(fmt.Sprintf("%v", v))
	if raw == "" || raw == "<nil>" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func normalizeDefenderBanDurationSec(sec int32) int32 {
	if sec < defenderBanMinSec || sec > defenderBanMaxSec {
		return defenderBanDefaultSec
	}
	return sec
}

func parseDefenderBanDurationButtonID(buttonID string) (int32, bool) {
	switch strings.TrimSpace(buttonID) {
	case "def_ban_1h":
		return 60 * 60, true
	case "def_ban_6h":
		return 6 * 60 * 60, true
	case "def_ban_24h":
		return 24 * 60 * 60, true
	case "def_ban_3d":
		return 3 * 24 * 60 * 60, true
	case "def_ban_7d":
		return 7 * 24 * 60 * 60, true
	case "def_ban_30d":
		return 30 * 24 * 60 * 60, true
	default:
		return 0, false
	}
}

func parseDefenderBanDurationInput(raw string) (int32, string, string) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	if len(normalized) < 2 {
		return 0, "Неверный формат. Используй, например: `5m`, `24h`, `3d`.", "Invalid format. Use for example: `5m`, `24h`, `3d`."
	}

	unit := normalized[len(normalized)-1]
	valueRaw := normalized[:len(normalized)-1]
	value, err := strconv.Atoi(valueRaw)
	if err != nil || value <= 0 {
		return 0, "Неверный формат. Используй, например: `5m`, `24h`, `3d`.", "Invalid format. Use for example: `5m`, `24h`, `3d`."
	}

	var sec int32
	switch unit {
	case 'm':
		sec = int32(value * 60)
	case 'h':
		sec = int32(value * 60 * 60)
	case 'd':
		sec = int32(value * 24 * 60 * 60)
	default:
		return 0, "Неверная единица. Разрешено: `m`, `h`, `d`.", "Invalid unit. Allowed: `m`, `h`, `d`."
	}

	if sec < defenderBanMinSec || sec > defenderBanMaxSec {
		return 0, "Срок должен быть в диапазоне от `5m` до `30d`.", "Duration must be between `5m` and `30d`."
	}
	return sec, "", ""
}

func formatDefenderBanDurationLabel(sec int32, language string) string {
	safe := normalizeDefenderBanDurationSec(sec)
	if safe%(24*60*60) == 0 {
		value := safe / (24 * 60 * 60)
		if language == fsm.LangEn {
			return fmt.Sprintf("%dd", value)
		}
		return fmt.Sprintf("%dд", value)
	}
	if safe%(60*60) == 0 {
		value := safe / (60 * 60)
		if language == fsm.LangEn {
			return fmt.Sprintf("%dh", value)
		}
		return fmt.Sprintf("%dч", value)
	}
	value := safe / 60
	if language == fsm.LangEn {
		return fmt.Sprintf("%dm", value)
	}
	return fmt.Sprintf("%dм", value)
}

func buildDefenderManualFilterFromPayload(payload map[string]any) (fsm.DefenderManualFilter, bool, string, string) {
	scopeRaw := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_scope"]))
	if scopeRaw == "" || scopeRaw == "<nil>" || scopeRaw == string(fsm.DefenderManualScopeConfigured) {
		return fsm.DefenderManualFilter{}, false, "", ""
	}
	scope := fsm.DefenderManualScope(scopeRaw)
	switch scope {
	case fsm.DefenderManualScopeUnregistered, fsm.DefenderManualScopeBlocked:
		return fsm.DefenderManualFilter{Scope: scope}, true, "", ""
	case fsm.DefenderManualScopeCampus:
		campusID := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_campus_id"]))
		if campusID == "" || campusID == "<nil>" {
			return fsm.DefenderManualFilter{}, false, "Сначала выбери кампус для очищения.", "Pick a campus first."
		}
		return fsm.DefenderManualFilter{
			Scope:    scope,
			CampusID: campusID,
		}, true, "", ""
	case fsm.DefenderManualScopeTribe:
		campusID := strings.TrimSpace(fmt.Sprintf("%v", payload["defender_cleanup_target_campus_id"]))
		if campusID == "" || campusID == "<nil>" {
			return fsm.DefenderManualFilter{}, false, "Сначала выбери кампус/трайб для очищения.", "Pick campus/tribe first."
		}
		tribeID := int16(parsePositiveInt(payload["defender_cleanup_target_tribe_id"], 0))
		if tribeID <= 0 {
			return fsm.DefenderManualFilter{}, false, "Сначала выбери трайб для очищения.", "Pick a tribe first."
		}
		return fsm.DefenderManualFilter{
			Scope:    scope,
			CampusID: campusID,
			TribeID:  tribeID,
		}, true, "", ""
	default:
		return fsm.DefenderManualFilter{}, false, "", ""
	}
}

func parseWhitelistRemoveID(buttonID string) (int64, bool) {
	after, ok := strings.CutPrefix(buttonID, "def_wl_rm_")
	if !ok || strings.TrimSpace(after) == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(after, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func splitCSVInt64(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]int64, 0, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		result = append(result, n)
	}
	return result
}

func escapeMarkdownV2(v string) string {
	r := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return r.Replace(strings.TrimSpace(v))
}

func escapeMarkdownLinkText(v string) string {
	r := strings.NewReplacer(
		"[", "\\[",
		"]", "\\]",
	)
	return r.Replace(strings.TrimSpace(v))
}

func escapeMarkdownPlain(v string) string {
	r := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"`", "\\`",
	)
	return r.Replace(strings.TrimSpace(v))
}
