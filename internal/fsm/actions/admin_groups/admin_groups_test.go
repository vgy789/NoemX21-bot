package admin_groups

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

func TestLoadAdminContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "bot_owner"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	Register(reg, cfg, logger, q, nil)

	action, ok := reg.Get("load_admin_context")
	require.True(t, ok)

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{S21Login: "bot_owner"}, nil)
	q.EXPECT().ListTelegramGroupsByOwner(gomock.Any(), int64(42)).Return([]db.TelegramGroup{
		{ChatID: -100111, ChatTitle: "Alpha"},
		{ChatID: -100222, ChatTitle: "Beta"},
	}, nil)

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, true, updates["is_group_owner"])
	require.Equal(t, true, updates["is_bot_owner"])
	require.Equal(t, 2, updates["groups_count"])
	require.Equal(t, "-100111", updates["group_chat_id_1"])
	require.Equal(t, "group_-100111", updates["group_button_id_1"])
	require.Equal(t, "Alpha", updates["group_title_1"])
	require.Equal(t, "-100222", updates["group_chat_id_2"])
	require.Equal(t, "group_-100222", updates["group_button_id_2"])
	require.Equal(t, "Beta", updates["group_title_2"])
	require.Equal(t, "", updates["group_chat_id_3"])
}

func TestSelectAdminGroup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("select_admin_group")
	require.True(t, ok)

	payload := map[string]any{
		"id":                "group_-100999",
		"group_button_id_1": "group_-100111",
		"group_chat_id_1":   "-100111",
		"group_title_1":     "Alpha",
		"group_button_id_2": "group_-100999",
		"group_chat_id_2":   "-100999",
		"group_title_2":     "Gamma",
	}

	_, updates, err := action(context.Background(), 1, payload)
	require.NoError(t, err)
	require.Equal(t, "-100999", updates["selected_group_chat_id"])
	require.Equal(t, "Gamma", updates["selected_group_title"])
}

func TestLoadAdminContext_OwnerTransferVisibility(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_admin_context")
	require.True(t, ok)

	// Old owner: no groups after ownership transfer.
	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "100",
	}).Return(db.UserAccount{}, pgx.ErrNoRows)
	q.EXPECT().ListTelegramGroupsByOwner(gomock.Any(), int64(100)).Return([]db.TelegramGroup{}, nil)

	_, updatesOld, err := action(context.Background(), 100, nil)
	require.NoError(t, err)
	require.Equal(t, false, updatesOld["is_group_owner"])

	// New owner: sees transferred group.
	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "200",
	}).Return(db.UserAccount{}, pgx.ErrNoRows)
	q.EXPECT().ListTelegramGroupsByOwner(gomock.Any(), int64(200)).Return([]db.TelegramGroup{
		{ChatID: -100500, ChatTitle: "Transferred Group"},
	}, nil)

	_, updatesNew, err := action(context.Background(), 200, nil)
	require.NoError(t, err)
	require.Equal(t, true, updatesNew["is_group_owner"])
	require.Equal(t, "-100500", updatesNew["group_chat_id_1"])
	require.Equal(t, "Transferred Group", updatesNew["group_title_1"])
}
