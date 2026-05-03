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
	lastMode        fsm.MemberTagRunMode
	lastUserID      int64
	lastChatID      int64
	result          fsm.MemberTagRunResult
	err             error
	rollbackEntries []fsm.MemberTagRollbackEntry
	rollbackResult  fsm.MemberTagRollbackResult
	rollbackErr     error
	runSnapshot     []fsm.MemberTagRollbackEntry
}

type fakeDefenderRunner struct {
	lastUserID      int64
	lastChatID      int64
	result          fsm.DefenderRunResult
	err             error
	resolveDisplay  string
	resolveUsername string
}

type querierWithUsernameResolver struct {
	*mock.MockQuerier
	usernameAccount db.UserAccount
	usernameErr     error
	lastUsername    string
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

func (q *querierWithUsernameResolver) GetTelegramUserAccountByUsername(_ context.Context, username string) (db.UserAccount, error) {
	q.lastUsername = username
	return q.usernameAccount, q.usernameErr
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

func (f *fakeMemberTagRunner) RunGroupMemberTagsWithRollback(ctx context.Context, ownerTelegramUserID, chatID int64, mode fsm.MemberTagRunMode) (fsm.MemberTagRunResult, []fsm.MemberTagRollbackEntry, error) {
	result, err := f.RunGroupMemberTags(ctx, ownerTelegramUserID, chatID, mode)
	if len(f.runSnapshot) == 0 {
		return result, nil, err
	}
	snapshotCopy := append([]fsm.MemberTagRollbackEntry(nil), f.runSnapshot...)
	return result, snapshotCopy, err
}

func (f *fakeMemberTagRunner) RollbackGroupMemberTags(_ context.Context, ownerTelegramUserID, chatID int64, entries []fsm.MemberTagRollbackEntry) (fsm.MemberTagRollbackResult, error) {
	f.lastUserID = ownerTelegramUserID
	f.lastChatID = chatID
	f.rollbackEntries = append([]fsm.MemberTagRollbackEntry(nil), entries...)
	return f.rollbackResult, f.rollbackErr
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

func TestLoadAdminContext_DedupesManagedGroupsByChatID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("load_admin_context")
	require.True(t, ok)

	oldTS := pgtype.Timestamptz{Time: time.Unix(100, 0), Valid: true}
	newTS := pgtype.Timestamptz{Time: time.Unix(200, 0), Valid: true}

	q.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(db.UserAccount{}, pgx.ErrNoRows)
	q.EXPECT().ListTelegramGroupsByOwner(gomock.Any(), int64(42)).Return([]db.TelegramGroup{
		{ChatID: -100222, ChatTitle: "Beta", UpdatedAt: oldTS},
		{ChatID: -100111, ChatTitle: "Alpha", UpdatedAt: oldTS},
		{ChatID: -100111, ChatTitle: "Alpha Renamed", UpdatedAt: newTS},
	}, nil)

	_, updates, err := action(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, 2, updates["groups_count"])
	require.Equal(t, "-100111", updates["group_chat_id_1"])
	require.Equal(t, "Alpha Renamed", updates["group_title_1"])
	require.Equal(t, "-100222", updates["group_chat_id_2"])
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

func TestRollbackGroupMemberTags_NoOpWithoutManualRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("rollback_group_member_tags")
	require.True(t, ok)

	chatID := int64(-100706)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	runner := &fakeMemberTagRunner{}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	_, updates, err := action(ctx, 42, map[string]any{
		"selected_group_chat_id": "-100706",
	})
	require.NoError(t, err)
	require.Len(t, updates, 0)
	require.Len(t, runner.rollbackEntries, 0)
}

func TestRollbackGroupMemberTags_UsesSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("rollback_group_member_tags")
	require.True(t, ok)

	chatID := int64(-100705)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	entries := []fsm.MemberTagRollbackEntry{
		{TelegramUserID: 1001, PreviousTag: "old-1"},
		{TelegramUserID: 1002, PreviousTag: "old-2"},
	}
	snapshot := encodeMemberTagRollbackSnapshot(memberTagRollbackSnapshot{
		ChatID:  chatID,
		Entries: entries,
	})
	require.NotEmpty(t, snapshot)

	runner := &fakeMemberTagRunner{
		rollbackResult: fsm.MemberTagRollbackResult{
			Restored: len(entries),
		},
	}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	_, updates, err := action(ctx, 42, map[string]any{
		"selected_group_chat_id":  "-100705",
		memberTagRollbackStateKey: snapshot,
	})
	require.NoError(t, err)
	require.Equal(t, entries, runner.rollbackEntries)
	require.Contains(t, updates["member_tags_last_run_summary_ru"].(string), "rollback_restored=2")
	require.Equal(t, "", updates[memberTagRollbackStateKey])
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
	expectEmptyDefenderFilterLists(q, chatID)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                      chatID,
		OwnerTelegramUserID:         42,
		IsInitialized:               true,
		IsActive:                    true,
		DefenderEnabled:             true,
		DefenderRemoveBlocked:       true,
		DefenderRecheckKnownMembers: true,
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
	require.Equal(t, true, updates["defender_recheck_known_members"])
	require.Equal(t, 1, updates["defender_whitelist_count"])
	require.True(t, strings.Contains(updates["defender_whitelist_list_ru"].(string), "[saha](tg://openmessage?user_id=1001)"))
	require.True(t, strings.Contains(updates["defender_whitelist_list_ru"].(string), "@vgy789"))
	require.NotContains(t, updates["defender_whitelist_list_ru"].(string), "`1001`")
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
	expectEmptyDefenderFilterLists(q, chatID)
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
	expectEmptyDefenderFilterLists(q, chatID)
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
	q.EXPECT().UpdateTelegramGroupDefenderRecheckKnownMembersByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderRecheckKnownMembersByOwnerParams{
		ChatID:                      chatID,
		OwnerTelegramUserID:         42,
		DefenderRecheckKnownMembers: false,
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

func TestSetGroupDefenderRecheckKnownMembers_AutoEnablesDefender(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_recheck_known_members")
	require.True(t, ok)

	chatID := int64(-100814)
	expectEmptyDefenderFilterLists(q, chatID)
	q.EXPECT().UpdateTelegramGroupDefenderRecheckKnownMembersByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderRecheckKnownMembersByOwnerParams{
		ChatID:                      chatID,
		OwnerTelegramUserID:         42,
		DefenderRecheckKnownMembers: true,
	}).Return(int64(1), nil)
	q.EXPECT().UpdateTelegramGroupDefenderEnabledByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderEnabledByOwnerParams{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		DefenderEnabled:     true,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                      chatID,
		OwnerTelegramUserID:         42,
		IsInitialized:               true,
		IsActive:                    true,
		DefenderEnabled:             true,
		DefenderRecheckKnownMembers: true,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100814",
		"id":                     "def_recheck_known_on",
	})
	require.NoError(t, err)
	require.Equal(t, true, updates["defender_recheck_known_members"])
	require.Equal(t, true, updates["defender_enabled"])
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
	expectEmptyDefenderFilterLists(q, chatID)
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
	expectEmptyDefenderFilterLists(q, chatID)
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

func TestSetGroupDefenderBanDuration_ByPreset(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_ban_duration")
	require.True(t, ok)

	chatID := int64(-100814)
	expectEmptyDefenderFilterLists(q, chatID)
	q.EXPECT().UpdateTelegramGroupDefenderBanDurationSecByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderBanDurationSecByOwnerParams{
		ChatID:                 chatID,
		OwnerTelegramUserID:    42,
		DefenderBanDurationSec: 6 * 60 * 60,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                 chatID,
		OwnerTelegramUserID:    42,
		IsInitialized:          true,
		IsActive:               true,
		DefenderEnabled:        true,
		DefenderRemoveBlocked:  false,
		DefenderBanDurationSec: 6 * 60 * 60,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100814",
		"id":                     "def_ban_6h",
	})
	require.NoError(t, err)
	require.Equal(t, "6ч", updates["defender_ban_duration_label_ru"])
	require.Equal(t, "6h", updates["defender_ban_duration_label_en"])
}

func TestSetGroupDefenderBanDurationFromInput_Valid(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_ban_duration_from_input")
	require.True(t, ok)

	chatID := int64(-100815)
	expectEmptyDefenderFilterLists(q, chatID)
	q.EXPECT().UpdateTelegramGroupDefenderBanDurationSecByOwner(gomock.Any(), db.UpdateTelegramGroupDefenderBanDurationSecByOwnerParams{
		ChatID:                 chatID,
		OwnerTelegramUserID:    42,
		DefenderBanDurationSec: 72 * 60 * 60,
	}).Return(int64(1), nil)
	q.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                 chatID,
		OwnerTelegramUserID:    42,
		IsInitialized:          true,
		IsActive:               true,
		DefenderBanDurationSec: 72 * 60 * 60,
	}, nil)
	q.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{}, nil)
	q.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	next, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100815",
		"id":                     "3d",
	})
	require.NoError(t, err)
	require.Equal(t, "", next)
	require.Equal(t, "3д", updates["defender_ban_duration_label_ru"])
}

func TestSetGroupDefenderBanDurationFromInput_InvalidRange(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("set_group_defender_ban_duration_from_input")
	require.True(t, ok)

	next, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100816",
		"id":                     "1m",
		"language":               "ru",
	})
	require.NoError(t, err)
	require.Equal(t, "GROUP_DEFENDER_BAN_DURATION_INPUT", next)
	require.Equal(t, "Срок должен быть в диапазоне от `5m` до `30d`.", updates["_alert"])
}

func TestAddGroupDefenderWhitelistFromInput_ByUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	base := mock.NewMockQuerier(ctrl)
	q := &querierWithUsernameResolver{
		MockQuerier: base,
		usernameAccount: db.UserAccount{
			ExternalID: "1001",
		},
	}

	reg := fsm.NewLogicRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Register(reg, &config.Config{}, logger, q, nil)

	action, ok := reg.Get("add_group_defender_whitelist_from_input")
	require.True(t, ok)

	chatID := int64(-100920)
	expectEmptyDefenderFilterLists(base, chatID)
	base.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 42,
		IsInitialized:       true,
		IsActive:            true,
	}, nil).Times(2)
	base.EXPECT().GetUserAccountIDByExternalId(gomock.Any(), db.GetUserAccountIDByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42",
	}).Return(int64(777), nil)
	base.EXPECT().UpsertTelegramGroupWhitelist(gomock.Any(), db.UpsertTelegramGroupWhitelistParams{
		ChatID:           chatID,
		TelegramUserID:   1001,
		AddedByAccountID: 777,
	}).Return(db.TelegramGroupWhitelist{
		ChatID:         chatID,
		TelegramUserID: 1001,
	}, nil)
	base.EXPECT().ListTelegramGroupWhitelists(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupWhitelist{
		{ChatID: chatID, TelegramUserID: 1001},
	}, nil)
	base.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "1001",
	}).Return(db.UserAccount{}, pgx.ErrNoRows)
	base.EXPECT().ListTelegramGroupLogs(gomock.Any(), gomock.Any()).Return([]db.TelegramGroupLog{}, nil)

	_, updates, err := action(context.Background(), 42, map[string]any{
		"selected_group_chat_id": "-100920",
		"id":                     "@vgy789",
	})
	require.NoError(t, err)
	require.Equal(t, "vgy789", q.lastUsername)
	require.Contains(t, updates["defender_preview_summary_ru"].(string), "@vgy789")
	require.Contains(t, updates["defender_preview_summary_ru"].(string), "`1001`")
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
	expectEmptyDefenderFilterLists(q, chatID)
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

func expectEmptyDefenderFilterLists(q *mock.MockQuerier, chatID int64) {
	q.EXPECT().ListTelegramGroupDefenderCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupDefenderCampusFilter{}, nil).AnyTimes()
	q.EXPECT().ListTelegramGroupDefenderTribeFilters(gomock.Any(), chatID).Return([]db.TelegramGroupDefenderTribeFilter{}, nil).AnyTimes()
}
