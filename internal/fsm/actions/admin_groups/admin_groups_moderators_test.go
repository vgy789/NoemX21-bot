package admin_groups

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

type querierWithModerators struct {
	*mock.MockQuerier

	moderatorsByChat         map[int64]map[int64]db.TelegramGroupModerator
	managedGroupsByUID       map[int64][]db.TelegramGroup
	loginAccounts            map[string]db.UserAccount
	usernameAccounts         map[string]db.UserAccount
	moderationCommandsByChat map[int64]bool
}

func (q *querierWithModerators) ListTelegramGroupsManagedByUser(_ context.Context, telegramUserID int64) ([]db.TelegramGroup, error) {
	if groups, ok := q.managedGroupsByUID[telegramUserID]; ok {
		return append([]db.TelegramGroup(nil), groups...), nil
	}
	return nil, nil
}

func (q *querierWithModerators) ExistsTelegramGroupModeratorFullAccess(_ context.Context, chatID, telegramUserID int64) (bool, error) {
	chatMods, ok := q.moderatorsByChat[chatID]
	if !ok {
		return false, nil
	}
	row, ok := chatMods[telegramUserID]
	if !ok {
		return false, nil
	}
	return row.FullAccess, nil
}

func (q *querierWithModerators) ListTelegramGroupModerators(_ context.Context, chatID int64) ([]db.TelegramGroupModerator, error) {
	chatMods, ok := q.moderatorsByChat[chatID]
	if !ok {
		return nil, nil
	}
	rows := make([]db.TelegramGroupModerator, 0, len(chatMods))
	for _, row := range chatMods {
		rows = append(rows, row)
	}
	return rows, nil
}

func (q *querierWithModerators) UpsertTelegramGroupModerator(_ context.Context, arg db.UpsertTelegramGroupModeratorParams) (db.TelegramGroupModerator, error) {
	if q.moderatorsByChat == nil {
		q.moderatorsByChat = map[int64]map[int64]db.TelegramGroupModerator{}
	}
	if _, ok := q.moderatorsByChat[arg.ChatID]; !ok {
		q.moderatorsByChat[arg.ChatID] = map[int64]db.TelegramGroupModerator{}
	}

	row := db.TelegramGroupModerator{
		ChatID:           arg.ChatID,
		TelegramUserID:   arg.TelegramUserID,
		CanBan:           arg.CanBan,
		CanMute:          arg.CanMute,
		FullAccess:       arg.FullAccess,
		AddedByAccountID: arg.AddedByAccountID,
	}
	q.moderatorsByChat[arg.ChatID][arg.TelegramUserID] = row
	return row, nil
}

func (q *querierWithModerators) GetTelegramGroupModeratorByChatAndUser(_ context.Context, chatID, telegramUserID int64) (db.TelegramGroupModerator, error) {
	chatMods, ok := q.moderatorsByChat[chatID]
	if !ok {
		return db.TelegramGroupModerator{}, pgx.ErrNoRows
	}
	row, ok := chatMods[telegramUserID]
	if !ok {
		return db.TelegramGroupModerator{}, pgx.ErrNoRows
	}
	return row, nil
}

func (q *querierWithModerators) UpdateTelegramGroupModeratorPermissions(_ context.Context, arg db.UpdateTelegramGroupModeratorPermissionsParams) (db.TelegramGroupModerator, error) {
	chatMods, ok := q.moderatorsByChat[arg.ChatID]
	if !ok {
		return db.TelegramGroupModerator{}, pgx.ErrNoRows
	}
	row, ok := chatMods[arg.TelegramUserID]
	if !ok {
		return db.TelegramGroupModerator{}, pgx.ErrNoRows
	}
	row.CanBan = arg.CanBan
	row.CanMute = arg.CanMute
	row.FullAccess = arg.FullAccess
	chatMods[arg.TelegramUserID] = row
	return row, nil
}

func (q *querierWithModerators) DeleteTelegramGroupModeratorByChatAndUser(_ context.Context, chatID, telegramUserID int64) error {
	chatMods, ok := q.moderatorsByChat[chatID]
	if !ok {
		return nil
	}
	delete(chatMods, telegramUserID)
	return nil
}

func (q *querierWithModerators) GetTelegramUserAccountByS21Login(_ context.Context, s21Login string) (db.UserAccount, error) {
	if acc, ok := q.loginAccounts[strings.ToLower(strings.TrimSpace(s21Login))]; ok {
		return acc, nil
	}
	return db.UserAccount{}, pgx.ErrNoRows
}

func (q *querierWithModerators) GetTelegramUserAccountByUsername(_ context.Context, username string) (db.UserAccount, error) {
	if acc, ok := q.usernameAccounts[strings.ToLower(strings.Trim(strings.TrimSpace(username), "@"))]; ok {
		return acc, nil
	}
	return db.UserAccount{}, pgx.ErrNoRows
}

func (q *querierWithModerators) GetTelegramGroupModerationCommandsEnabledByChatID(_ context.Context, chatID int64) (bool, error) {
	if q.moderationCommandsByChat == nil {
		return true, nil
	}
	enabled, ok := q.moderationCommandsByChat[chatID]
	if !ok {
		return true, nil
	}
	return enabled, nil
}

func (q *querierWithModerators) UpdateTelegramGroupModerationCommandsEnabled(_ context.Context, arg db.UpdateTelegramGroupModerationCommandsEnabledParams) (int64, error) {
	if q.moderationCommandsByChat == nil {
		q.moderationCommandsByChat = map[int64]bool{}
	}
	q.moderationCommandsByChat[arg.ChatID] = arg.ModerationCommandsEnabled
	return 1, nil
}

func TestLoadGroupModeratorsContext_LoadsRows(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	base := mock.NewMockQuerier(ctrl)
	q := &querierWithModerators{
		MockQuerier: base,
		moderatorsByChat: map[int64]map[int64]db.TelegramGroupModerator{
			-1001: {
				1001: {ChatID: -1001, TelegramUserID: 1001, CanBan: true, CanMute: false, FullAccess: true},
			},
		},
	}

	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_group_moderators_context")
	require.True(t, ok)

	base.EXPECT().GetTelegramGroupByChatID(gomock.Any(), int64(-1001)).Return(db.TelegramGroup{
		ChatID:              -1001,
		ChatTitle:           "Alpha",
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	base.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "1001",
	}).Return(db.UserAccount{S21Login: "tester"}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-1001",
		"selected_group_title":   "Alpha",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["can_manage_selected_group"])
	require.Equal(t, "grp_mod_sel_1001", updates["moderator_button_id_1"])
	require.Contains(t, updates["moderators_list_formatted_ru"].(string), "бан=✅")
}

func TestAddGroupModeratorFromInput_ByLogin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	base := mock.NewMockQuerier(ctrl)
	q := &querierWithModerators{
		MockQuerier:      base,
		moderatorsByChat: map[int64]map[int64]db.TelegramGroupModerator{},
		loginAccounts:    map[string]db.UserAccount{},
	}

	tgID := int64(2002)
	q.loginAccounts["newmod"] = db.UserAccount{
		S21Login:   "newmod",
		Platform:   db.EnumPlatformTelegram,
		ExternalID: strconv.FormatInt(tgID, 10),
	}

	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("add_group_moderator_from_input")
	require.True(t, ok)

	base.EXPECT().GetTelegramGroupByChatID(gomock.Any(), int64(-1002)).Return(db.TelegramGroup{
		ChatID:              -1002,
		ChatTitle:           "Beta",
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	base.EXPECT().GetUserAccountIDByExternalId(gomock.Any(), db.GetUserAccountIDByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(int64(777), nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-1002",
		"id":                     "newmod",
	})
	require.NoError(t, err)
	require.Equal(t, "2002", updates[targetModeratorStateKey])
	require.Contains(t, updates["_alert"].(string), "добавлен")

	row, err := q.GetTelegramGroupModeratorByChatAndUser(context.Background(), -1002, tgID)
	require.NoError(t, err)
	require.False(t, row.CanBan)
	require.False(t, row.CanMute)
	require.False(t, row.FullAccess)
}

func TestToggleModeratorPermission_TogglesBan(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	base := mock.NewMockQuerier(ctrl)
	q := &querierWithModerators{
		MockQuerier: base,
		moderatorsByChat: map[int64]map[int64]db.TelegramGroupModerator{
			-1003: {
				3003: {ChatID: -1003, TelegramUserID: 3003, CanBan: false, CanMute: false, FullAccess: false},
			},
		},
	}

	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("toggle_moderator_permission")
	require.True(t, ok)

	base.EXPECT().GetTelegramGroupByChatID(gomock.Any(), int64(-1003)).Return(db.TelegramGroup{
		ChatID:              -1003,
		ChatTitle:           "Gamma",
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	base.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "3003",
	}).Return(db.UserAccount{}, errors.New("no rows"))

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id":    "-1003",
		targetModeratorStateKey:     "3003",
		"id":                        "toggle_perm_ban",
		"selected_group_title":      "Gamma",
		"can_manage_selected_group": true,
	})
	require.NoError(t, err)
	require.Equal(t, "✅", updates["mod_can_ban_status_ru"])

	row, err := q.GetTelegramGroupModeratorByChatAndUser(context.Background(), -1003, 3003)
	require.NoError(t, err)
	require.True(t, row.CanBan)
}

func TestSetGroupModerationCommandsEnabled_TogglesOff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	base := mock.NewMockQuerier(ctrl)
	q := &querierWithModerators{
		MockQuerier:              base,
		moderationCommandsByChat: map[int64]bool{-1006: true},
	}

	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_moderation_commands_enabled")
	require.True(t, ok)

	base.EXPECT().GetTelegramGroupByChatID(gomock.Any(), int64(-1006)).Return(db.TelegramGroup{
		ChatID:              -1006,
		ChatTitle:           "Zeta",
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-1006",
		"id":                     "mod_cmds_disable",
	})
	require.NoError(t, err)
	require.Contains(t, updates["_alert"].(string), "выключены")
	require.Equal(t, false, q.moderationCommandsByChat[-1006])
}

func TestLoadAdminContext_UsesManagedGroupsLister(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	base := mock.NewMockQuerier(ctrl)
	q := &querierWithModerators{
		MockQuerier: base,
		managedGroupsByUID: map[int64][]db.TelegramGroup{
			42: {
				{ChatID: -1004, ChatTitle: "Delta", IsActive: true, IsInitialized: true},
			},
		},
	}

	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_admin_context")
	require.True(t, ok)

	base.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{}, errors.New("not found"))

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, true, updates["is_group_owner"])
	require.Equal(t, "-1004", updates["group_chat_id_1"])
}
