package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/campuslabel"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func (s *telegramService) PublishTeamSearchRequest(ctx context.Context, requestID int64) error {
	return runTeamGroupBroadcast(ctx, s.queries, s.getRuntimeSender(), s.log, requestID, db.EnumReviewStatusSEARCHING, true)
}

func (s *telegramService) SyncTeamSearchRequestStatus(ctx context.Context, requestID int64, status string) error {
	normalized := db.EnumReviewStatus(strings.ToUpper(strings.TrimSpace(status)))
	switch normalized {
	case db.EnumReviewStatusSEARCHING, db.EnumReviewStatusNEGOTIATING, db.EnumReviewStatusPAUSED, db.EnumReviewStatusCLOSED, db.EnumReviewStatusWITHDRAWN:
	default:
		return fmt.Errorf("unsupported team status: %q", status)
	}
	return runTeamGroupBroadcast(ctx, s.queries, s.getRuntimeSender(), s.log, requestID, normalized, false)
}

func runTeamGroupBroadcast(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, requestID int64, status db.EnumReviewStatus, publish bool) error {
	if queries == nil || sender == nil || requestID <= 0 {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}

	data, err := loadTeamGroupNotificationData(ctx, queries, requestID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	if publish {
		return publishTeamToGroups(ctx, queries, sender, log, data)
	}
	return syncTeamMessagesInGroups(ctx, queries, sender, log, data, status)
}

type teamGroupNotificationData struct {
	RequestID          int64
	ProjectID          int64
	RequesterCampusID  pgtype.UUID
	ProjectName        string
	ProjectType        string
	RequesterLogin     string
	RequesterLevel     string
	RequesterCampus    string
	PlannedStartText   string
	RequestNoteText    string
	TelegramUsername   string
	RocketchatID       string
	AlternativeContact string
}

func loadTeamGroupNotificationData(ctx context.Context, queries db.Querier, requestID int64) (teamGroupNotificationData, error) {
	row, err := queries.GetTeamSearchRequestByID(ctx, requestID)
	if err != nil {
		return teamGroupNotificationData{}, err
	}

	rocketchatID := ""
	alternativeContact := ""
	if reg, regErr := queries.GetRegisteredUserByS21Login(ctx, row.RequesterS21Login); regErr == nil {
		rocketchatID = strings.TrimSpace(reg.RocketchatID)
		alternativeContact = strings.TrimSpace(reg.AlternativeContact.String)
	}

	level := strings.TrimSpace(fmt.Sprintf("%v", row.RequesterLevel))
	if level == "<nil>" {
		level = ""
	}
	if level == "" {
		level = "0"
	}

	campus := campuslabel.Localize(campusNameString(row.RequesterCampusName), fsm.LangRu)
	if campus == "" {
		campus = "Unknown campus"
	}

	projectType := strings.ToUpper(strings.TrimSpace(row.ProjectType))
	if projectType == "" {
		projectType = "GROUP"
	}

	return teamGroupNotificationData{
		RequestID:          row.ID,
		ProjectID:          row.ProjectID,
		RequesterCampusID:  row.RequesterCampusID,
		ProjectName:        strings.TrimSpace(row.ProjectName),
		ProjectType:        projectType,
		RequesterLogin:     strings.TrimSpace(row.RequesterS21Login),
		RequesterLevel:     level,
		RequesterCampus:    campus,
		PlannedStartText:   strings.TrimSpace(row.PlannedStartText),
		RequestNoteText:    strings.TrimSpace(row.RequestNoteText),
		TelegramUsername:   sanitizePRRGroupTelegramUsername(fmt.Sprintf("%v", row.RequesterTelegramUsername)),
		RocketchatID:       rocketchatID,
		AlternativeContact: alternativeContact,
	}, nil
}

func publishTeamToGroups(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, data teamGroupNotificationData) error {
	groups, err := queries.ListTelegramGroupsWithTeamNotifications(ctx)
	if err != nil {
		return fmt.Errorf("load team groups failed: %w", err)
	}
	if len(groups) == 0 {
		return nil
	}

	text, buttons := buildTeamGroupSearchingMessage(data)
	markup := buildMarkup(buttons)
	for _, group := range groups {
		match, matchErr := isGroupEligibleForTeam(ctx, queries, group.ChatID, data.ProjectID, data.RequesterCampusID)
		if matchErr != nil {
			log.Warn("team-groups: failed to evaluate filters", "chat_id", group.ChatID, "team_search_request_id", data.RequestID, "error", matchErr)
			continue
		}
		if !match {
			continue
		}

		opts := &gotgbot.SendMessageOpts{
			ParseMode:   "Markdown",
			ReplyMarkup: markup,
		}
		if group.TeamNotificationsThreadID > 0 {
			opts.MessageThreadId = group.TeamNotificationsThreadID
		}
		msg, sendErr := sender.SendMessage(group.ChatID, text, opts)
		if sendErr != nil {
			log.Warn("team-groups: failed to publish request", "chat_id", group.ChatID, "team_search_request_id", data.RequestID, "error", sendErr)
			continue
		}
		if msg == nil || msg.MessageId == 0 {
			continue
		}
		_ = queries.UpsertTelegramGroupTeamMessage(ctx, db.UpsertTelegramGroupTeamMessageParams{
			TeamSearchRequestID: data.RequestID,
			ChatID:              group.ChatID,
			MessageID:           int64(msg.MessageId),
			MessageThreadID:     group.TeamNotificationsThreadID,
			LastRenderedStatus:  db.EnumReviewStatusSEARCHING,
		})
	}

	return nil
}

func syncTeamMessagesInGroups(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, data teamGroupNotificationData, status db.EnumReviewStatus) error {
	rows, err := queries.ListTelegramGroupTeamMessagesByRequest(ctx, data.RequestID)
	if err != nil {
		return fmt.Errorf("load team group messages failed: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	for _, row := range rows {
		if status == db.EnumReviewStatusWITHDRAWN {
			if applyTeamWithdrawnBehavior(ctx, queries, sender, log, row, data) {
				continue
			}
		}

		text, buttons := buildTeamGroupStatusMessage(status, data)
		err = editGroupMessage(sender, row.ChatID, row.MessageID, text, buttons)
		if err != nil {
			if isTelegramMessageGone(err) {
				_, _ = queries.DeleteTelegramGroupTeamMessageByRequestAndChat(ctx, db.DeleteTelegramGroupTeamMessageByRequestAndChatParams{
					TeamSearchRequestID: data.RequestID,
					ChatID:              row.ChatID,
				})
			}
			log.Warn("team-groups: failed to sync status", "chat_id", row.ChatID, "message_id", row.MessageID, "status", status, "error", err)
			continue
		}

		_ = queries.UpdateTelegramGroupTeamMessageStatus(ctx, db.UpdateTelegramGroupTeamMessageStatusParams{
			TeamSearchRequestID: data.RequestID,
			ChatID:              row.ChatID,
			LastRenderedStatus:  status,
		})
	}

	return nil
}

func applyTeamWithdrawnBehavior(ctx context.Context, queries db.Querier, sender Sender, log *slog.Logger, row db.TelegramGroupTeamMessage, data teamGroupNotificationData) bool {
	group, err := queries.GetTelegramGroupByChatID(ctx, row.ChatID)
	if err == nil && strings.EqualFold(strings.TrimSpace(group.TeamWithdrawnBehavior), "delete") {
		if _, delErr := sender.DeleteMessage(row.ChatID, row.MessageID); delErr == nil {
			_, _ = queries.DeleteTelegramGroupTeamMessageByRequestAndChat(ctx, db.DeleteTelegramGroupTeamMessageByRequestAndChatParams{
				TeamSearchRequestID: data.RequestID,
				ChatID:              row.ChatID,
			})
			return true
		} else {
			log.Warn("team-groups: delete withdrawn message failed, fallback to stub", "chat_id", row.ChatID, "message_id", row.MessageID, "error", delErr)
		}
	}
	return false
}

func isGroupEligibleForTeam(ctx context.Context, queries db.Querier, chatID int64, projectID int64, campusID pgtype.UUID) (bool, error) {
	projectFilters, err := queries.ListTelegramGroupTeamProjectFilters(ctx, chatID)
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

	campusFilters, err := queries.ListTelegramGroupTeamCampusFilters(ctx, chatID)
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

func buildTeamGroupSearchingMessage(data teamGroupNotificationData) (string, [][]fsm.ButtonRender) {
	project := fsm.EscapeMarkdown(nonEmpty(data.ProjectName, "Unknown project"))
	nickname := fsm.EscapeMarkdown(nonEmpty(data.RequesterLogin, "unknown"))
	level := fsm.EscapeMarkdown(nonEmpty(data.RequesterLevel, "0"))
	campus := fsm.EscapeMarkdown(nonEmpty(data.RequesterCampus, "Unknown campus"))
	startText := fsm.EscapeMarkdown(nonEmpty(data.PlannedStartText, "—"))
	noteText := fsm.EscapeMarkdown(nonEmpty(data.RequestNoteText, "—"))

	lines := []string{
		"🟢 *Новая заявка на поиск команды!*",
		"",
		fmt.Sprintf("📁 Проект: #%s", project),
		fmt.Sprintf("👤 Автор: %s (lvl %s)", nickname, level),
		fmt.Sprintf("📍 Кампус: %s", campus),
		fmt.Sprintf("⏰ Старт: %s", startText),
		fmt.Sprintf("📝 Заметка: %s", noteText),
	}
	if strings.TrimSpace(data.AlternativeContact) != "" {
		lines = append(lines, fmt.Sprintf("🔗 Доп. контакт: %s", fsm.EscapeMarkdown(data.AlternativeContact)))
	}

	rows := make([][]fsm.ButtonRender, 0, 2)
	if data.TelegramUsername != "" {
		rows = append(rows, []fsm.ButtonRender{{
			Text: "💬 Написать в Telegram",
			URL:  "https://t.me/" + data.TelegramUsername,
		}})
	}

	rcURL := "https://rocketchat-student.21-school.ru"
	if strings.TrimSpace(data.RocketchatID) != "" {
		rcURL = "https://rocketchat-student.21-school.ru/direct/" + data.RocketchatID
	}
	rows = append(rows, []fsm.ButtonRender{{
		Text: "🚀 Написать в Rocket.Chat",
		URL:  rcURL,
	}})

	return strings.Join(lines, "\n"), rows
}

func buildTeamGroupStatusMessage(status db.EnumReviewStatus, data teamGroupNotificationData) (string, [][]fsm.ButtonRender) {
	project := fsm.EscapeMarkdown(nonEmpty(data.ProjectName, "Unknown project"))
	nickname := fsm.EscapeMarkdown(nonEmpty(data.RequesterLogin, "unknown"))

	switch status {
	case db.EnumReviewStatusSEARCHING:
		return buildTeamGroupSearchingMessage(data)
	case db.EnumReviewStatusNEGOTIATING:
		return fmt.Sprintf(
			"🟡 *Поиск команды на паузе*\n📁 Проект: %s\n👤 Автор: %s\n🤝 Автор уже обсуждает участие с кандидатом.",
			project,
			nickname,
		), nil
	case db.EnumReviewStatusPAUSED:
		return fmt.Sprintf(
			"⏸ *Поиск команды приостановлен*\n📁 Проект: %s\n👤 Автор: %s",
			project,
			nickname,
		), nil
	case db.EnumReviewStatusCLOSED:
		return fmt.Sprintf(
			"✅ *Поиск команды закрыт*\n📁 Проект: %s\n👤 Автор: %s",
			project,
			nickname,
		), nil
	case db.EnumReviewStatusWITHDRAWN:
		return fmt.Sprintf(
			"⚪ *Заявка на поиск команды отозвана*\n📁 Проект: %s\n👤 Автор: %s",
			project,
			nickname,
		), nil
	default:
		return buildTeamGroupSearchingMessage(data)
	}
}
