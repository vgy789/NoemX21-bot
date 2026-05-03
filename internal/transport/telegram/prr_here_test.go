package telegram

import (
	"io"
	"log/slog"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	tgmock "github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

func TestHandlePRRHere_SavesThreadDestination(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &telegramService{
		log:     log,
		queries: queries,
		sender:  sender,
	}

	chatID := int64(-1005001)
	userID := int64(7001)
	threadID := int64(911)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	queries.EXPECT().UpdateTelegramGroupForumFlagsByChatID(gomock.Any(), db.UpdateTelegramGroupForumFlagsByChatIDParams{
		ChatID:  chatID,
		IsForum: true,
	}).Return(int64(1), nil)
	queries.EXPECT().UpdateTelegramGroupPRRNotificationDestinationByOwner(gomock.Any(), db.UpdateTelegramGroupPRRNotificationDestinationByOwnerParams{
		ChatID:                      chatID,
		OwnerTelegramUserID:         userID,
		PrrNotificationsThreadID:    threadID,
		PrrNotificationsThreadLabel: "Topic #911",
	}).Return(int64(1), nil)
	sender.EXPECT().SendMessage(chatID, gomock.Any(), gomock.Any()).Return(nil, nil)

	update := &gotgbot.Update{
		Message: &gotgbot.Message{
			MessageId:       12,
			MessageThreadId: threadID,
			From:            &gotgbot.User{Id: userID},
			Chat:            gotgbot.Chat{Id: chatID, Type: "supergroup", IsForum: true},
			Text:            "/prr_here",
		},
	}
	ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

	err := s.handlePRRHere(&gotgbot.Bot{}, ctx)
	require.NoError(t, err)
}

func TestHandlePRRHere_ResetsToGeneralChat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &telegramService{
		log:     log,
		queries: queries,
		sender:  sender,
	}

	chatID := int64(-1005002)
	userID := int64(7002)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: userID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	queries.EXPECT().UpdateTelegramGroupForumFlagsByChatID(gomock.Any(), db.UpdateTelegramGroupForumFlagsByChatIDParams{
		ChatID:  chatID,
		IsForum: false,
	}).Return(int64(1), nil)
	queries.EXPECT().UpdateTelegramGroupPRRNotificationDestinationByOwner(gomock.Any(), db.UpdateTelegramGroupPRRNotificationDestinationByOwnerParams{
		ChatID:                      chatID,
		OwnerTelegramUserID:         userID,
		PrrNotificationsThreadID:    0,
		PrrNotificationsThreadLabel: "Общий чат",
	}).Return(int64(1), nil)
	sender.EXPECT().SendMessage(chatID, gomock.Any(), gomock.Any()).Return(nil, nil)

	update := &gotgbot.Update{
		Message: &gotgbot.Message{
			MessageId:       13,
			MessageThreadId: 0,
			From:            &gotgbot.User{Id: userID},
			Chat:            gotgbot.Chat{Id: chatID, Type: "supergroup", IsForum: false},
			Text:            "/prr_here",
		},
	}
	ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

	err := s.handlePRRHere(&gotgbot.Bot{}, ctx)
	require.NoError(t, err)
}

func TestHandlePRRHere_DeniesNonOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := tgmock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &telegramService{
		log:     log,
		queries: queries,
		sender:  sender,
	}

	chatID := int64(-1005003)
	userID := int64(7003)

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: userID + 1,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	update := &gotgbot.Update{
		Message: &gotgbot.Message{
			MessageId:       14,
			MessageThreadId: 100,
			From:            &gotgbot.User{Id: userID},
			Chat:            gotgbot.Chat{Id: chatID, Type: "supergroup", IsForum: true},
			Text:            "/prr_here",
		},
	}
	ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

	err := s.handlePRRHere(&gotgbot.Bot{}, ctx)
	require.NoError(t, err)
}

func TestHandlePRRHere_NoQueries(t *testing.T) {
	s := &telegramService{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	update := &gotgbot.Update{
		Message: &gotgbot.Message{
			MessageId:       15,
			MessageThreadId: 1,
			From:            &gotgbot.User{Id: 1},
			Chat:            gotgbot.Chat{Id: -1, Type: "supergroup", IsForum: true},
			Text:            "/prr_here",
		},
	}
	ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)
	require.NoError(t, s.handlePRRHere(&gotgbot.Bot{}, ctx))
}
