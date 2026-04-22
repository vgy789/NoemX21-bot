package admin_groups

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

func TestLoadGroupTeamNotificationsContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := dbmock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_group_team_notifications_context")
	require.True(t, ok)

	chatID := int64(-1007101)
	userID := int64(52)
	campusID := mustPGUUID(t, "dddddddd-dddd-dddd-dddd-dddddddddddd")

	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                       chatID,
		OwnerTelegramUserID:          userID,
		IsInitialized:                true,
		IsActive:                     true,
		TeamNotificationsEnabled:     true,
		TeamNotificationsThreadID:    31,
		TeamNotificationsThreadLabel: "Topic #31",
		TeamWithdrawnBehavior:        groupPRRWithdrawnDelete,
	}, nil)
	q.EXPECT().ListTelegramGroupTeamProjectFilters(gomock.Any(), chatID).Return([]db.TelegramGroupTeamProjectFilter{
		{ChatID: chatID, ProjectID: 2001},
	}, nil)
	q.EXPECT().GetCatalogProjectTitlesByIDs(gomock.Any(), []int64{2001}).Return([]db.GetCatalogProjectTitlesByIDsRow{
		{ID: 2001, Title: "Rush01"},
	}, nil)
	q.EXPECT().ListTelegramGroupTeamCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupTeamCampusFilter{
		{ChatID: chatID, CampusID: campusID},
	}, nil)
	q.EXPECT().GetCampusByID(gomock.Any(), campusID).Return(db.GetCampusByIDRow{ShortName: "MSK"}, nil)

	_, updates, err := action(context.Background(), userID, map[string]any{
		"selected_group_chat_id": "-1007101",
	})
	require.NoError(t, err)
	assert.Equal(t, true, updates["team_notifications_enabled"])
	assert.Equal(t, "✅ Включено", updates["team_notifications_enabled_label_ru"])
	assert.Equal(t, "Topic #31", updates["team_notifications_destination_label"])
	assert.Equal(t, "Удалять сообщение", updates["team_withdrawn_behavior_label_ru"])
	assert.Equal(t, "Rush01", updates["group_team_project_filters_label_ru"])
	assert.Equal(t, "MSK", updates["group_team_campus_filters_label_ru"])
}

func TestSaveGroupTeamFilters_ReplacesFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := dbmock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("save_group_team_filters")
	require.True(t, ok)

	chatID := int64(-1007102)
	userID := int64(53)
	campus1 := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	campus2 := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	campus1UUID := mustPGUUID(t, campus1)
	campus2UUID := mustPGUUID(t, campus2)

	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil).Times(2)

	q.EXPECT().ClearTelegramGroupTeamProjectFiltersByOwner(gomock.Any(), db.ClearTelegramGroupTeamProjectFiltersByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
	}).Return(int64(1), nil)

	projectIDs := make([]int64, 0, 2)
	q.EXPECT().UpsertTelegramGroupTeamProjectFilterByOwner(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, arg db.UpsertTelegramGroupTeamProjectFilterByOwnerParams) (int64, error) {
			projectIDs = append(projectIDs, arg.ProjectID)
			return 1, nil
		},
	).Times(2)

	q.EXPECT().ClearTelegramGroupTeamCampusFiltersByOwner(gomock.Any(), db.ClearTelegramGroupTeamCampusFiltersByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
	}).Return(int64(1), nil)

	campusIDs := make([]string, 0, 2)
	q.EXPECT().UpsertTelegramGroupTeamCampusFilterByOwner(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, arg db.UpsertTelegramGroupTeamCampusFilterByOwnerParams) (int64, error) {
			campusIDs = append(campusIDs, uuidToString(arg.CampusID))
			return 1, nil
		},
	).Times(2)

	q.EXPECT().ListTelegramGroupTeamProjectFilters(gomock.Any(), chatID).Return([]db.TelegramGroupTeamProjectFilter{}, nil)
	q.EXPECT().ListTelegramGroupTeamCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupTeamCampusFilter{}, nil)

	_, updates, err := action(context.Background(), userID, map[string]any{
		"selected_group_chat_id": "-1007102",
		"filter_project_ids":     []any{"101", "202"},
		"filter_campus_ids":      []any{campus1, campus2},
	})
	require.NoError(t, err)

	slices.Sort(projectIDs)
	assert.Equal(t, []int64{101, 202}, projectIDs)
	slices.Sort(campusIDs)
	assert.Equal(t, []string{uuidToString(campus1UUID), uuidToString(campus2UUID)}, campusIDs)
	assert.Equal(t, false, updates["group_team_filter_draft_loaded"])
}

func TestSetGroupTeamDestinationGeneral(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := dbmock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_team_destination_general")
	require.True(t, ok)

	chatID := int64(-1007103)
	userID := int64(54)

	q.EXPECT().UpdateTelegramGroupTeamNotificationDestinationByOwner(gomock.Any(), db.UpdateTelegramGroupTeamNotificationDestinationByOwnerParams{
		ChatID:                       chatID,
		OwnerTelegramUserID:          userID,
		TeamNotificationsThreadID:    0,
		TeamNotificationsThreadLabel: "Общий чат",
	}).Return(int64(1), nil)

	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                       chatID,
		OwnerTelegramUserID:          userID,
		IsInitialized:                true,
		IsActive:                     true,
		TeamNotificationsThreadLabel: "Общий чат",
	}, nil)
	q.EXPECT().ListTelegramGroupTeamProjectFilters(gomock.Any(), chatID).Return([]db.TelegramGroupTeamProjectFilter{}, nil)
	q.EXPECT().ListTelegramGroupTeamCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupTeamCampusFilter{}, nil)

	_, updates, err := action(context.Background(), userID, map[string]any{
		"selected_group_chat_id": "-1007103",
	})
	require.NoError(t, err)
	assert.Equal(t, "Общий чат", updates["team_notifications_destination_label"])
}
