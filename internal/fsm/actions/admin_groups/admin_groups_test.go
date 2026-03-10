package admin_groups

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"go.uber.org/mock/gomock"
)

type fakeMemberTagRunner struct {
	lastMode   fsm.MemberTagRunMode
	lastUserID int64
	lastChatID int64
	result     fsm.MemberTagRunResult
	err        error
}

type fakeDefenderRunner struct {
	lastUserID      int64
	lastChatID      int64
	result          fsm.DefenderRunResult
	err             error
	resolveDisplay  string
	resolveUsername string
}

func (f *fakeDefenderRunner) RunGroupDefender(_ context.Context, ownerTelegramUserID, chatID int64) (fsm.DefenderRunResult, error) {
	f.lastUserID = ownerTelegramUserID
	f.lastChatID = chatID
	return f.result, f.err
}

func (f *fakeDefenderRunner) PreviewGroupDefenderCandidates(_ context.Context, _, _ int64) ([]fsm.DefenderPreviewItem, error) {
	return nil, nil
}

func (f *fakeDefenderRunner) ResolveGroupMemberIdentity(_ context.Context, _, _, _ int64) (string, string, error) {
	return f.resolveDisplay, f.resolveUsername, nil
}

func (f *fakeMemberTagRunner) RunGroupMemberTags(_ context.Context, ownerTelegramUserID, chatID int64, mode fsm.MemberTagRunMode) (fsm.MemberTagRunResult, error) {
	f.lastMode = mode
	f.lastUserID = ownerTelegramUserID
	f.lastChatID = chatID
	return f.result, f.err
}

func (f *fakeMemberTagRunner) SyncMemberTagsForRegisteredUser(_ context.Context, _ int64) error {
	return nil
}

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

func TestLoadGroupMemberTagsContext_Owner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_group_member_tags_context")
	require.True(t, ok)

	chatID := int64(-100777)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		ChatTitle:           "Alpha",
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
		MemberTagsEnabled:   true,
		MemberTagFormat:     memberTagFormatLoginLevel,
	}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100777",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["can_manage_selected_group"])
	require.Equal(t, true, updates["member_tags_enabled"])
	require.Equal(t, "✅ Включено", updates["member_tags_enabled_label_ru"])
	require.Equal(t, "login [lvl]", updates["member_tag_format_label_ru"])
}

func TestSetGroupMemberTagsEnabled_ByOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_member_tags_enabled")
	require.True(t, ok)

	chatID := int64(-100555)
	q.EXPECT().UpdateTelegramGroupMemberTagsEnabledByOwner(gomock.Any(), db.UpdateTelegramGroupMemberTagsEnabledByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		MemberTagsEnabled:   true,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
		MemberTagsEnabled:   true,
		MemberTagFormat:     memberTagFormatLogin,
	}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100555",
		"id":                     "tags_enable_on",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["member_tags_enabled"])
	require.Equal(t, "✅ Включено", updates["member_tags_enabled_label_ru"])
}

func TestRunGroupMemberTags_UsesRunner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("run_group_member_tags")
	require.True(t, ok)

	chatID := int64(-100909)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	runner := &fakeMemberTagRunner{
		result: fsm.MemberTagRunResult{
			Updated:             3,
			SkippedExisting:     2,
			SkippedUnregistered: 1,
		},
	}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	_, updates, err := action(ctx, 42, map[string]any{
		"selected_group_chat_id": "-100909",
		"id":                     "tags_run_keep",
	})
	require.NoError(t, err)
	require.Equal(t, fsm.MemberTagRunModeKeepExisting, runner.lastMode)
	require.Equal(t, int64(42), runner.lastUserID)
	require.Equal(t, chatID, runner.lastChatID)
	require.True(t, strings.Contains(updates["member_tags_last_run_summary_ru"].(string), "updated=3"))
}

func TestRunGroupMemberTags_ShowsAlertWhenNoRights(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("run_group_member_tags")
	require.True(t, ok)

	chatID := int64(-100707)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	runner := &fakeMemberTagRunner{
		result: fsm.MemberTagRunResult{
			SkippedNoRights: 5,
		},
	}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	_, updates, err := action(ctx, 42, map[string]any{
		"selected_group_chat_id": "-100707",
		"id":                     "tags_run_keep",
	})
	require.NoError(t, err)
	require.Equal(t, alertMemberTagNoRightsRU, updates["_alert"])
}

func TestLoadGroupDefenderContext_Owner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_group_defender_context")
	require.True(t, ok)

	chatID := int64(-100808)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		IsInitialized:         true,
		IsActive:              true,
		DefenderEnabled:       true,
		DefenderRemoveBlocked: true,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), db.ListTelegramGroupWhitelistsParams{
		ChatID:   chatID,
		RowLimit: defenderWhitelistLoadLimit,
	}).Return([]db.TelegramGroupWhitelist{
		{ChatID: chatID, TelegramUserID: 1001},
	}, nil)
	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "1001",
	}).Return(db.UserAccount{}, pgx.ErrNoRows)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), db.ListTelegramGroupLogsParams{
		ChatID:   chatID,
		RowLimit: defenderLogsLoadLimit,
	}).Return([]db.TelegramGroupLog{
		{
			ChatID:         chatID,
			Source:         "auto_join",
			TelegramUserID: 1002,
			Action:         "removed",
			Reason:         "unregistered",
			CreatedAt:      pgtype.Timestamptz{Time: timeNowUTC(), Valid: true},
		},
	}, nil)

	ctx := context.WithValue(context.Background(), fsm.ContextKeyDefenderRunner, &fakeDefenderRunner{
		resolveDisplay:  "saha",
		resolveUsername: "vgy789",
	})

	_, updates, err := action(ctx, 42, map[string]any{
		"selected_group_chat_id": "-100808",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["defender_enabled"])
	require.Equal(t, true, updates["defender_remove_blocked"])
	require.Equal(t, 1, updates["defender_whitelist_count"])
	require.True(t, strings.Contains(updates["defender_whitelist_list_ru"].(string), "[saha](https://t.me/vgy789)"))
	require.True(t, strings.Contains(updates["defender_whitelist_list_ru"].(string), "@vgy789"))
	require.True(t, strings.Contains(updates["defender_whitelist_list_ru"].(string), "`1001`"))
	require.Equal(t, "❌ saha (1001)", updates["defender_whitelist_remove_label_1"])
	require.True(t, strings.Contains(updates["defender_logs_list_ru"].(string), "Автопроверка при входе"))
}

func TestSetGroupDefenderEnabled_ByOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_enabled")
	require.True(t, ok)

	chatID := int64(-100809)
	q.EXPECT().UpdateTelegramGroupDefenderEnabledByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		DefenderEnabled:     true,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		IsInitialized:         true,
		IsActive:              true,
		DefenderEnabled:       true,
		DefenderRemoveBlocked: false,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100809",
		"id":                     "def_enable_on",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["defender_enabled"])
	require.Equal(t, "✅ Включено", updates["defender_enabled_label_ru"])
}

func TestSetGroupDefenderEnabled_OffAutoDisablesRemoveBlocked(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_enabled")
	require.True(t, ok)

	chatID := int64(-100811)
	q.EXPECT().UpdateTelegramGroupDefenderEnabledByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		DefenderEnabled:     false,
	}).Return(int64(1), nil)
	q.EXPECT().UpdateTelegramGroupDefenderRemoveBlockedByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		DefenderRemoveBlocked: false,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		IsInitialized:         true,
		IsActive:              true,
		DefenderEnabled:       false,
		DefenderRemoveBlocked: false,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100811",
		"id":                     "def_enable_off",
	})
	require.NoError(t, err)
	require.Equal(t, false, updates["defender_enabled"])
	require.Equal(t, false, updates["defender_remove_blocked"])
}

func TestSetGroupDefenderRemoveBlocked_AutoEnablesDefender(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_remove_blocked")
	require.True(t, ok)

	chatID := int64(-100812)
	q.EXPECT().UpdateTelegramGroupDefenderRemoveBlockedByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		DefenderRemoveBlocked: true,
	}).Return(int64(1), nil)
	q.EXPECT().UpdateTelegramGroupDefenderEnabledByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		DefenderEnabled:     true,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		IsInitialized:         true,
		IsActive:              true,
		DefenderEnabled:       true,
		DefenderRemoveBlocked: true,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100812",
		"id":                     "def_blocked_on",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["defender_remove_blocked"])
	require.Equal(t, true, updates["defender_enabled"])
}

func TestSetGroupDefenderRemoveBlocked_OffKeepsDefenderEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_remove_blocked")
	require.True(t, ok)

	chatID := int64(-100813)
	q.EXPECT().UpdateTelegramGroupDefenderRemoveBlockedByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderRemoveBlockedByOwnerParams{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		DefenderRemoveBlocked: false,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		OwnerTelegramUserID:   42,
		IsInitialized:         true,
		IsActive:              true,
		DefenderEnabled:       true,
		DefenderRemoveBlocked: false,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100813",
		"id":                     "def_blocked_off",
	})
	require.NoError(t, err)
	require.Equal(t, false, updates["defender_remove_blocked"])
	require.Equal(t, true, updates["defender_enabled"])
}

func TestRunGroupDefender_UsesRunner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("run_group_defender")
	require.True(t, ok)

	chatID := int64(-100810)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil).Times(2)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	runner := &fakeDefenderRunner{
		result: fsm.DefenderRunResult{
			Removed:         2,
			SkippedNoRights: 1,
		},
	}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyDefenderRunner, runner)

	_, updates, err := action(ctx, 42, map[string]any{
		"selected_group_chat_id": "-100810",
	})
	require.NoError(t, err)
	require.Equal(t, int64(42), runner.lastUserID)
	require.Equal(t, chatID, runner.lastChatID)
	require.True(t, strings.Contains(updates["defender_last_run_summary_ru"].(string), "removed=2"))
	require.Equal(t, "⚠️ Нужно право на ban users. Назначьте боту право бана участников и запустите снова.", updates["defender_last_run_notice_ru"])
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}
