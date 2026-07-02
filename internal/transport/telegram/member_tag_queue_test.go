package telegram

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	service "github.com/vgy789/noemx21-bot/internal/service"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func TestLegacyMemberTagQueueAppliesRegisteredProfileInDisabledGroup(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	users := serviceMock.NewMockUserService(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &telegramService{queries: queries, userSvc: users, log: log}
	chatID, userID := int64(-1009001), int64(9001)
	item := db.LegacyMemberTagQueue{ChatID: chatID, TelegramUserID: userID, DesiredAction: "apply"}
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID: chatID, IsActive: true, IsInitialized: true, MemberTagsEnabled: false, MemberTagFormat: memberTagFormatLogin,
	}, nil)
	users.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{
		Login: "student", Status: db.EnumStudentStatusACTIVE,
	}, nil)
	queries.EXPECT().MarkLegacyMemberTagApplied(gomock.Any(), db.MarkLegacyMemberTagAppliedParams{
		ChatID: chatID, TelegramUserID: userID, LastAppliedTag: "student",
	}).Return(nil)

	client := queueTestClient(userID, true, "")
	bot := &gotgbot.Bot{Token: "test", User: gotgbot.User{Id: 9900, IsBot: true}, BotClient: client}
	svc.applyLegacyMemberTagQueueItem(context.Background(), bot, item)
	require.Len(t, client.setTagCalls, 1)
	assert.Equal(t, "student", client.setTagCalls[0].Tag)
}

func TestLegacyMemberTagQueueDoesNotClearUnmanagedTag(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &telegramService{queries: queries, log: log}
	chatID, userID := int64(-1009002), int64(9002)
	item := db.LegacyMemberTagQueue{
		ChatID: chatID, TelegramUserID: userID, DesiredAction: "clear", LastAppliedTag: "managed",
	}
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID: chatID, IsActive: true, IsInitialized: true,
	}, nil)
	queries.EXPECT().MarkLegacyMemberTagSuppressed(gomock.Any(), db.MarkLegacyMemberTagSuppressedParams{
		ChatID: chatID, TelegramUserID: userID,
	}).Return(nil)

	client := queueTestClient(userID, true, "custom")
	bot := &gotgbot.Bot{Token: "test", User: gotgbot.User{Id: 9900, IsBot: true}, BotClient: client}
	svc.applyLegacyMemberTagQueueItem(context.Background(), bot, item)
	assert.Empty(t, client.setTagCalls)
}

func TestLegacyMemberTagQueueRetriesWithoutTagPermission(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &telegramService{queries: queries, log: log}
	chatID, userID := int64(-1009003), int64(9003)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID: chatID, IsActive: true, IsInitialized: true,
	}, nil)
	queries.EXPECT().RetryLegacyMemberTag(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, params db.RetryLegacyMemberTagParams) error {
			assert.NotEmpty(t, params.LastErrorCode)
			return nil
		},
	)
	client := queueTestClient(userID, false, "")
	bot := &gotgbot.Bot{Token: "test", User: gotgbot.User{Id: 9900, IsBot: true}, BotClient: client}
	svc.applyLegacyMemberTagQueueItem(context.Background(), bot, db.LegacyMemberTagQueue{ChatID: chatID, TelegramUserID: userID})
	assert.Empty(t, client.setTagCalls)
}

func queueTestClient(userID int64, canManage bool, tag string) *fakeMemberTagsBotClient {
	return &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		9900: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: canManage, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: 9900, IsBot: true}},
		userID: {Status: gotgbot.ChatMemberStatusMember, Tag: tag, User: struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}{ID: userID}},
	}}
}
