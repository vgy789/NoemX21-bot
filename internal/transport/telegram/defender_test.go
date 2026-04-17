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
	service "github.com/vgy789/noemx21-bot/internal/service"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

type fakeDefenderBotClient struct {
	members      map[int64]rawChatMember
	banCalls     []fakeDefenderBanCall
	unbanCalls   []int64
	approveCalls []int64
	declineCalls []int64
}

type fakeDefenderBanCall struct {
	userID    int64
	untilDate int64
}

func (f *fakeDefenderBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
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
	case "banChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
		}
		untilDate, ok := toInt64(params["until_date"])
		if !ok || untilDate <= 0 {
			return nil, fmt.Errorf("missing or invalid until_date: %#v", params["until_date"])
		}
		f.banCalls = append(f.banCalls, fakeDefenderBanCall{userID: userID, untilDate: untilDate})
		member := f.members[userID]
		member.Status = gotgbot.ChatMemberStatusBanned
		f.members[userID] = member
		return json.Marshal(true)
	case "unbanChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
		}
		f.unbanCalls = append(f.unbanCalls, userID)
		member := f.members[userID]
		member.Status = gotgbot.ChatMemberStatusLeft
		f.members[userID] = member
		return json.Marshal(true)
	case "approveChatJoinRequest":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
		}
		f.approveCalls = append(f.approveCalls, userID)
		return json.Marshal(true)
	case "declineChatJoinRequest":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
		}
		f.declineCalls = append(f.declineCalls, userID)
		return json.Marshal(true)
	default:
		return nil, fmt.Errorf("unexpected method: %s", method)
	}
}

func (f *fakeDefenderBotClient) GetAPIURL(_ *gotgbot.RequestOpts) string {
	return gotgbot.DefaultAPIURL
}

func (f *fakeDefenderBotClient) FileURL(_, _ string, _ *gotgbot.RequestOpts) string {
	return ""
}

func TestHandleChatMember_AutoDefenderRemovesUnregistered(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100901)
	userID := int64(1901)
	expectEmptyDefenderFilters(queries, chatID)
	group := db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: 7007,
		IsInitialized:       true,
		IsActive:            true,
		DefenderEnabled:     true,
	}

	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil).AnyTimes()
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil).Times(2)
	queries.EXPECT().ExistsTelegramGroupWhitelist(gomock.Any(), db.ExistsTelegramGroupWhitelistParams{
		ChatID:         chatID,
		TelegramUserID: userID,
	}).Return(false, nil)
	queries.EXPECT().MarkTelegramGroupMemberLeft(gomock.Any(), gomock.Any()).Return(nil)
	logRows := make([]db.InsertTelegramGroupLogParams, 0)
	queries.EXPECT().InsertTelegramGroupLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg db.InsertTelegramGroupLogParams) error {
		logRows = append(logRows, arg)
		return nil
	}).AnyTimes()
	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(nil, fmt.Errorf("user account not found"))

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		9000: {
			Status:      gotgbot.ChatMemberStatusAdministrator,
			CanRestrict: true,
			User: struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			}{ID: 9000, IsBot: true},
		},
		userID: {
			Status: gotgbot.ChatMemberStatusMember,
			User: struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			}{ID: userID, IsBot: false},
		},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatMember: &gotgbot.ChatMemberUpdated{
		Chat:          gotgbot.Chat{Id: chatID, Type: "supergroup"},
		OldChatMember: gotgbot.ChatMemberLeft{User: gotgbot.User{Id: userID}},
		NewChatMember: gotgbot.ChatMemberMember{User: gotgbot.User{Id: userID, IsBot: false}},
	}}, nil)

	err := s.handleChatMember(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.banCalls, 1)
	require.Len(t, client.unbanCalls, 0)
	assert.Equal(t, userID, client.banCalls[0].userID)
	assert.NotZero(t, client.banCalls[0].untilDate)
	require.NotEmpty(t, logRows)
	foundRemoved := false
	for _, row := range logRows {
		if row.Action == "removed" && row.Reason == "unregistered" {
			foundRemoved = true
			assert.Contains(t, row.Details, "duration_sec=")
			assert.Contains(t, row.Details, "until_utc=")
		}
	}
	assert.True(t, foundRemoved)
}

func TestHandleChatMember_AutoDefenderSkipsWhitelisted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100902)
	userID := int64(1902)
	expectEmptyDefenderFilters(queries, chatID)
	group := db.TelegramGroup{ChatID: chatID, OwnerTelegramUserID: 7007, IsInitialized: true, IsActive: true, DefenderEnabled: true}

	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil).Times(2)
	queries.EXPECT().ExistsTelegramGroupWhitelist(gomock.Any(), db.ExistsTelegramGroupWhitelistParams{ChatID: chatID, TelegramUserID: userID}).Return(true, nil)
	queries.EXPECT().InsertTelegramGroupLog(gomock.Any(), gomock.Any()).Return(nil)

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanRestrict: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID, IsBot: false}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatMember: &gotgbot.ChatMemberUpdated{
		Chat:          gotgbot.Chat{Id: chatID, Type: "supergroup"},
		OldChatMember: gotgbot.ChatMemberLeft{User: gotgbot.User{Id: userID}},
		NewChatMember: gotgbot.ChatMemberMember{User: gotgbot.User{Id: userID, IsBot: false}},
	}}, nil)

	err := s.handleChatMember(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.banCalls, 0)
	require.Len(t, client.unbanCalls, 0)
}

func TestHandleChatJoinRequest_AutoDefenderDeclinesUnregistered(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100912)
	userID := int64(1912)
	expectEmptyDefenderFilters(queries, chatID)
	group := db.TelegramGroup{
		ChatID:          chatID,
		IsInitialized:   true,
		IsActive:        true,
		DefenderEnabled: true,
	}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil)
	queries.EXPECT().ExistsTelegramGroupWhitelist(gomock.Any(), db.ExistsTelegramGroupWhitelistParams{
		ChatID:         chatID,
		TelegramUserID: userID,
	}).Return(false, nil)

	logRows := make([]db.InsertTelegramGroupLogParams, 0)
	queries.EXPECT().InsertTelegramGroupLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg db.InsertTelegramGroupLogParams) error {
		logRows = append(logRows, arg)
		return nil
	}).AnyTimes()

	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(nil, fmt.Errorf("user account not found"))

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		9000: {
			Status:         gotgbot.ChatMemberStatusAdministrator,
			CanInviteUsers: true,
			User: struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			}{ID: 9000, IsBot: true},
		},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatJoinRequest: &gotgbot.ChatJoinRequest{
		Chat: gotgbot.Chat{Id: chatID, Type: "supergroup"},
		From: gotgbot.User{Id: userID, IsBot: false},
	}}, nil)

	err := s.handleChatJoinRequest(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.approveCalls, 0)
	require.Len(t, client.declineCalls, 1)
	assert.Equal(t, userID, client.declineCalls[0])
	require.NotEmpty(t, logRows)
	assert.Equal(t, "auto_join_request", logRows[0].Source)
	assert.Equal(t, "declined", logRows[0].Action)
	assert.Equal(t, "unregistered", logRows[0].Reason)
}

func TestHandleChatJoinRequest_AutoDefenderApprovesWhitelisted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100913)
	userID := int64(1913)
	group := db.TelegramGroup{
		ChatID:          chatID,
		IsInitialized:   true,
		IsActive:        true,
		DefenderEnabled: true,
	}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil)
	queries.EXPECT().ExistsTelegramGroupWhitelist(gomock.Any(), db.ExistsTelegramGroupWhitelistParams{
		ChatID:         chatID,
		TelegramUserID: userID,
	}).Return(true, nil)

	logRows := make([]db.InsertTelegramGroupLogParams, 0)
	queries.EXPECT().InsertTelegramGroupLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg db.InsertTelegramGroupLogParams) error {
		logRows = append(logRows, arg)
		return nil
	}).AnyTimes()

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		9000: {
			Status:         gotgbot.ChatMemberStatusAdministrator,
			CanInviteUsers: true,
			User: struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			}{ID: 9000, IsBot: true},
		},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatJoinRequest: &gotgbot.ChatJoinRequest{
		Chat: gotgbot.Chat{Id: chatID, Type: "supergroup"},
		From: gotgbot.User{Id: userID, IsBot: false},
	}}, nil)

	err := s.handleChatJoinRequest(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.approveCalls, 1)
	require.Len(t, client.declineCalls, 0)
	assert.Equal(t, userID, client.approveCalls[0])
	require.NotEmpty(t, logRows)
	assert.Equal(t, "auto_join_request", logRows[0].Source)
	assert.Equal(t, "approved", logRows[0].Action)
	assert.Equal(t, "whitelist", logRows[0].Reason)
}

func TestHandleChatJoinRequest_AutoDefenderSkipsWithoutInviteRights(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	chatID := int64(-100914)
	userID := int64(1914)
	group := db.TelegramGroup{
		ChatID:          chatID,
		IsInitialized:   true,
		IsActive:        true,
		DefenderEnabled: true,
	}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(group, nil)

	logRows := make([]db.InsertTelegramGroupLogParams, 0)
	queries.EXPECT().InsertTelegramGroupLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg db.InsertTelegramGroupLogParams) error {
		logRows = append(logRows, arg)
		return nil
	}).AnyTimes()

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		9000: {
			Status:         gotgbot.ChatMemberStatusAdministrator,
			CanInviteUsers: false,
			User: struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			}{ID: 9000, IsBot: true},
		},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	ctx := ext.NewContext(bot, &gotgbot.Update{ChatJoinRequest: &gotgbot.ChatJoinRequest{
		Chat: gotgbot.Chat{Id: chatID, Type: "supergroup"},
		From: gotgbot.User{Id: userID, IsBot: false},
	}}, nil)

	err := s.handleChatJoinRequest(bot, ctx)
	require.NoError(t, err)
	require.Len(t, client.approveCalls, 0)
	require.Len(t, client.declineCalls, 0)
	require.NotEmpty(t, logRows)
	assert.Equal(t, "skipped_no_rights", logRows[0].Action)
	assert.Equal(t, "bot_rights", logRows[0].Reason)
}

func TestDefenderRunner_ManualRunBlockedUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	userSvc := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries, userSvc: userSvc}

	ownerID := int64(5001)
	chatID := int64(-100903)
	userID := int64(1903)
	expectEmptyDefenderFilters(queries, chatID)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		OwnerTelegramUserID:   ownerID,
		IsInitialized:         true,
		IsActive:              true,
		DefenderEnabled:       true,
		DefenderRemoveBlocked: true,
	}, nil)
	queries.EXPECT().ListTelegramGroupKnownMembers(gomock.Any(), chatID).Return([]db.TelegramGroupMember{
		{ChatID: chatID, TelegramUserID: userID, IsMember: true},
	}, nil)
	queries.EXPECT().ExistsTelegramGroupWhitelist(gomock.Any(), db.ExistsTelegramGroupWhitelistParams{
		ChatID:         chatID,
		TelegramUserID: userID,
	}).Return(false, nil)
	queries.EXPECT().MarkTelegramGroupMemberLeft(gomock.Any(), gomock.Any()).Return(nil)
	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil).AnyTimes()
	logRows := make([]db.InsertTelegramGroupLogParams, 0)
	queries.EXPECT().InsertTelegramGroupLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg db.InsertTelegramGroupLogParams) error {
		logRows = append(logRows, arg)
		return nil
	}).AnyTimes()

	userSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{
		Login:  "blocked_user",
		Status: db.EnumStudentStatusBLOCKED,
	}, nil)

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		9000: {Status: gotgbot.ChatMemberStatusAdministrator, CanRestrict: true, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9000, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID, IsBot: false}},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: 9000, IsBot: true}, BotClient: client}

	runner := s.newDefenderRunner(bot)
	require.NotNil(t, runner)
	result, err := runner.RunGroupDefender(context.Background(), ownerID, chatID)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Removed)
	assert.Equal(t, 1, result.SkippedBlocked)
	require.Len(t, client.banCalls, 1)
	require.Len(t, client.unbanCalls, 0)
	require.NotEmpty(t, logRows)
	foundRemoved := false
	for _, row := range logRows {
		if row.Action == "removed" && row.Reason == "blocked" {
			foundRemoved = true
			assert.Contains(t, row.Details, "duration_sec=")
			assert.Contains(t, row.Details, "until_utc=")
		}
	}
	assert.True(t, foundRemoved)
}

func expectEmptyDefenderFilters(queries *dbmock.MockQuerier, chatID int64) {
	queries.EXPECT().ListTelegramGroupDefenderCampusFilters(gomock.Any(), chatID).Return([]db.TelegramGroupDefenderCampusFilter{}, nil).AnyTimes()
	queries.EXPECT().ListTelegramGroupDefenderTribeFilters(gomock.Any(), chatID).Return([]db.TelegramGroupDefenderTribeFilter{}, nil).AnyTimes()
}
