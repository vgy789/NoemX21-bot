package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	tgmock "github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

func TestRunTeamGroupBroadcast_PublishRequest_WithThreadAndFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	requestID := int64(601)
	projectID := int64(9101)
	chatMatch := int64(-1003001)
	chatSkip := int64(-1003002)
	campusID := mustUUID(t, "44444444-4444-4444-4444-444444444444")

	queries.EXPECT().GetTeamSearchRequestByID(gomock.Any(), requestID).Return(db.GetTeamSearchRequestByIDRow{
		ID:                        requestID,
		ProjectID:                 projectID,
		ProjectName:               "Rush01",
		ProjectType:               "GROUP",
		RequesterS21Login:         "builder",
		RequesterCampusID:         campusID,
		RequesterCampusName:       "MSK",
		PlannedStartText:          "tomorrow 19:00",
		RequestNoteText:           "Need 2 backend devs",
		RequesterLevel:            "9",
		RequesterTelegramUsername: "buildertg",
	}, nil)
	queries.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "builder").Return(db.RegisteredUser{
		S21Login:     "builder",
		RocketchatID: "rc-builder",
	}, nil)

	queries.EXPECT().ListTelegramGroupsWithTeamNotifications(gomock.Any()).Return([]db.TelegramGroup{
		{ChatID: chatMatch, TeamNotificationsThreadID: 78},
		{ChatID: chatSkip},
	}, nil)

	queries.EXPECT().ListTelegramGroupTeamProjectFilters(gomock.Any(), chatMatch).Return([]db.TelegramGroupTeamProjectFilter{
		{ChatID: chatMatch, ProjectID: projectID},
	}, nil)
	queries.EXPECT().ListTelegramGroupTeamCampusFilters(gomock.Any(), chatMatch).Return([]db.TelegramGroupTeamCampusFilter{
		{ChatID: chatMatch, CampusID: campusID},
	}, nil)

	queries.EXPECT().ListTelegramGroupTeamProjectFilters(gomock.Any(), chatSkip).Return([]db.TelegramGroupTeamProjectFilter{
		{ChatID: chatSkip, ProjectID: projectID + 1},
	}, nil)

	sender.EXPECT().SendMessage(chatMatch, gomock.Any(), gomock.Any()).DoAndReturn(
		func(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
			require.Equal(t, chatMatch, chatID)
			require.Contains(t, text, "Новая заявка на поиск команды")
			require.Contains(t, text, "Need 2 backend devs")
			require.NotNil(t, opts)
			require.Equal(t, int64(78), opts.MessageThreadId)
			return &gotgbot.Message{MessageId: 556}, nil
		},
	)

	queries.EXPECT().UpsertTelegramGroupTeamMessage(gomock.Any(), db.UpsertTelegramGroupTeamMessageParams{
		TeamSearchRequestID: requestID,
		ChatID:              chatMatch,
		MessageID:           556,
		MessageThreadID:     78,
		LastRenderedStatus:  db.EnumReviewStatusSEARCHING,
	}).Return(nil)

	err := runTeamGroupBroadcast(context.Background(), queries, sender, log, requestID, db.EnumReviewStatusSEARCHING, true)
	require.NoError(t, err)
}

func TestRunTeamGroupBroadcast_SyncWithdrawn_DeleteFallbackToStub(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	requestID := int64(7771)
	chatID := int64(-1003003)
	campusID := mustUUID(t, "55555555-5555-5555-5555-555555555555")

	queries.EXPECT().GetTeamSearchRequestByID(gomock.Any(), requestID).Return(db.GetTeamSearchRequestByIDRow{
		ID:                  requestID,
		ProjectID:           52,
		ProjectName:         "Race00",
		ProjectType:         "GROUP",
		RequesterS21Login:   "teammate",
		RequesterCampusID:   campusID,
		RequesterCampusName: "SPB",
		RequesterLevel:      "4",
	}, nil)
	queries.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "teammate").Return(db.RegisteredUser{
		S21Login: "teammate",
	}, nil)
	queries.EXPECT().ListTelegramGroupTeamMessagesByRequest(gomock.Any(), requestID).Return([]db.TelegramGroupTeamMessage{
		{TeamSearchRequestID: requestID, ChatID: chatID, MessageID: 91},
	}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		TeamWithdrawnBehavior: "delete",
	}, nil)
	sender.EXPECT().DeleteMessage(chatID, int64(91)).Return(false, errors.New("cannot delete"))
	sender.EXPECT().EditMessageText(gomock.Any(), gomock.Any()).DoAndReturn(
		func(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
			require.Contains(t, text, "отозвана")
			require.NotNil(t, opts)
			require.Equal(t, chatID, opts.ChatId)
			require.Equal(t, int64(91), opts.MessageId)
			return nil, false, nil
		},
	)
	queries.EXPECT().UpdateTelegramGroupTeamMessageStatus(gomock.Any(), db.UpdateTelegramGroupTeamMessageStatusParams{
		TeamSearchRequestID: requestID,
		ChatID:              chatID,
		LastRenderedStatus:  db.EnumReviewStatusWITHDRAWN,
	}).Return(nil)

	err := runTeamGroupBroadcast(context.Background(), queries, sender, log, requestID, db.EnumReviewStatusWITHDRAWN, false)
	require.NoError(t, err)
}

func TestRunTeamGroupBroadcast_SyncWithdrawn_DeleteSuccessRemovesLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	requestID := int64(7772)
	chatID := int64(-1003004)
	campusID := mustUUID(t, "66666666-6666-6666-6666-666666666666")

	queries.EXPECT().GetTeamSearchRequestByID(gomock.Any(), requestID).Return(db.GetTeamSearchRequestByIDRow{
		ID:                  requestID,
		ProjectID:           53,
		ProjectName:         "Graph",
		ProjectType:         "GROUP",
		RequesterS21Login:   "teammate2",
		RequesterCampusID:   campusID,
		RequesterCampusName: "KZN",
		RequesterLevel:      "2",
	}, nil)
	queries.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "teammate2").Return(db.RegisteredUser{
		S21Login: "teammate2",
	}, nil)
	queries.EXPECT().ListTelegramGroupTeamMessagesByRequest(gomock.Any(), requestID).Return([]db.TelegramGroupTeamMessage{
		{TeamSearchRequestID: requestID, ChatID: chatID, MessageID: 322},
	}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                chatID,
		TeamWithdrawnBehavior: "delete",
	}, nil)
	sender.EXPECT().DeleteMessage(chatID, int64(322)).Return(true, nil)
	queries.EXPECT().DeleteTelegramGroupTeamMessageByRequestAndChat(gomock.Any(), db.DeleteTelegramGroupTeamMessageByRequestAndChatParams{
		TeamSearchRequestID: requestID,
		ChatID:              chatID,
	}).Return(int64(1), nil)

	err := runTeamGroupBroadcast(context.Background(), queries, sender, log, requestID, db.EnumReviewStatusWITHDRAWN, false)
	require.NoError(t, err)
}

func TestBuildTeamGroupStatusMessage_Formats(t *testing.T) {
	data := teamGroupNotificationData{
		ProjectName:        "Rush",
		ProjectType:        "GROUP",
		RequesterLogin:     "login",
		RequesterLevel:     "5",
		RequesterCampus:    "MSK",
		PlannedStartText:   "soon",
		RequestNoteText:    "Need frontend",
		RocketchatID:       "rc-id",
		AlternativeContact: "@alt",
	}

	text, buttons := buildTeamGroupStatusMessage(db.EnumReviewStatusSEARCHING, data)
	assert.Contains(t, text, "поиск команды")
	assert.Contains(t, text, "Need frontend")
	require.Len(t, buttons, 1)
	assert.Contains(t, buttons[0][0].URL, "rocketchat")

	text, buttons = buildTeamGroupStatusMessage(db.EnumReviewStatusNEGOTIATING, data)
	assert.Contains(t, strings.ToLower(text), "паузе")
	assert.Nil(t, buttons)

	text, _ = buildTeamGroupStatusMessage(db.EnumReviewStatusCLOSED, data)
	assert.Contains(t, text, "закрыт")

	text, _ = buildTeamGroupStatusMessage(db.EnumReviewStatusWITHDRAWN, data)
	assert.Contains(t, text, "отозвана")
}
