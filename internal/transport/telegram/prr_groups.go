package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

var prrGroupTelegramUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

func (s *telegramService) PublishReviewRequest(ctx context.Context, reviewRequestID int64) error {
	return runPRRGroupBroadcast(ctx, s.queries, s.getRuntimeSender(), s.log, reviewRequestID, db.EnumReviewStatusSEARCHING, true)
}

func (s *telegramService) SyncReviewRequestStatus(ctx context.Context, reviewRequestID int64, status string) error {
	normalized := db.EnumReviewStatus(strings.ToUpper(strings.TrimSpace(status)))
	switch normalized {
	case db.EnumReviewStatusSEARCHING, db.EnumReviewStatusNEGOTIATING, db.EnumReviewStatusPAUSED, db.EnumReviewStatusCLOSED, db.EnumReviewStatusWITHDRAWN:
	default:
		return fmt.Errorf("unsupported PRR status: %q", status)
	}
	return runPRRGroupBroadcast(ctx, s.queries, s.getRuntimeSender(), s.log, reviewRequestID, normalized, false)
}

func (s *telegramService) getRuntimeSender() Sender {
	if s == nil {
		return nil
	}
	if s.sender != nil {
		return s.sender
	}
	if s.bot != nil {
		return &DefaultSender{Bot: s.bot}
	}
	return nil
}

func runPRRGroupBroadcast(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, reviewRequestID int64, status db.EnumReviewStatus, publish bool) error {
	if queries == nil || sender == nil || reviewRequestID <= 0 {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}

	data, err := loadPRRGroupNotificationData(ctx, queries, reviewRequestID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	if publish {
		return publishPRRToGroups(ctx, queries, sender, log, data)
	}
	return syncPRRMessagesInGroups(ctx, queries, sender, log, data, status)
}

type prrGroupNotificationData struct {
	ReviewRequestID   int64
	ProjectID         int64
	RequesterCampusID pgtype.UUID
	ProjectName       string
	RequesterLogin    string
	RequesterLevel    string
	RequesterCampus   string
	AvailabilityText  string
	TelegramUsername  string
	RocketchatID      string
}

func loadPRRGroupNotificationData(ctx context.Context, queries db.Querier, reviewRequestID int64) (prrGroupNotificationData, error) {
	row, err := queries.GetReviewRequestByID(ctx, reviewRequestID)
	if err != nil {
		return prrGroupNotificationData{}, err
	}

	rocketchatID := ""
	if reg, regErr := queries.GetRegisteredUserByS21Login(ctx, row.RequesterS21Login); regErr == nil {
		rocketchatID = strings.TrimSpace(reg.RocketchatID)
	}

	level := strings.TrimSpace(fmt.Sprintf("%v", row.RequesterLevel))
	if level == "<nil>" {
		level = ""
	}
	if level == "" {
		level = "0"
	}

	campus := strings.TrimSpace(row.RequesterCampusName)
	if campus == "" {
		campus = "Unknown campus"
	}

	return prrGroupNotificationData{
		ReviewRequestID:   row.ID,
		ProjectID:         row.ProjectID,
		RequesterCampusID: row.RequesterCampusID,
		ProjectName:       strings.TrimSpace(row.ProjectName),
		RequesterLogin:    strings.TrimSpace(row.RequesterS21Login),
		RequesterLevel:    level,
		RequesterCampus:   campus,
		AvailabilityText:  strings.TrimSpace(row.AvailabilityText),
		TelegramUsername:  sanitizePRRGroupTelegramUsername(fmt.Sprintf("%v", row.RequesterTelegramUsername)),
		RocketchatID:      rocketchatID,
	}, nil
}

func publishPRRToGroups(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, data prrGroupNotificationData) error {
	groups, err := queries.ListTelegramGroupsWithPRRNotifications(ctx)
	if err != nil {
		return fmt.Errorf("load PRR groups failed: %w", err)
	}
	if len(groups) == 0 {
		return nil
	}

	text, buttons := buildPRRGroupSearchingMessage(data)
	markup := buildMarkup(buttons)
	for _, group := range groups {
		match, matchErr := isGroupEligibleForPRR(ctx, queries, group.ChatID, data.ProjectID, data.RequesterCampusID)
		if matchErr != nil {
			log.Warn("prr-groups: failed to evaluate filters", "chat_id", group.ChatID, "review_request_id", data.ReviewRequestID, "error", matchErr)
			continue
		}
		if !match {
			continue
		}

		opts := &gotgbot.SendMessageOpts{
			ParseMode:   "Markdown",
			ReplyMarkup: markup,
		}
		if group.PrrNotificationsThreadID > 0 {
			opts.MessageThreadId = group.PrrNotificationsThreadID
		}
		msg, sendErr := sender.SendMessage(group.ChatID, text, opts)
		if sendErr != nil {
			log.Warn("prr-groups: failed to publish request", "chat_id", group.ChatID, "review_request_id", data.ReviewRequestID, "error", sendErr)
			continue
		}
		if msg == nil || msg.MessageId == 0 {
			continue
		}
		_ = queries.UpsertTelegramGroupPRRMessage(ctx, db.UpsertTelegramGroupPRRMessageParams{
			ReviewRequestID:    data.ReviewRequestID,
			ChatID:             group.ChatID,
			MessageID:          int64(msg.MessageId),
			MessageThreadID:    group.PrrNotificationsThreadID,
			LastRenderedStatus: db.EnumReviewStatusSEARCHING,
		})
	}

	return nil
}

func syncPRRMessagesInGroups(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, data prrGroupNotificationData, status db.EnumReviewStatus) error {
	rows, err := queries.ListTelegramGroupPRRMessagesByReviewRequest(ctx, data.ReviewRequestID)
	if err != nil {
		return fmt.Errorf("load PRR group messages failed: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	for _, row := range rows {
		if status == db.EnumReviewStatusWITHDRAWN {
			if applyWithdrawnBehavior(ctx, queries, sender, log, row, data) {
				continue
			}
		}

		text, buttons := buildPRRGroupStatusMessage(status, data)
		err = editGroupMessage(sender, row.ChatID, row.MessageID, text, buttons)
		if err != nil {
			if isTelegramMessageGone(err) {
				_, _ = queries.DeleteTelegramGroupPRRMessageByReviewRequestAndChat(ctx, db.DeleteTelegramGroupPRRMessageByReviewRequestAndChatParams{
					ReviewRequestID: data.ReviewRequestID,
					ChatID:          row.ChatID,
				})
			}
			log.Warn("prr-groups: failed to sync status", "chat_id", row.ChatID, "message_id", row.MessageID, "status", status, "error", err)
			continue
		}

		_ = queries.UpdateTelegramGroupPRRMessageStatus(ctx, db.UpdateTelegramGroupPRRMessageStatusParams{
			ReviewRequestID:    data.ReviewRequestID,
			ChatID:             row.ChatID,
			LastRenderedStatus: status,
		})
	}

	return nil
}

func applyWithdrawnBehavior(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, row db.TelegramGroupPrrMessage, data prrGroupNotificationData) bool {
	group, err := queries.GetTelegramGroupByChatID(ctx, row.ChatID)
	if err == nil && strings.EqualFold(strings.TrimSpace(group.PrrWithdrawnBehavior), "delete") {
		if _, delErr := sender.DeleteMessage(row.ChatID, row.MessageID); delErr == nil {
			_, _ = queries.DeleteTelegramGroupPRRMessageByReviewRequestAndChat(ctx, db.DeleteTelegramGroupPRRMessageByReviewRequestAndChatParams{
				ReviewRequestID: data.ReviewRequestID,
				ChatID:          row.ChatID,
			})
			return true
		} else {
			log.Warn("prr-groups: delete withdrawn message failed, fallback to stub", "chat_id", row.ChatID, "message_id", row.MessageID, "error", delErr)
		}
	}
	return false
}

func isGroupEligibleForPRR(ctx context.Context, queries db.Querier, chatID int64, projectID int64, campusID pgtype.UUID) (bool, error) {
	projectFilters, err := queries.ListTelegramGroupPRRProjectFilters(ctx, chatID)
	if err != nil {
		return false, err
	}
	if len(projectFilters) > 0 {
		projectMatched := false
		for _, filter := range projectFilters {
			if filter.ProjectID == projectID {
				projectMatched = true
				break
			}
		}
		if !projectMatched {
			return false, nil
		}
	}

	campusFilters, err := queries.ListTelegramGroupPRRCampusFilters(ctx, chatID)
	if err != nil {
		return false, err
	}
	if len(campusFilters) == 0 {
		return true, nil
	}
	if !campusID.Valid {
		return false, nil
	}
	for _, filter := range campusFilters {
		if filter.CampusID == campusID {
			return true, nil
		}
	}
	return false, nil
}

func buildPRRGroupSearchingMessage(data prrGroupNotificationData) (string, [][]fsm.ButtonRender) {
	project := fsm.EscapeMarkdown(data.ProjectName)
	if strings.TrimSpace(project) == "" {
		project = "Unknown project"
	}
	nickname := fsm.EscapeMarkdown(data.RequesterLogin)
	if strings.TrimSpace(nickname) == "" {
		nickname = "unknown"
	}
	level := fsm.EscapeMarkdown(data.RequesterLevel)
	campus := fsm.EscapeMarkdown(data.RequesterCampus)
	timeText := fsm.EscapeMarkdown(nonEmpty(data.AvailabilityText, "—"))

	text := fmt.Sprintf(
		"🟢 *Новый запрос на ревью!*\n\n📁 Проект: #%s\n👤 Студент: %s (lvl %s)\n📍 Кампус: %s\n\n⏰ *Когда ждет:* \"%s\"",
		project,
		nickname,
		level,
		campus,
		timeText,
	)

	rows := make([][]fsm.ButtonRender, 0, 2)
	if data.TelegramUsername != "" {
		rows = append(rows, []fsm.ButtonRender{{
			Text: "💬 Откликнуться в ЛС",
			URL:  "https://t.me/" + data.TelegramUsername,
		}})
	}

	rcURL := "https://rocketchat-student.21-school.ru"
	if strings.TrimSpace(data.RocketchatID) != "" {
		rcURL = "https://rocketchat-student.21-school.ru/direct/" + data.RocketchatID
	}
	rows = append(rows, []fsm.ButtonRender{{
		Text: "🚀 Откликнуться в Rocket.Chat",
		URL:  rcURL,
	}})
	return text, rows
}

func buildPRRGroupStatusMessage(status db.EnumReviewStatus, data prrGroupNotificationData) (string, [][]fsm.ButtonRender) {
	project := fsm.EscapeMarkdown(data.ProjectName)
	nickname := fsm.EscapeMarkdown(data.RequesterLogin)

	switch status {
	case db.EnumReviewStatusSEARCHING:
		return buildPRRGroupSearchingMessage(data)
	case db.EnumReviewStatusNEGOTIATING, db.EnumReviewStatusPAUSED:
		return fmt.Sprintf(
			"🟡 *prr на паузе*\n📁 *Проект:* `%s`\n👤 *Студент:* `%s`\n⏳ *Студент уже договаривается с кем-то. Попробуй заглянуть позже или выбери другой проект.*",
			project,
			nickname,
		), nil
	case db.EnumReviewStatusCLOSED:
		return fmt.Sprintf(
			"✅ *Запрос закрыт*\n📁 *Проект:* `%s`\n👤 *Студент:* `%s`\n✨ _Ревьюер найден, или проект успешно сдан!_",
			project,
			nickname,
		), nil
	case db.EnumReviewStatusWITHDRAWN:
		return fmt.Sprintf(
			"⚪ *Запрос отозван*\n📁 *Проект:* `%s`\n👤 *Студент:* `%s`",
			project,
			nickname,
		), nil
	default:
		return buildPRRGroupSearchingMessage(data)
	}
}

func editGroupMessage(sender Sender, chatID int64, messageID int64, text string, buttons [][]fsm.ButtonRender) error {
	_, _, err := sender.EditMessageText(text, &gotgbot.EditMessageTextOpts{
		ChatId:      chatID,
		MessageId:   messageID,
		ParseMode:   "Markdown",
		ReplyMarkup: buildMarkup(buttons),
	})
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

func isTelegramMessageGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "message to edit not found") ||
		strings.Contains(msg, "chat not found") ||
		strings.Contains(msg, "message can't be edited")
}

func sanitizePRRGroupTelegramUsername(raw string) string {
	username := strings.TrimSpace(raw)
	username = strings.TrimPrefix(username, "@")
	if username == "" {
		return ""
	}
	if !prrGroupTelegramUsernamePattern.MatchString(username) {
		return ""
	}
	return username
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func normalizeThreadLabel(threadID int64) string {
	if threadID <= 0 {
		return "Общий чат"
	}
	return "Topic #" + strconv.FormatInt(threadID, 10)
}
