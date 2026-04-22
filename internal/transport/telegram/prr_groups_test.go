package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	tgmock "github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

func TestRunPRRGroupBroadcast_PublishReviewRequest_WithThreadAndFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	prrID := int64(501)
	projectID := int64(9001)
	chatMatch := int64(-1001001)
	chatSkip := int64(-1001002)
	campusID := mustUUID(t, "11111111-1111-1111-1111-111111111111")

	queries.EXPECT().GetReviewRequestByID(gomock.Any(), prrID).Return(db.GetReviewRequestByIDRow{
		ID:                        prrID,
		ProjectID:                 projectID,
		ProjectName:               "CPP3",
		RequesterS21Login:         "student42",
		RequesterCampusID:         campusID,
		RequesterCampusName:       "MSK",
		AvailabilityText:          "today 20:00",
		RequesterLevel:            "8",
		RequesterTelegramUsername: "studenttg",
	}, nil)
	queries.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "student42").Return(db.RegisteredUser{
		S21Login:     "student42",
		RocketchatID: "rc-student-42",
	}, nil)

	queries.EXPECT().ListTelegramGroupsWithPRRNotifications(gomock.Any()).Return([]db.TelegramGroup{
		{ChatID: chatMatch, PrrNotificationsThreadID: 77},
		{ChatID: chatSkip},
	}, nil)

	queries.EXPECT().ListTelegramGroupPRRProjectFilters(gomock.Any(), chatMatch).Return([]db.TelegramGroupPrrProjectFilter{
		{ChatID: chatMatch, ProjectID: projectID},
	}, nil)
	queries.EXPECT().ListTelegramGroupPRRCampusFilters(gomock.Any(), chatMatch).Return([]db.TelegramGroupPrrCampusFilter{
		{ChatID: chatMatch, CampusID: campusID},
	}, nil)

	queries.EXPECT().ListTelegramGroupPRRProjectFilters(gomock.Any(), chatSkip).Return([]db.TelegramGroupPrrProjectFilter{
		{ChatID: chatSkip, ProjectID: projectID + 1},
	}, nil)

	sender.EXPECT().SendMessage(chatMatch, gomock.Any(), gomock.Any()).DoAndReturn(
		func(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
			require.Equal(t, chatMatch, chatID)
			require.Contains(t, text, "Новый запрос на ревью")
			require.NotNil(t, opts)
			require.Equal(t, int64(77), opts.MessageThreadId)
			return &gotgbot.Message{MessageId: 555}, nil
		},
	)

	queries.EXPECT().UpsertTelegramGroupPRRMessage(gomock.Any(), db.UpsertTelegramGroupPRRMessageParams{
		ReviewRequestID:    prrID,
		ChatID:             chatMatch,
		MessageID:          555,
		MessageThreadID:    77,
		LastRenderedStatus: db.EnumReviewStatusSEARCHING,
	}).Return(nil)

	err := runPRRGroupBroadcast(context.Background(), queries, sender, log, prrID, db.EnumReviewStatusSEARCHING, true)
	require.NoError(t, err)
}

func TestRunPRRGroupBroadcast_SyncWithdrawn_DeleteFallbackToStub(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	prrID := int64(777)
	chatID := int64(-1002001)
	campusID := mustUUID(t, "22222222-2222-2222-2222-222222222222")

	queries.EXPECT().GetReviewRequestByID(gomock.Any(), prrID).Return(db.GetReviewRequestByIDRow{
		ID:                        prrID,
		ProjectID:                 51,
		ProjectName:               "Golang",
		RequesterS21Login:         "student",
		RequesterCampusID:         campusID,
		RequesterCampusName:       "SPB",
		RequesterLevel:            "4",
		RequesterTelegramUsername: "studenttg",
	}, nil)
	queries.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "student").Return(db.RegisteredUser{
		S21Login: "student",
	}, nil)
	queries.EXPECT().ListTelegramGroupPRRMessagesByReviewRequest(gomock.Any(), prrID).Return([]db.TelegramGroupPrrMessage{
		{ReviewRequestID: prrID, ChatID: chatID, MessageID: 99},
	}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:               chatID,
		PrrWithdrawnBehavior: "delete",
	}, nil)
	sender.EXPECT().DeleteMessage(chatID, int64(99)).Return(false, errors.New("cannot delete"))
	sender.EXPECT().EditMessageText(gomock.Any(), gomock.Any()).DoAndReturn(
		func(text string, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
			require.Contains(t, text, "Запрос отозван")
			require.NotNil(t, opts)
			require.Equal(t, chatID, opts.ChatId)
			require.Equal(t, int64(99), opts.MessageId)
			return nil, false, nil
		},
	)
	queries.EXPECT().UpdateTelegramGroupPRRMessageStatus(gomock.Any(), db.UpdateTelegramGroupPRRMessageStatusParams{
		ReviewRequestID:    prrID,
		ChatID:             chatID,
		LastRenderedStatus: db.EnumReviewStatusWITHDRAWN,
	}).Return(nil)

	err := runPRRGroupBroadcast(context.Background(), queries, sender, log, prrID, db.EnumReviewStatusWITHDRAWN, false)
	require.NoError(t, err)
}

func TestRunPRRGroupBroadcast_SyncWithdrawn_DeleteSuccessRemovesLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	prrID := int64(888)
	chatID := int64(-1002002)
	campusID := mustUUID(t, "33333333-3333-3333-3333-333333333333")

	queries.EXPECT().GetReviewRequestByID(gomock.Any(), prrID).Return(db.GetReviewRequestByIDRow{
		ID:                  prrID,
		ProjectID:           7,
		ProjectName:         "SQL",
		RequesterS21Login:   "student2",
		RequesterCampusID:   campusID,
		RequesterCampusName: "KZN",
		RequesterLevel:      "2",
	}, nil)
	queries.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "student2").Return(db.RegisteredUser{
		S21Login: "student2",
	}, nil)
	queries.EXPECT().ListTelegramGroupPRRMessagesByReviewRequest(gomock.Any(), prrID).Return([]db.TelegramGroupPrrMessage{
		{ReviewRequestID: prrID, ChatID: chatID, MessageID: 321},
	}, nil)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:               chatID,
		PrrWithdrawnBehavior: "delete",
	}, nil)
	sender.EXPECT().DeleteMessage(chatID, int64(321)).Return(true, nil)
	queries.EXPECT().DeleteTelegramGroupPRRMessageByReviewRequestAndChat(gomock.Any(), db.DeleteTelegramGroupPRRMessageByReviewRequestAndChatParams{
		ReviewRequestID: prrID,
		ChatID:          chatID,
	}).Return(int64(1), nil)

	err := runPRRGroupBroadcast(context.Background(), queries, sender, log, prrID, db.EnumReviewStatusWITHDRAWN, false)
	require.NoError(t, err)
}

func TestSanitizePRRGroupTelegramUsername(t *testing.T) {
	assert.Equal(t, "good_name", sanitizePRRGroupTelegramUsername("@good_name"))
	assert.Equal(t, "", sanitizePRRGroupTelegramUsername("@bad"))
	assert.Equal(t, "", sanitizePRRGroupTelegramUsername("none"))
}

func TestBuildPRRGroupStatusMessage_Formats(t *testing.T) {
	data := prrGroupNotificationData{
		ProjectName:      "CPP2_s21_containers",
		RequesterLogin:   "login_name",
		RequesterLevel:   "5",
		RequesterCampus:  "MSK",
		AvailabilityText: "now",
		RocketchatID:     "rc-id",
	}

	text, buttons := buildPRRGroupStatusMessage(db.EnumReviewStatusSEARCHING, data)
	assert.Contains(t, text, "Новый запрос")
	require.Len(t, buttons, 1)
	assert.Contains(t, buttons[0][0].URL, "rocketchat")

	text, buttons = buildPRRGroupStatusMessage(db.EnumReviewStatusNEGOTIATING, data)
	assert.Contains(t, strings.ToLower(text), "паузе")
	assert.Contains(t, text, "`CPP2_s21_containers`")
	assert.Contains(t, text, "`login_name`")
	assert.NotContains(t, text, "\\_")
	assert.Nil(t, buttons)

	text, _ = buildPRRGroupStatusMessage(db.EnumReviewStatusCLOSED, data)
	assert.Contains(t, text, "Запрос закрыт")
	assert.Contains(t, text, "`CPP2_s21_containers`")
	assert.NotContains(t, text, "\\_")

	text, _ = buildPRRGroupStatusMessage(db.EnumReviewStatusWITHDRAWN, data)
	assert.Contains(t, text, "Запрос отозван")
	assert.Contains(t, text, "`CPP2_s21_containers`")
	assert.NotContains(t, text, "\\_")
}

func mustUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	require.NoError(t, id.Scan(raw))
	return id
}
