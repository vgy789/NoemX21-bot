package telegram

import (
	"context"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/service"
	servicemock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func TestProcessGlobalMemberTagItemPersistsPreviouslyUnknownMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	users := servicemock.NewMockUserService(ctrl)
	const chatID, userID, runID = int64(-1001), int64(42001), int64(7)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID: chatID, IsActive: true, IsInitialized: true, MemberTagsEnabled: true, MemberTagFormat: memberTagFormatLogin,
	}, nil)
	queries.EXPECT().IsTelegramGroupMemberKnown(gomock.Any(), db.IsTelegramGroupMemberKnownParams{ChatID: chatID, TelegramUserID: userID}).Return(false, nil)
	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, arg db.UpsertTelegramGroupMemberParams) (db.TelegramGroupMember, error) {
			assert.True(t, arg.IsMember)
			assert.True(t, arg.LastSeenAt.Valid)
			return db.TelegramGroupMember{ChatID: chatID, TelegramUserID: userID, IsMember: true}, nil
		})
	users.EXPECT().GetProfileByTelegramID(gomock.Any(), userID).Return(&service.UserProfile{Login: "student", Status: db.EnumStudentStatusACTIVE}, nil)
	queries.EXPECT().CompleteGlobalMemberTagRunItem(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, arg db.CompleteGlobalMemberTagRunItemParams) error {
			assert.True(t, arg.Column4, "new membership must be counted as discovered")
			assert.True(t, arg.Column6, "tag update must be counted")
			return nil
		})

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		userID: {Status: gotgbot.ChatMemberStatusMember},
	}}
	bot := &gotgbot.Bot{Token: "test", User: gotgbot.User{Id: 99, IsBot: true}, BotClient: client}
	s := &telegramService{queries: queries, userSvc: users}

	s.processGlobalMemberTagItem(context.Background(), bot, db.GlobalMemberTagRunItem{RunID: runID, ChatID: chatID, TelegramUserID: userID})

	assert.Len(t, client.setTagCalls, 1)
	assert.Equal(t, "student", client.setTagCalls[0].Tag)
}

func TestGlobalMemberTagWorkerDoesNotFinishPreparingRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	queries.EXPECT().GetActiveGlobalMemberTagRun(gomock.Any()).Return(db.GlobalMemberTagRun{ID: 7, State: "preparing"}, nil)
	s := &telegramService{queries: queries}

	s.runGlobalMemberTagStep(context.Background(), &gotgbot.Bot{})
}

func TestGlobalMemberTagTelegramErrorClassification(t *testing.T) {
	assert.True(t, isTelegramMemberAbsentError(&gotgbot.TelegramError{Code: 400, Description: "Bad Request: user not found"}))
	assert.True(t, isPermanentGlobalTelegramError(&gotgbot.TelegramError{Code: 403, Description: "Forbidden"}))
	assert.False(t, isPermanentGlobalTelegramError(&gotgbot.TelegramError{Code: 429, Description: "Too Many Requests"}))
	assert.False(t, isPermanentGlobalTelegramError(&gotgbot.TelegramError{Code: 500, Description: "Internal Server Error"}))
}

func TestStartGroupMemberTagDiscoveryRequiresRecordedOwnerAndQueuesAllCandidates(t *testing.T) {
	ctrl := gomock.NewController(t)
	queries := dbmock.NewMockQuerier(ctrl)
	const chatID, ownerID, botID = int64(-1001), int64(42), int64(99)

	client := &fakeMemberTagsBotClient{members: map[int64]rawChatMember{
		botID: {Status: gotgbot.ChatMemberStatusAdministrator, CanManageTags: true},
	}}
	bot := &gotgbot.Bot{Token: "test", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}
	runner := &telegramMemberTagRunner{svc: &telegramService{queries: queries}, bot: bot}

	status, err := runner.StartGroupMemberTagDiscovery(context.Background(), ownerID, chatID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), status.TotalItems)
	assert.Equal(t, "retired", status.State)
}
