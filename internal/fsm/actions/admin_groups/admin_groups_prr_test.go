package admin_groups

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

func TestLoadGroupPRRNotificationsContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := dbmock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_group_prr_notifications_context")
	require.True(t, ok)

	chatID := int64(-1007001)
	userID := int64(42)
	campusID := mustPGUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                      chatID,
		OwnerTelegramUserID:         userID,
		IsInitialized:               true,
		IsActive:                    true,
		PrrNotificationsEnabled:     true,
		PrrNotificationsThreadID:    21,
		PrrNotificationsThreadLabel: "Topic #21",
		PrrWithdrawnBehavior:        groupPRRWithdrawnDelete,
	}, nil)
	q.EXPECT().ListTelegramGroupPRRProjectFilters(gomock.Any(), chatID).Return([]db.TelegramGroupPrrProjectFilter{
		{ChatID: chatID, ProjectID: 1001},
	}, nil)
	q.EXPECT().GetCatalogProjectTitlesByIDs(gomock.Any(), []int64{1001}).Return([]db.GetCatalogProjectTitlesByIDsRow{
		{ID: 1001, Title: "CPP3"},
	}, nil)
	q.EXPECT().ListTelegramGroupPRRCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupPrrCampusFilter{
		{ChatID: chatID, CampusID: campusID},
	}, nil)
	q.EXPECT().GetCampusByID(gomock.Any(), campusID).Return(db.GetCampusByIDRow{ShortName: "MSK"}, nil)

	_, updates, err := action(context.Background(), userID, map[string]any{
		"selected_group_chat_id": "-1007001",
	})
	require.NoError(t, err)
	assert.Equal(t, true, updates["prr_notifications_enabled"])
	assert.Equal(t, "✅ Включено", updates["prr_notifications_enabled_label_ru"])
	assert.Equal(t, "Topic #21", updates["prr_notifications_destination_label"])
	assert.Equal(t, "Удалять сообщение", updates["prr_withdrawn_behavior_label_ru"])
	assert.Equal(t, "CPP3", updates["group_prr_project_filters_label_ru"])
	assert.Equal(t, "MSK", updates["group_prr_campus_filters_label_ru"])
}

func TestSaveGroupPRRFilters_ReplacesFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := dbmock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("save_group_prr_filters")
	require.True(t, ok)

	chatID := int64(-1007002)
	userID := int64(43)
	campus1 := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	campus2 := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	campus1UUID := mustPGUUID(t, campus1)
	campus2UUID := mustPGUUID(t, campus2)

	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil).Times(2)

	q.EXPECT().ClearTelegramGroupPRRProjectFiltersByOwner(gomock.Any(), db.ClearTelegramGroupPRRProjectFiltersByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
	}).Return(int64(1), nil)

	projectIDs := make([]int64, 0, 2)
	q.EXPECT().UpsertTelegramGroupPRRProjectFilterByOwner(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, arg db.UpsertTelegramGroupPRRProjectFilterByOwnerParams) (int64, error) {
			projectIDs = append(projectIDs, arg.ProjectID)
			return 1, nil
		},
	).Times(2)

	q.EXPECT().ClearTelegramGroupPRRCampusFiltersByOwner(gomock.Any(), db.ClearTelegramGroupPRRCampusFiltersByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
	}).Return(int64(1), nil)

	campusIDs := make([]string, 0, 2)
	q.EXPECT().UpsertTelegramGroupPRRCampusFilterByOwner(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, arg db.UpsertTelegramGroupPRRCampusFilterByOwnerParams) (int64, error) {
			campusIDs = append(campusIDs, uuidToString(arg.CampusID))
			return 1, nil
		},
	).Times(2)

	q.EXPECT().ListTelegramGroupPRRProjectFilters(gomock.Any(), chatID).Return([]db.TelegramGroupPrrProjectFilter{}, nil)
	q.EXPECT().ListTelegramGroupPRRCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupPrrCampusFilter{}, nil)

	_, updates, err := action(context.Background(), userID, map[string]any{
		"selected_group_chat_id": "-1007002",
		"filter_project_ids":     []any{"101", "202"},
		"filter_campus_ids":      []any{campus1, campus2},
	})
	require.NoError(t, err)

	slices.Sort(projectIDs)
	assert.Equal(t, []int64{101, 202}, projectIDs)
	slices.Sort(campusIDs)
	assert.Equal(t, []string{uuidToString(campus1UUID), uuidToString(campus2UUID)}, campusIDs)
	assert.Equal(t, false, updates["group_prr_filter_draft_loaded"])
}

func TestSetGroupPRRDestinationGeneral(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := dbmock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_prr_destination_general")
	require.True(t, ok)

	chatID := int64(-1007003)
	userID := int64(44)

	q.EXPECT().UpdateTelegramGroupPRRNotificationDestinationByOwner(gomock.Any(), db.UpdateTelegramGroupPRRNotificationDestinationByOwnerParams{
		ChatID:                      chatID,
		OwnerTelegramUserID:         userID,
		PrrNotificationsThreadID:    0,
		PrrNotificationsThreadLabel: "Общий чат",
	}).Return(int64(1), nil)

	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                      chatID,
		OwnerTelegramUserID:         userID,
		IsInitialized:               true,
		IsActive:                    true,
		PrrNotificationsThreadLabel: "Общий чат",
	}, nil)
	q.EXPECT().ListTelegramGroupPRRProjectFilters(gomock.Any(), chatID).Return([]db.TelegramGroupPrrProjectFilter{}, nil)
	q.EXPECT().ListTelegramGroupPRRCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupPrrCampusFilter{}, nil)

	_, updates, err := action(context.Background(), userID, map[string]any{
		"selected_group_chat_id": "-1007003",
	})
	require.NoError(t, err)
	assert.Equal(t, "Общий чат", updates["prr_notifications_destination_label"])
}

func mustPGUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	require.NoError(t, id.Scan(raw))
	return id
}
