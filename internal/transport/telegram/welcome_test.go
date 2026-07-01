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
	"go.uber.org/mock/gomock"
)

func TestHandleGroupWelcome_SendsMessageAndDeletesServiceMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID   = int64(-100901)
		userID   = int64(42001)
		threadID = int64(777)
		botID    = int64(9000)
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, sender: sender, queries: queries}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:                       chatID,
		IsInitialized:                true,
		IsActive:                     true,
		WelcomeEnabled:               true,
		WelcomeThreadID:              threadID,
		WelcomeDeleteServiceMessages: true,
	}, nil)
	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "42001",
	}).Return(db.UserAccount{S21Login: "clemenhi"}, nil)
	queries.EXPECT().CreateTelegramGroupWelcomeMessage(gomock.Any(), db.CreateTelegramGroupWelcomeMessageParams{
		ChatID: chatID, MessageID: 1,
	}).Return(nil)

	client := &fakeDefenderBotClient{members: map[int64]rawChatMember{
		userID: {
			Status: gotgbot.ChatMemberStatusMember,
			Tag:    "A0",
			User: struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			}{ID: userID, IsBot: false},
		},
	}}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	s.handleGroupWelcome(context.Background(), bot, &gotgbot.Message{
		MessageId: 333,
		Chat:      gotgbot.Chat{Id: chatID, Type: "supergroup"},
		NewChatMembers: []gotgbot.User{{
			Id:        userID,
			FirstName: "Clem",
			LastName:  "Henri",
			Username:  "Akrilly",
		}},
	})

	require.Equal(t, []int64{333}, sender.deleted)
	require.Len(t, sender.texts, 1)
	assert.Contains(t, sender.texts[0], `href="tg://openmessage?user_id=42001"`)
	assert.Contains(t, sender.texts[0], "Clem Henri")
	assert.Contains(t, sender.texts[0], "@Akrilly")
	assert.NotContains(t, sender.texts[0], "(A0)")
	assert.Contains(t, sender.texts[0], "(clemenhi)")
	assert.Contains(t, sender.texts[0], "присоединился к чату.")
	require.Len(t, sender.messageOpts, 1)
	require.NotNil(t, sender.messageOpts[0])
	assert.Equal(t, "HTML", sender.messageOpts[0].ParseMode)
	assert.Equal(t, threadID, sender.messageOpts[0].MessageThreadId)
}

func TestBuildWelcomeMessage_OmitsUnknownLoginAndVisibleNumericFallback(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{}, assert.AnError)
	s := &telegramService{queries: queries}

	text := s.buildWelcomeMessage(context.Background(), gotgbot.User{Id: 42001})

	assert.Contains(t, text, ">Новый участник</a>")
	assert.NotContains(t, text, ">42001</a>")
	assert.NotContains(t, text, "(")
}

func TestHandleGroupWelcome_DisabledDoesNothing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), int64(-100902)).Return(db.TelegramGroup{
		ChatID: -100902, IsInitialized: true, IsActive: true, WelcomeEnabled: false,
	}, nil)
	sender := &recordingSender{}
	s := &telegramService{queries: queries, sender: sender}

	s.handleGroupWelcome(context.Background(), &gotgbot.Bot{}, &gotgbot.Message{
		Chat:           gotgbot.Chat{Id: -100902, Type: "supergroup"},
		NewChatMembers: []gotgbot.User{{Id: 42, FirstName: "Peer"}},
	})

	assert.Empty(t, sender.texts)
}

func TestBuildWelcomeMessage_EscapesPublicFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{S21Login: "peer<one>"}, nil)
	s := &telegramService{queries: queries}

	text := s.buildWelcomeMessage(context.Background(), gotgbot.User{
		Id: 43, FirstName: "<Peer>", Username: "peer&one",
	})

	assert.Contains(t, text, "&lt;Peer&gt;")
	assert.Contains(t, text, "@peer&amp;one")
	assert.Contains(t, text, "(peer&lt;one&gt;)")
	assert.NotContains(t, text, "<Peer>")
}
