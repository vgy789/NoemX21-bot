package telegram

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

func (s *telegramService) handleGroupWelcome(ctx context.Context, b *gotgbot.Bot, msg *gotgbot.Message) {
	if s == nil || s.queries == nil || msg == nil || len(msg.NewChatMembers) == 0 {
		return
	}
	if !isGroupChat(&msg.Chat) {
		return
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, msg.Chat.Id)
	if err != nil || !group.IsActive || !group.IsInitialized || !group.WelcomeEnabled {
		return
	}

	if group.WelcomeDeleteServiceMessages && msg.MessageId != 0 {
		if _, err := s.getSender(b).DeleteMessage(group.ChatID, int64(msg.MessageId)); err != nil && s.log != nil {
			s.log.Debug("failed to delete telegram join service message", "chat_id", group.ChatID, "message_id", msg.MessageId, "error", err)
		}
	}

	for _, member := range msg.NewChatMembers {
		if member.Id == 0 || member.IsBot {
			continue
		}
		text := s.buildWelcomeMessage(ctx, b, group.ChatID, member)
		opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
		if group.WelcomeThreadID > 0 {
			opts.MessageThreadId = group.WelcomeThreadID
		}
		if _, err := s.getSender(b).SendMessage(group.ChatID, text, opts); err != nil && s.log != nil {
			s.log.Warn("failed to send welcome message", "chat_id", group.ChatID, "user_id", member.Id, "error", err)
		}
	}
}

func (s *telegramService) buildWelcomeMessage(ctx context.Context, b *gotgbot.Bot, chatID int64, user gotgbot.User) string {
	parts := []string{formatWelcomeUserLink(user)}
	if username := strings.TrimSpace(user.Username); username != "" {
		parts = append(parts, "@"+html.EscapeString(username))
	}
	if tag := s.resolveWelcomeMemberTag(ctx, b, chatID, user.Id); tag != "" {
		parts = append(parts, "("+html.EscapeString(tag)+")")
	}
	if login := s.resolveWelcomeSchoolLogin(ctx, user.Id); login != "" {
		parts = append(parts, "("+html.EscapeString(login)+")")
	}
	return strings.Join(parts, " ") + " присоединился к чату."
}

func formatWelcomeUserLink(user gotgbot.User) string {
	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = "ID " + strconv.FormatInt(user.Id, 10)
	}
	return fmt.Sprintf(`<a href="tg://openmessage?user_id=%d">%s</a>`, user.Id, html.EscapeString(name))
}

func (s *telegramService) resolveWelcomeMemberTag(ctx context.Context, b *gotgbot.Bot, chatID, userID int64) string {
	if b == nil || userID == 0 {
		return ""
	}
	member, err := s.getRawChatMember(ctx, b, chatID, userID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(member.Tag)
}

func (s *telegramService) resolveWelcomeSchoolLogin(ctx context.Context, userID int64) string {
	if s == nil || s.queries == nil || userID == 0 {
		return ""
	}
	account, err := s.queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: strconv.FormatInt(userID, 10),
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(account.S21Login)
}
