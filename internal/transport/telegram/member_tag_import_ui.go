package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service/membertagimport"
)

const (
	memberTagImportFlow  = "settings.yaml"
	memberTagImportState = "MEMBER_TAG_IMPORT"
)

func (s *telegramService) handleMemberTagImportDocument(ctx context.Context, b *gotgbot.Bot, tgctx *ext.Context) (bool, error) {
	if s == nil || s.engine == nil || tgctx == nil || tgctx.Message == nil || tgctx.Message.Document == nil ||
		tgctx.EffectiveUser == nil || tgctx.EffectiveChat == nil || !isPrivateChat(tgctx.EffectiveChat) {
		return false, nil
	}

	flow, state, err := s.engine.CurrentState(ctx, tgctx.EffectiveUser.Id)
	if err != nil || flow != memberTagImportFlow || state != memberTagImportState {
		return false, nil
	}

	chatID := tgctx.EffectiveChat.Id
	messageID := int64(tgctx.Message.MessageId)
	defer s.deleteMemberTagImportMessage(b, chatID, messageID)
	s.sendMemberTagImportNotice(b, chatID, "Импорт JSON отключён: Group Manager переведён в legacy-режим без массовых операций.")
	return true, nil
}

func (s *telegramService) isConfiguredBotOwner(ctx context.Context, telegramUserID int64) bool {
	if s == nil || s.queries == nil || s.cfg == nil || telegramUserID <= 0 {
		return false
	}
	configuredLogin := strings.TrimSpace(s.cfg.Init.SchoolLogin)
	if configuredLogin == "" {
		return false
	}
	account, err := s.queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform: db.EnumPlatformTelegram, ExternalID: strconv.FormatInt(telegramUserID, 10),
	})
	return err == nil && strings.EqualFold(strings.TrimSpace(account.S21Login), configuredLogin)
}

func formatMemberTagImportReport(report membertagimport.Report, queued int64) string {
	return fmt.Sprintf("Импорт завершён.\nСтрок: %d\nПринято: %d\nПринято profiles: %d\nНекорректных profiles: %d\nПропущено некорректных: %d\nСтрок с конфликтами: %d\nКонфликтующих ID: %d\nБез статуса: %d\nПоставлено в очередь: %d",
		report.TotalRows, report.AcceptedRows, report.AcceptedStatsRows, report.SkippedInvalidStatsRows, report.SkippedInvalidRows, report.SkippedConflictRows,
		report.SkippedConflictIDs, report.SkippedNullStatusRows, queued)
}

func (s *telegramService) sendMemberTagImportNotice(b *gotgbot.Bot, chatID int64, text string) {
	if s == nil || strings.TrimSpace(text) == "" || (b == nil && s.sender == nil) {
		return
	}
	_, _ = s.getSender(b).SendMessage(chatID, text, nil)
}

func (s *telegramService) deleteMemberTagImportMessage(b *gotgbot.Bot, chatID, messageID int64) {
	if s == nil || messageID == 0 || (b == nil && s.sender == nil) {
		return
	}
	if _, err := s.getSender(b).DeleteMessage(chatID, messageID); err != nil && s.log != nil {
		s.log.Debug("legacy member-tag upload cleanup failed", "error_type", safeTelegramErrorType(err))
	}
}
