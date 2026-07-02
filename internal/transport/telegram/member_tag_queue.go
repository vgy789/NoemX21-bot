package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

const (
	legacyMemberTagQueueInterval = time.Minute
	legacyMemberTagQueueBatch    = int32(100)
)

func (s *telegramService) startLegacyMemberTagQueue(ctx context.Context, bot *gotgbot.Bot) {
	if s == nil || s.queries == nil || bot == nil {
		return
	}
	s.memberTagQueueOnce.Do(func() {
		go func() {
			s.runLegacyMemberTagQueueBatch(ctx, bot)
			ticker := time.NewTicker(legacyMemberTagQueueInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.runLegacyMemberTagQueueBatch(ctx, bot)
				}
			}
		}()
	})
}

func (s *telegramService) runLegacyMemberTagQueueBatch(ctx context.Context, bot *gotgbot.Bot) {
	items, err := s.queries.ListDueLegacyMemberTags(ctx, legacyMemberTagQueueBatch)
	if err != nil {
		s.log.Warn("failed to list legacy member-tag queue", "error_type", safeTelegramErrorType(err))
		return
	}
	for _, item := range items {
		s.applyLegacyMemberTagQueueItem(ctx, bot, item)
	}
}

func (s *telegramService) applyLegacyMemberTagQueueItem(ctx context.Context, bot *gotgbot.Bot, item db.LegacyMemberTagQueue) {
	retry := func(err error) {
		_ = s.queries.RetryLegacyMemberTag(ctx, db.RetryLegacyMemberTagParams{
			ChatID: item.ChatID, TelegramUserID: item.TelegramUserID, LastErrorCode: safeTelegramErrorType(err),
		})
	}
	group, err := s.queries.GetTelegramGroupByChatID(ctx, item.ChatID)
	if err != nil || !group.IsActive || !group.IsInitialized {
		_ = s.queries.MarkLegacyMemberTagSkipped(ctx, db.MarkLegacyMemberTagSkippedParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		return
	}
	botMember, err := s.getRawChatMember(ctx, bot, item.ChatID, bot.Id)
	if err != nil || !canEditMemberTags(botMember) {
		if err == nil {
			err = errors.New("member_tag_permission_denied")
		}
		retry(err)
		return
	}
	member, err := s.getRawChatMember(ctx, bot, item.ChatID, item.TelegramUserID)
	if err != nil {
		retry(err)
		return
	}
	if !isRawMemberActive(member) || !isRegularMemberForTag(member) {
		_ = s.queries.MarkLegacyMemberTagSkipped(ctx, db.MarkLegacyMemberTagSkippedParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		return
	}

	profile, _, suppressed := s.resolveMemberTagProfile(ctx, item.TelegramUserID)
	if item.DesiredAction == "clear" || suppressed {
		if item.LastAppliedTag != "" && member.Tag == item.LastAppliedTag {
			if err := s.setChatMemberTag(ctx, bot, item.ChatID, item.TelegramUserID, ""); err != nil {
				retry(err)
				return
			}
		}
		_ = s.queries.MarkLegacyMemberTagSuppressed(ctx, db.MarkLegacyMemberTagSuppressedParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		return
	}
	if profile == nil {
		_ = s.queries.MarkLegacyMemberTagSkipped(ctx, db.MarkLegacyMemberTagSkippedParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		return
	}
	tag := buildMemberTag(profile, normalizeMemberTagFormat(group.MemberTagFormat))
	if tag == "" {
		_ = s.queries.MarkLegacyMemberTagSkipped(ctx, db.MarkLegacyMemberTagSkippedParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		return
	}
	if member.Tag != "" && member.Tag != item.LastAppliedTag && member.Tag != tag {
		_ = s.queries.MarkLegacyMemberTagSkipped(ctx, db.MarkLegacyMemberTagSkippedParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		return
	}
	if member.Tag != tag {
		if err := s.setChatMemberTag(ctx, bot, item.ChatID, item.TelegramUserID, tag); err != nil {
			retry(err)
			return
		}
	}
	_ = s.queries.MarkLegacyMemberTagApplied(ctx, db.MarkLegacyMemberTagAppliedParams{
		ChatID: item.ChatID, TelegramUserID: item.TelegramUserID, LastAppliedTag: tag,
	})
}
