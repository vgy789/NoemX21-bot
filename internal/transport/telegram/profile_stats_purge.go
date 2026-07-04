package telegram

import (
	"context"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

const profileStatsPurgeConfirmation = "CONFIRM"

func (s *telegramService) handleProfileStatsPurge(b *gotgbot.Bot, tgctx *ext.Context) error {
	if s == nil || tgctx == nil || tgctx.Message == nil || tgctx.EffectiveUser == nil || tgctx.EffectiveChat == nil || !isPrivateChat(tgctx.EffectiveChat) {
		return nil
	}
	ctx := context.Background()
	chatID := tgctx.EffectiveChat.Id
	if !s.isConfiguredBotOwner(ctx, tgctx.EffectiveUser.Id) {
		s.sendMemberTagImportNotice(b, chatID, "Недостаточно прав для очистки статистики.")
		return nil
	}
	parts := strings.Fields(tgctx.Message.Text)
	if len(parts) != 2 || parts[1] != profileStatsPurgeConfirmation {
		s.sendMemberTagImportNotice(b, chatID, "Для полного удаления статистики отправьте: /purge_profile_stats CONFIRM")
		return nil
	}
	deleted, err := s.queries.PurgeParticipantStatsCache(ctx)
	if err != nil {
		if s.log != nil {
			s.log.Error("profile stats purge failed", "error_type", safeTelegramErrorType(err))
		}
		s.sendMemberTagImportNotice(b, chatID, "Не удалось удалить статистику.")
		return nil
	}
	s.sendMemberTagImportNotice(b, chatID, "Статистика School21 удалена. Удалите также локальные snapshot-файлы. Удалено строк: "+formatInt64(deleted))
	return nil
}

func formatInt64(value int64) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}
