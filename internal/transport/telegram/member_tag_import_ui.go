package telegram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service/membertagimport"
)

const (
	memberTagImportFlow     = "settings.yaml"
	memberTagImportState    = "MEMBER_TAG_IMPORT"
	memberTagImportMaxBytes = int64(2 * 1024 * 1024)
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

	document := tgctx.Message.Document
	chatID := tgctx.EffectiveChat.Id
	messageID := int64(tgctx.Message.MessageId)
	defer s.deleteMemberTagImportMessage(b, chatID, messageID)

	if !s.isConfiguredBotOwner(ctx, tgctx.EffectiveUser.Id) {
		s.sendMemberTagImportNotice(b, chatID, "Недостаточно прав для импорта.")
		return true, nil
	}
	if b == nil || s.pool == nil {
		s.sendMemberTagImportNotice(b, chatID, "Импорт сейчас недоступен. Попробуйте позже.")
		return true, nil
	}
	if !strings.EqualFold(filepath.Ext(strings.TrimSpace(document.FileName)), ".json") {
		s.sendMemberTagImportNotice(b, chatID, "Нужен файл с расширением .json.")
		return true, nil
	}
	if document.FileSize > memberTagImportMaxBytes {
		s.sendMemberTagImportNotice(b, chatID, "Файл больше допустимых 2 МиБ.")
		return true, nil
	}

	data, err := downloadTelegramDocument(ctx, b, document.FileId, memberTagImportMaxBytes)
	if err != nil {
		if s.log != nil {
			s.log.Warn("legacy member-tag upload download failed", "error_type", safeTelegramErrorType(err))
		}
		s.sendMemberTagImportNotice(b, chatID, "Не удалось прочитать файл. Проверьте формат и попробуйте снова.")
		return true, nil
	}
	report, err := membertagimport.Parse(bytes.NewReader(data))
	data = nil
	if err != nil {
		if s.log != nil {
			s.log.Warn("legacy member-tag upload parse failed", "error_type", safeTelegramErrorType(err))
		}
		s.sendMemberTagImportNotice(b, chatID, "Файл не соответствует ожидаемому JSON-формату.")
		return true, nil
	}

	queued, err := membertagimport.Apply(ctx, s.pool, report, time.Now().UTC())
	if err != nil {
		if s.log != nil {
			s.log.Error("legacy member-tag upload apply failed", "error_type", safeTelegramErrorType(err))
		}
		s.sendMemberTagImportNotice(b, chatID, "Не удалось применить импорт. Данные не изменены.")
		return true, nil
	}
	s.sendMemberTagImportNotice(b, chatID, formatMemberTagImportReport(report, queued))
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

func downloadTelegramDocument(ctx context.Context, b *gotgbot.Bot, fileID string, maxBytes int64) ([]byte, error) {
	if b == nil || strings.TrimSpace(fileID) == "" || maxBytes <= 0 {
		return nil, fmt.Errorf("invalid telegram document reference")
	}
	file, err := b.GetFileWithContext(ctx, fileID, nil)
	if err != nil {
		return nil, fmt.Errorf("get telegram document: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL(b, nil), nil)
	if err != nil {
		return nil, fmt.Errorf("create telegram document request: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download telegram document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("telegram document response status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read telegram document: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("telegram document exceeds size limit")
	}
	return data, nil
}

func formatMemberTagImportReport(report membertagimport.Report, queued int64) string {
	return fmt.Sprintf("Импорт завершён.\nСтрок: %d\nПринято: %d\nПропущено некорректных: %d\nСтрок с конфликтами: %d\nКонфликтующих ID: %d\nБез статуса: %d\nПоставлено в очередь: %d",
		report.TotalRows, report.AcceptedRows, report.SkippedInvalidRows, report.SkippedConflictRows,
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
