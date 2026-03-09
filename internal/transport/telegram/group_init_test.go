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
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

type fakeBotClient struct {
	statuses map[int64]string
}

func (f *fakeBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	if method != "getChatMember" {
		return nil, fmt.Errorf("unexpected method: %s", method)
	}

	userID, ok := toInt64(params["user_id"])
	if !ok {
		return nil, fmt.Errorf("invalid user_id param type: %T", params["user_id"])
	}

	status := f.statuses[userID]
	if status == "" {
		status = gotgbot.ChatMemberStatusMember
	}

	resp := map[string]any{
		"status": status,
		"user": map[string]any{
			"id":         userID,
			"is_bot":     false,
			"first_name": "Test",
		},
	}

	return json.Marshal(resp)
}

func (f *fakeBotClient) GetAPIURL(_ *gotgbot.RequestOpts) string {
	return gotgbot.DefaultAPIURL
}

func (f *fakeBotClient) FileURL(_, _ string, _ *gotgbot.RequestOpts) string {
	return ""
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func TestHandleGroupInit_UnregisteredOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := mock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &telegramService{log: log, sender: sender, queries: queries}

	bot := &gotgbot.Bot{
		Token: "test-token",
		User:  gotgbot.User{Id: 9000, IsBot: true, Username: "testgroupbot"},
		BotClient: &fakeBotClient{statuses: map[int64]string{
			1001: gotgbot.ChatMemberStatusOwner,
			9000: gotgbot.ChatMemberStatusAdministrator,
		}},
	}

	chatID := int64(-100500)
	update := &gotgbot.Update{Message: &gotgbot.Message{
		Chat: gotgbot.Chat{Id: chatID, Type: "supergroup", Title: "Group A"},
		From: &gotgbot.User{Id: 1001, Username: "owner_a"},
		Text: "/init",
	}}
	ctx := ext.NewContext(bot, update, nil)

	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "1001",
	}).Return(db.UserAccount{}, pgx.ErrNoRows)

	var msgText string
	sender.EXPECT().SendMessage(chatID, gomock.Any(), gomock.Nil()).DoAndReturn(func(_ int64, text string, _ *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
		msgText = text
		return nil, nil
	})

	err := s.handleGroupInit(bot, ctx)
	require.NoError(t, err)

	assert.Contains(t, msgText, "должен быть зарегистрирован")
	assert.Contains(t, msgText, "/start")
	assert.Contains(t, msgText, "https://t.me/testgroupbot?start=register_group_owner")
}

func TestHandleGroupInit_SuccessTransfersOwnership(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	sender := mock.NewMockSender(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &telegramService{log: log, sender: sender, queries: queries}

	bot := &gotgbot.Bot{
		Token: "test-token",
		User:  gotgbot.User{Id: 9000, IsBot: true, Username: "testgroupbot"},
		BotClient: &fakeBotClient{statuses: map[int64]string{
			2002: gotgbot.ChatMemberStatusOwner,
			9000: gotgbot.ChatMemberStatusAdministrator,
		}},
	}

	chatID := int64(-100777)
	update := &gotgbot.Update{Message: &gotgbot.Message{
		Chat: gotgbot.Chat{Id: chatID, Type: "supergroup", Title: "Group B"},
		From: &gotgbot.User{Id: 2002, Username: "owner_b"},
		Text: "/init",
	}}
	ctx := ext.NewContext(bot, update, nil)

	queries.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "2002",
	}).Return(db.UserAccount{ID: 1}, nil)

	queries.EXPECT().UpsertTelegramGroup(gomock.Any(), db.UpsertTelegramGroupParams{
		ChatID:                chatID,
		ChatTitle:             "Group B",
		OwnerTelegramUserID:   int64(2002),
		OwnerTelegramUsername: "owner_b",
		IsInitialized:         true,
		IsActive:              true,
	}).Return(db.TelegramGroup{}, nil)

	var msgText string
	sender.EXPECT().SendMessage(chatID, gomock.Any(), gomock.Nil()).DoAndReturn(func(_ int64, text string, _ *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
		msgText = text
		return nil, nil
	})

	err := s.handleGroupInit(bot, ctx)
	require.NoError(t, err)
	assert.Contains(t, msgText, "Права управления закреплены за текущим владельцем")
}

func TestHandleMyChatMember_DeactivateOnRemoved(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := dbmock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &telegramService{log: log, queries: queries}

	chatID := int64(-100999)
	update := &gotgbot.Update{
		MyChatMember: &gotgbot.ChatMemberUpdated{
			Chat:          gotgbot.Chat{Id: chatID, Type: "supergroup", Title: "Group C"},
			OldChatMember: gotgbot.ChatMemberAdministrator{User: gotgbot.User{Id: 9000}},
			NewChatMember: gotgbot.ChatMemberLeft{User: gotgbot.User{Id: 9000}},
		},
	}
	ctx := ext.NewContext(&gotgbot.Bot{}, update, nil)

	queries.EXPECT().DeactivateTelegramGroup(gomock.Any(), chatID).Return(nil)

	err := s.handleMyChatMember(nil, ctx)
	require.NoError(t, err)
}
