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
		text := s.buildWelcomeMessage(ctx, member)
		opts := &gotgbot.SendMessageOpts{ParseMode: "HTML"}
		if group.WelcomeThreadID > 0 {
			opts.MessageThreadId = group.WelcomeThreadID
		}
		sent, err := s.getSender(b).SendMessage(group.ChatID, text, opts)
		if err != nil {
			if s.log != nil {
				s.log.Warn("failed to send welcome message", "error_type", safeTelegramErrorType(err))
			}
			continue
		}
		if sent == nil || sent.MessageId == 0 {
			if s.log != nil {
				s.log.Warn("welcome message returned without a message reference")
			}
			continue
		}
		if err := s.queries.CreateTelegramGroupWelcomeMessage(ctx, db.CreateTelegramGroupWelcomeMessageParams{
			ChatID:    group.ChatID,
			MessageID: sent.MessageId,
		}); err != nil {
			if s.log != nil {
				s.log.Error("failed to schedule welcome deletion", "error_type", safeTelegramErrorType(err))
			}
			// Do not knowingly leave an unscheduled personal-data projection behind.
			_, _ = s.getSender(b).DeleteMessage(group.ChatID, sent.MessageId)
		}
	}
}

func (s *telegramService) buildWelcomeMessage(ctx context.Context, user gotgbot.User) string {
	parts := []string{formatWelcomeUserLink(user)}
	if username := strings.TrimSpace(user.Username); username != "" {
		parts = append(parts, "@"+html.EscapeString(username))
	}
	if login := s.resolveWelcomeSchoolLogin(ctx, user.Id); login != "" {
		parts = append(parts, "("+html.EscapeString(login)+")")
	}
	return strings.Join(parts, " ") + " присоединился к чату."
}

func formatWelcomeUserLink(user gotgbot.User) string {
	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = "Новый участник"
	}
	return fmt.Sprintf(`<a href="tg://openmessage?user_id=%d">%s</a>`, user.Id, html.EscapeString(name))
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
