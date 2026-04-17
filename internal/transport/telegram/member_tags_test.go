package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	service "github.com/vgy789/noemx21-bot/internal/service"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

type setTagCall struct {
	ChatID int64
	UserID int64
	Tag    string
}

type fakeMemberTagsBotClient struct {
	members     map[int64]rawChatMember
	setTagCalls []setTagCall
}

func (f *fakeMemberTagsBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	switch method {
	case "getChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
		}
		member := f.members[userID]
		if member.Status == "" {
			member.Status = gotgbot.ChatMemberStatusMember
		}
		if member.User.ID == 0 {
			member.User.ID = userID
		}
		f.members[userID] = member
		return json.Marshal(member)
	case "setChatMemberTag":
		chatID, ok := toInt64(params["chat_id"])
		if !ok {
			return nil, fmt.Errorf("invalid chat_id param type: %T", params["chat_id"])
		}
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
		}
		tag := fmt.Sprintf("%v", params["tag"])
		member := f.members[userID]
		if member.Status == "" {
			member.Status = gotgbot.ChatMemberStatusMember
		}
		if member.User.ID == 0 {
			member.User.ID = userID
		}
		member.Tag = tag
		f.members[userID] = member
		f.setTagCalls = append(f.setTagCalls, setTagCall{ChatID: chatID, UserID: userID, Tag: tag})
		return json.Marshal(true)
	default:
		return nil, fmt.Errorf("unexpected method: %s", method)
	}
}

func (f *fakeMemberTagsBotClient) GetAPIURL(_ *gotgbot.RequestOpts) string {
	return gotgbot.DefaultAPIURL
}

func (f *fakeMemberTagsBotClient) FileURL(_, _ string, _ *gotgbot.RequestOpts) string {
	return ""
}

func TestHandleChatMember_AutoTagOnJoinWhenEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100601)
	userID := int64(1101)
	group := db.TelegramGroup{ChatID: chatID, OwnerTelegramUserID: 7007, IsInitialized: true, IsActive: true, MemberTagsEnabled: true, MemberTagFormat: memberTagFormatLogin}

	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil).Times(2)
	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{Login: "peer", Level: 21}, nil)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: "", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatMember: &gotgbot.ChatMemberUpdated{
		Chat:          gotgbot.Chat{Id: chatID, Type: "supergroup"},
		OldChatMember: gotgbot.ChatMemberLeft{User: gotgbot.User{Id: userID}},
		NewChatMember: gotgbot.ChatMemberMember{User: gotgbot.User{Id: userID, IsBot: false}},
	}}, nil)

	err := s.handleChatMember(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.setTagCalls, 1)
	assert.Equal(t, userID, client.setTagCalls[0].UserID)
	assert.Equal(t, "peer", client.setTagCalls[0].Tag)
}

func TestHandleChatMember_SkipWhenTagAlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100602)
	userID := int64(1102)
	group := db.TelegramGroup{ChatID: chatID, OwnerTelegramUserID: 7007, IsInitialized: true, IsActive: true, MemberTagsEnabled: true, MemberTagFormat: memberTagFormatLogin}

	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil).Times(2)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: "already_set", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatMember: &gotgbot.ChatMemberUpdated{
		Chat:          gotgbot.Chat{Id: chatID, Type: "supergroup"},
		OldChatMember: gotgbot.ChatMemberLeft{User: gotgbot.User{Id: userID}},
		NewChatMember: gotgbot.ChatMemberMember{User: gotgbot.User{Id: userID, IsBot: false}},
	}}, nil)

	err := s.handleChatMember(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.setTagCalls, 0)
}

func TestHandleChatMember_SkipWhenBotHasNoTagRights(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100603)
	userID := int64(1103)
	group := db.TelegramGroup{ChatID: chatID, OwnerTelegramUserID: 7007, IsInitialized: true, IsActive: true, MemberTagsEnabled: true, MemberTagFormat: memberTagFormatLogin}

	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil).Times(2)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: false, CanEditTag: false, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: "", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatMember: &gotgbot.ChatMemberUpdated{
		Chat:          gotgbot.Chat{Id: chatID, Type: "supergroup"},
		OldChatMember: gotgbot.ChatMemberLeft{User: gotgbot.User{Id: userID}},
		NewChatMember: gotgbot.ChatMemberMember{User: gotgbot.User{Id: userID, IsBot: false}},
	}}, nil)

	err := s.handleChatMember(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.setTagCalls, 0)
}

func TestMemberTagRunner_SyncMemberTagsForRegisteredUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	userID := int64(2101)
	chatID := int64(-100701)
	queries.EXPECT().ListMemberTagGroupsByTelegramUser(gomock.Any(), userID).Return([]db.TelegramGroup{{
		ChatID:          chatID,
		IsInitialized:   true,
		IsActive:        true,
		MemberTagFormat: memberTagFormatLoginLevel,
	}}, nil)
	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{Login: "peer", Level: 21}, nil)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: "", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	runner := s.newMemberTagRunner(bot)
	require.NotNil(t, runner)
	err := runner.SyncMemberTagsForRegisteredUser(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, client.setTagCalls, 1)
	assert.Equal(t, "peer [21]", client.setTagCalls[0].Tag)
}

func TestMemberTagRunner_ManualKeepExisting(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	ownerID := int64(301)
	chatID := int64(-100801)
	user1 := int64(401)
	user2 := int64(402)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
		MemberTagFormat:     memberTagFormatLogin,
	}, nil)
	queries.EXPECT().ListTelegramGroupKnownMembers(gomock.Any(), chatID).Return([]db.TelegramGroupMember{
		{ChatID: chatID, TelegramUserID: user1, IsMember: true},
		{ChatID: chatID, TelegramUserID: user2, IsMember: true},
	}, nil)
	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), user2).Return(&service.UserProfile{Login: "fresh", Level: 7}, nil)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		user1: {Status: gotgbot.ChatMemberStatusMember, Tag: "existing", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: user1}},
		user2: {Status: gotgbot.ChatMemberStatusMember, Tag: "", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: user2}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	runner := s.newMemberTagRunner(bot)
	result, err := runner.RunGroupMemberTags(context.Background(), ownerID, chatID, fsm.MemberTagRunModeKeepExisting)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Updated)
	assert.Equal(t, 1, result.SkippedExisting)
	require.Len(t, client.setTagCalls, 1)
	assert.Equal(t, user2, client.setTagCalls[0].UserID)
	assert.Equal(t, "fresh", client.setTagCalls[0].Tag)
}

func TestMemberTagRunner_ManualClearThenApply(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	ownerID := int64(302)
	chatID := int64(-100802)
	userID := int64(403)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
		MemberTagFormat:     memberTagFormatLoginLevel,
	}, nil)
	queries.EXPECT().ListTelegramGroupKnownMembers(gomock.Any(), chatID).Return([]db.TelegramGroupMember{{
		ChatID: chatID, TelegramUserID: userID, IsMember: true,
	}}, nil)
	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{Login: "gehnaeli", Level: 11}, nil)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: "legacy", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	runner := s.newMemberTagRunner(bot)
	result, err := runner.RunGroupMemberTags(context.Background(), ownerID, chatID, fsm.MemberTagRunModeClearAndApply)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Updated)
	assert.Equal(t, 0, result.Errors)
	require.Len(t, client.setTagCalls, 2)
	assert.Equal(t, "", client.setTagCalls[0].Tag)
	assert.Equal(t, "gehnaeli [11]", client.setTagCalls[1].Tag)
}

func TestMemberTagRunner_ManualRollbackRestoresPreviousTag(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	ownerID := int64(304)
	chatID := int64(-100804)
	userID := int64(405)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
		MemberTagFormat:     memberTagFormatLoginLevel,
	}, nil).Times(2)
	queries.EXPECT().ListTelegramGroupKnownMembers(gomock.Any(), chatID).Return([]db.TelegramGroupMember{{
		ChatID: chatID, TelegramUserID: userID, IsMember: true,
	}}, nil)
	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{Login: "gehnaeli", Level: 11}, nil)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: "legacy", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	runner := s.newMemberTagRunner(bot)
	rollbackRunner, ok := runner.(fsm.MemberTagRollbackRunner)
	require.True(t, ok)

	runResult, snapshot, err := rollbackRunner.RunGroupMemberTagsWithRollback(context.Background(), ownerID, chatID, fsm.MemberTagRunModeClearAndApply)
	require.NoError(t, err)
	assert.Equal(t, 1, runResult.Updated)
	require.Len(t, snapshot, 1)
	assert.Equal(t, userID, snapshot[0].TelegramUserID)
	assert.Equal(t, "legacy", snapshot[0].PreviousTag)

	rollbackResult, err := rollbackRunner.RollbackGroupMemberTags(context.Background(), ownerID, chatID, snapshot)
	require.NoError(t, err)
	assert.Equal(t, 1, rollbackResult.Restored)
	require.Len(t, client.setTagCalls, 3)
	assert.Equal(t, "", client.setTagCalls[0].Tag)
	assert.Equal(t, "gehnaeli [11]", client.setTagCalls[1].Tag)
	assert.Equal(t, "legacy", client.setTagCalls[2].Tag)
}

func TestMemberTagRunner_ManualRunSkipsAdministrators(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	ownerID := int64(303)
	chatID := int64(-100803)
	adminUserID := int64(404)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
		MemberTagFormat:     memberTagFormatLogin,
	}, nil)
	queries.EXPECT().ListTelegramGroupKnownMembers(gomock.Any(), chatID).Return([]db.TelegramGroupMember{
		{ChatID: chatID, TelegramUserID: adminUserID, IsMember: true},
	}, nil)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		adminUserID: {Status: gotgbot.ChatMemberStatusAdministrator, Tag: "admin-tag", User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: adminUserID}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	runner := s.newMemberTagRunner(bot)
	result, err := runner.RunGroupMemberTags(context.Background(), ownerID, chatID, fsm.MemberTagRunModeClearAndApply)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Errors)
	assert.Equal(t, 1, result.SkippedNotMember)
	require.Len(t, client.setTagCalls, 0)
}
