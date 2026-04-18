package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

type fakeModerationRestrictCall struct {
	userID      int64
	untilDate   int64
	permissions map[string]any
}

type fakeModerationBanCall struct {
	userID int64
}

type fakeModerationUnbanCall struct {
	userID       int64
	onlyIfBanned bool
}

type fakeModerationBotClient struct {
	members       map[int64]rawChatMember
	usernames     map[int64]string
	restrictCalls []fakeModerationRestrictCall
	banCalls      []fakeModerationBanCall
	unbanCalls    []fakeModerationUnbanCall
}

func (f *fakeModerationBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	switch method {
	case "getChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, assert.AnError
		}

		member := f.members[userID]
		if member.Status == "" {
			member.Status = gotgbot.ChatMemberStatusMember
		}
		if member.User.ID == 0 {
			member.User.ID = userID
		}
		username := strings.TrimSpace(f.usernames[userID])

		resp := map[string]any{
			"status":               member.Status,
			"is_member":            member.IsMember,
			"can_restrict_members": member.CanRestrict,
			"user": map[string]any{
				"id":         userID,
				"is_bot":     member.User.IsBot,
				"first_name": "Test",
				"username":   username,
			},
		}
		return json.Marshal(resp)
	case "restrictChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, assert.AnError
		}
		untilDate, _ := toInt64(params["until_date"])
		permissions, _ := params["permissions"].(map[string]any)
		f.restrictCalls = append(f.restrictCalls, fakeModerationRestrictCall{
			userID:      userID,
			untilDate:   untilDate,
			permissions: permissions,
		})
		return json.Marshal(true)
	case "banChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, assert.AnError
		}
		f.banCalls = append(f.banCalls, fakeModerationBanCall{userID: userID})
		return json.Marshal(true)
	case "unbanChatMember":
		userID, ok := toInt64(params["user_id"])
		if !ok {
			return nil, assert.AnError
		}
		onlyIfBanned, _ := params["only_if_banned"].(bool)
		f.unbanCalls = append(f.unbanCalls, fakeModerationUnbanCall{
			userID:       userID,
			onlyIfBanned: onlyIfBanned,
		})
		return json.Marshal(true)
	default:
		return nil, assert.AnError
	}
}

func (f *fakeModerationBotClient) GetAPIURL(_ *gotgbot.RequestOpts) string {
	return gotgbot.DefaultAPIURL
}

func (f *fakeModerationBotClient) FileURL(_, _ string, _ *gotgbot.RequestOpts) string {
	return ""
}

type recordingSender struct {
	texts []string
}

func (r *recordingSender) SendMessage(_ int64, text string, _ *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
	r.texts = append(r.texts, text)
	return &gotgbot.Message{}, nil
}

func (r *recordingSender) SendPhoto(_ int64, _ gotgbot.InputFileOrString, _ *gotgbot.SendPhotoOpts) (*gotgbot.Message, error) {
	return &gotgbot.Message{}, nil
}

func (r *recordingSender) EditMessageText(_ string, _ *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
	return &gotgbot.Message{}, true, nil
}

func (r *recordingSender) DeleteMessage(_ int64, _ int64) (bool, error) {
	return true, nil
}

func (r *recordingSender) AnswerCallbackQuery(_ string, _ *gotgbot.AnswerCallbackQueryOpts) (bool, error) {
	return true, nil
}

func TestHandleMuteCommand_ReplyDuration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID  = int64(-100111)
		ownerID = int64(5001)
		replyID = int64(1201)
		botID   = int64(9000)
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &fakeModerationBotClient{
		members: map[int64]rawChatMember{
			botID: {
				Status:      gotgbot.ChatMemberStatusAdministrator,
				CanRestrict: true,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: botID, IsBot: true},
			},
		},
	}

	s := &telegramService{log: log, sender: sender, queries: queries}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	ctx := makeGroupCommandContext(bot, chatID, ownerID, "/mute 10m", replyID, "reply_user")
	before := time.Now().UTC().Unix()
	err := s.handleMuteCommand(bot, ctx)
	require.NoError(t, err)

	require.Len(t, client.restrictCalls, 1)
	assert.Equal(t, replyID, client.restrictCalls[0].userID)
	assert.True(t, client.restrictCalls[0].untilDate > before)
	require.NotEmpty(t, sender.texts)
	assert.Contains(t, sender.texts[len(sender.texts)-1], "пускать пузыри под водой")
}

func TestHandleMuteCommand_ExplicitTargetOverridesReply(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID     = int64(-100112)
		ownerID    = int64(5002)
		replyID    = int64(1202)
		resolvedID = int64(2202)
		botID      = int64(9000)
		s21Login   = "student_login"
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &fakeModerationBotClient{
		members: map[int64]rawChatMember{
			botID: {
				Status:      gotgbot.ChatMemberStatusAdministrator,
				CanRestrict: true,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: botID, IsBot: true},
			},
		},
	}

	s := &telegramService{log: log, sender: sender, queries: queries}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	queries.EXPECT().GetUserAccountByS21Login(gomock.Any(), s21Login).Return(db.UserAccount{
		S21Login:   s21Login,
		ExternalID: strconv.FormatInt(resolvedID, 10),
	}, nil)

	ctx := makeGroupCommandContext(bot, chatID, ownerID, "/mute "+s21Login+" 10m", replyID, "reply_user")
	err := s.handleMuteCommand(bot, ctx)
	require.NoError(t, err)

	require.Len(t, client.restrictCalls, 1)
	assert.Equal(t, resolvedID, client.restrictCalls[0].userID)
	assert.NotEqual(t, replyID, client.restrictCalls[0].userID)
}

func TestHandleBanCommand_ByUsernameFallbackToKnownMembers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID   = int64(-100113)
		ownerID  = int64(5003)
		targetID = int64(2303)
		botID    = int64(9000)
		username = "TargetUser"
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &fakeModerationBotClient{
		members: map[int64]rawChatMember{
			botID: {
				Status:      gotgbot.ChatMemberStatusAdministrator,
				CanRestrict: true,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: botID, IsBot: true},
			},
			targetID: {
				Status: gotgbot.ChatMemberStatusMember,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: targetID, IsBot: false},
			},
		},
		usernames: map[int64]string{
			targetID: username,
		},
	}

	s := &telegramService{log: log, sender: sender, queries: queries}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	queries.EXPECT().ListTelegramGroupKnownMembers(gomock.Any(), chatID).Return([]db.TelegramGroupMember{
		{ChatID: chatID, TelegramUserID: targetID, IsMember: true},
	}, nil)
	queries.EXPECT().MarkTelegramGroupMemberLeft(gomock.Any(), gomock.Any()).Return(nil)
	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil)

	ctx := makeGroupCommandContext(bot, chatID, ownerID, "/ban @"+username, 0, "")
	err := s.handleBanCommand(bot, ctx)
	require.NoError(t, err)

	require.Len(t, client.banCalls, 1)
	assert.Equal(t, targetID, client.banCalls[0].userID)
}

func TestHandleMuteCommand_RejectsZeroDuration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID  = int64(-100114)
		ownerID = int64(5004)
		replyID = int64(1204)
		botID   = int64(9000)
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &fakeModerationBotClient{
		members: map[int64]rawChatMember{
			botID: {
				Status:      gotgbot.ChatMemberStatusAdministrator,
				CanRestrict: true,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: botID, IsBot: true},
			},
		},
	}

	s := &telegramService{log: log, sender: sender, queries: queries}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	ctx := makeGroupCommandContext(bot, chatID, ownerID, "/mute 0m", replyID, "reply_user")
	err := s.handleMuteCommand(bot, ctx)
	require.NoError(t, err)

	require.Empty(t, client.restrictCalls)
	require.NotEmpty(t, sender.texts)
	assert.Contains(t, sender.texts[len(sender.texts)-1], "больше нуля")
}

func TestHandleKickCommand_BanThenUnban(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID   = int64(-100115)
		ownerID  = int64(5005)
		targetID = int64(2305)
		botID    = int64(9000)
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &fakeModerationBotClient{
		members: map[int64]rawChatMember{
			botID: {
				Status:      gotgbot.ChatMemberStatusAdministrator,
				CanRestrict: true,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: botID, IsBot: true},
			},
		},
	}

	s := &telegramService{log: log, sender: sender, queries: queries}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)
	queries.EXPECT().MarkTelegramGroupMemberLeft(gomock.Any(), gomock.Any()).Return(nil)
	queries.EXPECT().UpsertTelegramGroupMember(gomock.Any(), gomock.Any()).Return(db.TelegramGroupMember{}, nil)

	ctx := makeGroupCommandContext(bot, chatID, ownerID, "/kick "+strconv.FormatInt(targetID, 10), 0, "")
	err := s.handleKickCommand(bot, ctx)
	require.NoError(t, err)

	require.Len(t, client.banCalls, 1)
	require.Len(t, client.unbanCalls, 1)
	assert.Equal(t, targetID, client.banCalls[0].userID)
	assert.Equal(t, targetID, client.unbanCalls[0].userID)
	assert.True(t, client.unbanCalls[0].onlyIfBanned)
}

func TestHandleUnmuteCommand_ByReply(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const (
		chatID   = int64(-100116)
		ownerID  = int64(5006)
		targetID = int64(2306)
		botID    = int64(9000)
	)

	queries := dbmock.NewMockQuerier(ctrl)
	sender := &recordingSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &fakeModerationBotClient{
		members: map[int64]rawChatMember{
			botID: {
				Status:      gotgbot.ChatMemberStatusAdministrator,
				CanRestrict: true,
				User: struct {
					ID    int64 `json:"id"`
					IsBot bool  `json:"is_bot"`
				}{ID: botID, IsBot: true},
			},
		},
	}

	s := &telegramService{log: log, sender: sender, queries: queries}
	bot := &gotgbot.Bot{Token: "test-token", User: gotgbot.User{Id: botID, IsBot: true}, BotClient: client}

	queries.EXPECT().GetTelegramGroupByChatID(gomock.Any(), chatID).Return(db.TelegramGroup{
		ChatID:              chatID,
		OwnerTelegramUserID: ownerID,
		IsInitialized:       true,
		IsActive:            true,
	}, nil)

	ctx := makeGroupCommandContext(bot, chatID, ownerID, "/unmute", targetID, "target_reply")
	err := s.handleUnmuteCommand(bot, ctx)
	require.NoError(t, err)

	require.Len(t, client.restrictCalls, 1)
	assert.Equal(t, targetID, client.restrictCalls[0].userID)
	assert.Equal(t, int64(0), client.restrictCalls[0].untilDate)
	assert.Equal(t, true, client.restrictCalls[0].permissions["can_send_messages"])
}

func makeGroupCommandContext(bot *gotgbot.Bot, chatID, fromUserID int64, text string, replyFromID int64, replyUsername string) *ext.Context {
	msg := &gotgbot.Message{
		Chat: gotgbot.Chat{
			Id:    chatID,
			Type:  "supergroup",
			Title: "Moderation test group",
		},
		From: &gotgbot.User{
			Id:       fromUserID,
			Username: "group_owner",
		},
		Text: text,
	}
	if replyFromID > 0 {
		msg.ReplyToMessage = &gotgbot.Message{
			From: &gotgbot.User{
				Id:       replyFromID,
				Username: replyUsername,
			},
		}
	}
	update := &gotgbot.Update{Message: msg}
	return ext.NewContext(bot, update, nil)
}
