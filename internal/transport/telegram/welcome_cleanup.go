package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

const (
	welcomeCleanupInterval = time.Hour
	welcomeCleanupBatch    = int32(100)
)

type welcomeDeletionStore interface {
	ListDueTelegramGroupWelcomeMessages(context.Context, int32) ([]db.ListDueTelegramGroupWelcomeMessagesRow, error)
	DeleteTelegramGroupWelcomeMessage(context.Context, db.DeleteTelegramGroupWelcomeMessageParams) error
	RetryTelegramGroupWelcomeMessageDeletion(context.Context, db.RetryTelegramGroupWelcomeMessageDeletionParams) error
}

func (s *telegramService) startWelcomeCleanup(ctx context.Context, b *gotgbot.Bot) {
	if s == nil || s.queries == nil || b == nil {
		return
	}
	s.welcomeCleanupOnce.Do(func() {
		go func() {
			s.runWelcomeCleanupBatch(ctx, s.queries, s.getSender(b))
			ticker := time.NewTicker(welcomeCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.runWelcomeCleanupBatch(ctx, s.queries, s.getSender(b))
				}
			}
		}()
	})
}

func (s *telegramService) runWelcomeCleanupBatch(ctx context.Context, store welcomeDeletionStore, sender Sender) {
	if store == nil || sender == nil {
		return
	}
	due, err := store.ListDueTelegramGroupWelcomeMessages(ctx, welcomeCleanupBatch)
	if err != nil {
		if s != nil && s.log != nil {
			s.log.Warn("failed to list due welcome deletions", "error_type", safeTelegramErrorType(err))
		}
		return
	}

	for _, item := range due {
		deleted, deleteErr := sender.DeleteMessage(item.ChatID, item.MessageID)
		if deleteErr == nil && deleted || isTelegramMessageAlreadyAbsent(deleteErr) {
			if err := store.DeleteTelegramGroupWelcomeMessage(ctx, db.DeleteTelegramGroupWelcomeMessageParams{
				ChatID: item.ChatID, MessageID: item.MessageID,
			}); err != nil && s != nil && s.log != nil {
				s.log.Warn("failed to acknowledge welcome deletion", "error_type", safeTelegramErrorType(err))
			}
			continue
		}

		if err := store.RetryTelegramGroupWelcomeMessageDeletion(ctx, db.RetryTelegramGroupWelcomeMessageDeletionParams{
			ChatID: item.ChatID, MessageID: item.MessageID,
		}); err != nil {
			if s != nil && s.log != nil {
				s.log.Warn("failed to schedule welcome deletion retry", "error_type", safeTelegramErrorType(err))
			}
			continue
		}
		if item.AttemptCount == 0 || (item.AttemptCount+1)%4 == 0 {
			_, _ = sender.SendMessage(item.OwnerTelegramUserID,
				"Не удалось удалить просроченное автоприветствие в одной из ваших групп. Бот повторит попытку позже.", nil)
		}
		if s != nil && s.log != nil {
			s.log.Warn("welcome deletion will be retried", "error_type", safeTelegramErrorType(deleteErr))
		}
	}
}

func isTelegramMessageAlreadyAbsent(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "message to delete not found") ||
		strings.Contains(message, "message not found")
}

func safeTelegramErrorType(err error) string {
	if err == nil {
		return "none"
	}
	return fmt.Sprintf("%T", err)
}
