package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

var moderationDurationPattern = regexp.MustCompile(`^([0-9]+)\s*([dhmsдчмс])[a-zа-я]*$`)

type moderationTarget struct {
	TelegramUserID int64
	Source         string
	Login          string
	Username       string
}

type telegramUserAccountByUsernameResolver interface {
	GetTelegramUserAccountByUsername(ctx context.Context, username string) (db.UserAccount, error)
}

type groupModerationCommandsConfigReader interface {
	GetTelegramGroupModerationCommandsEnabledByChatID(ctx context.Context, chatID int64) (bool, error)
}

func (s *telegramService) handleMuteCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	group, ok := s.requireGroupModerationAccess(b, ctx)
	if !ok {
		return nil
	}

	msg := ctx.EffectiveMessage
	args := extractCommandArgs(msg)

	target, duration, err := s.resolveMuteTargetAndDuration(context.Background(), b, group.ChatID, msg, args)
	if err != nil {
		_, _ = s.getSender(b).SendMessage(group.ChatID, err.Error(), nil)
		return nil
	}

	if !s.ensureBotCanRestrictMembers(context.Background(), b, group.ChatID) {
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Нужно право на ban users. Назначьте боту право бана участников и повторите команду.", nil)
		return nil
	}

	_, err = s.restrictChatMemberForDuration(context.Background(), b, group.ChatID, target.TelegramUserID, duration)
	if err != nil {
		s.log.Warn("failed to mute chat member", "chat_id", group.ChatID, "target_user_id", target.TelegramUserID, "error", err)
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Не удалось выдать мут. Проверьте, что бот администратор с правом ограничения участников.", nil)
		return nil
	}

	_, _ = s.getSender(b).SendMessage(group.ChatID,
		fmt.Sprintf("🌊 %s отправляется пускать пузыри под водой на %s.",
			formatModerationTargetOpenMessageLink(target),
			formatModerationDurationRussian(duration),
		),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"},
	)
	return nil
}

func (s *telegramService) handleUnmuteCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	group, ok := s.requireGroupModerationAccess(b, ctx)
	if !ok {
		return nil
	}

	target, err := s.resolveModerationTargetForSimpleCommand(context.Background(), b, group.ChatID, ctx.EffectiveMessage, extractCommandArgs(ctx.EffectiveMessage), "/unmute")
	if err != nil {
		_, _ = s.getSender(b).SendMessage(group.ChatID, err.Error(), nil)
		return nil
	}

	if !s.ensureBotCanRestrictMembers(context.Background(), b, group.ChatID) {
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Нужно право на ban users. Назначьте боту право бана участников и повторите команду.", nil)
		return nil
	}

	if err := s.unrestrictChatMember(context.Background(), b, group.ChatID, target.TelegramUserID); err != nil {
		s.log.Warn("failed to unmute chat member", "chat_id", group.ChatID, "target_user_id", target.TelegramUserID, "error", err)
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Не удалось снять мут. Проверьте права бота и корректность цели.", nil)
		return nil
	}

	_, _ = s.getSender(b).SendMessage(group.ChatID,
		fmt.Sprintf("🌊 %s вынырнул:а и снова может крякать в общем чате.", formatModerationTargetOpenMessageLink(target)),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"},
	)
	return nil
}

func (s *telegramService) handleBanCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	group, ok := s.requireGroupModerationAccess(b, ctx)
	if !ok {
		return nil
	}

	target, err := s.resolveModerationTargetForSimpleCommand(context.Background(), b, group.ChatID, ctx.EffectiveMessage, extractCommandArgs(ctx.EffectiveMessage), "/ban")
	if err != nil {
		_, _ = s.getSender(b).SendMessage(group.ChatID, err.Error(), nil)
		return nil
	}

	if !s.ensureBotCanRestrictMembers(context.Background(), b, group.ChatID) {
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Нужно право на ban users. Назначьте боту право бана участников и повторите команду.", nil)
		return nil
	}

	if err := s.banChatMember(context.Background(), b, group.ChatID, target.TelegramUserID); err != nil {
		s.log.Warn("failed to ban chat member", "chat_id", group.ChatID, "target_user_id", target.TelegramUserID, "error", err)
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Не удалось заблокировать участника. Проверьте права бота и корректность цели.", nil)
		return nil
	}
	s.markKnownGroupMemberLeft(context.Background(), group.ChatID, target.TelegramUserID, gotgbot.ChatMemberStatusBanned)

	_, _ = s.getSender(b).SendMessage(group.ChatID, fmt.Sprintf("Участник заблокирован: %s.", describeModerationTarget(target)), nil)
	return nil
}

func (s *telegramService) handleEbanCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	group, ok := s.requireGroupModerationAccess(b, ctx)
	if !ok {
		return nil
	}

	target, err := s.resolveModerationTargetForSimpleCommand(context.Background(), b, group.ChatID, ctx.EffectiveMessage, extractCommandArgs(ctx.EffectiveMessage), "/eban")
	if err != nil {
		_, _ = s.getSender(b).SendMessage(group.ChatID, err.Error(), nil)
		return nil
	}

	if !s.ensureBotCanRestrictMembers(context.Background(), b, group.ChatID) {
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Нужно право на ban users. Назначьте боту право бана участников и повторите команду.", nil)
		return nil
	}

	if err := s.unbanChatMember(context.Background(), b, group.ChatID, target.TelegramUserID, false); err != nil {
		s.log.Warn("failed to unban chat member", "chat_id", group.ChatID, "target_user_id", target.TelegramUserID, "error", err)
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Не удалось снять блокировку. Проверьте права бота и корректность цели.", nil)
		return nil
	}

	_, _ = s.getSender(b).SendMessage(group.ChatID, fmt.Sprintf("Блокировка снята: %s.", describeModerationTarget(target)), nil)
	return nil
}

func (s *telegramService) handleKickCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	group, ok := s.requireGroupModerationAccess(b, ctx)
	if !ok {
		return nil
	}

	target, err := s.resolveModerationTargetForSimpleCommand(context.Background(), b, group.ChatID, ctx.EffectiveMessage, extractCommandArgs(ctx.EffectiveMessage), "/kick")
	if err != nil {
		_, _ = s.getSender(b).SendMessage(group.ChatID, err.Error(), nil)
		return nil
	}

	if !s.ensureBotCanRestrictMembers(context.Background(), b, group.ChatID) {
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Нужно право на ban users. Назначьте боту право бана участников и повторите команду.", nil)
		return nil
	}

	if err := s.banChatMember(context.Background(), b, group.ChatID, target.TelegramUserID); err != nil {
		s.log.Warn("failed to kick chat member (ban stage)", "chat_id", group.ChatID, "target_user_id", target.TelegramUserID, "error", err)
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Не удалось исключить участника. Проверьте права бота и корректность цели.", nil)
		return nil
	}
	if err := s.unbanChatMember(context.Background(), b, group.ChatID, target.TelegramUserID, true); err != nil {
		s.log.Warn("failed to kick chat member (unban stage)", "chat_id", group.ChatID, "target_user_id", target.TelegramUserID, "error", err)
		_, _ = s.getSender(b).SendMessage(group.ChatID, "Участник заблокирован, но не удалось автоматически снять блокировку для kick. Выполните /eban вручную.", nil)
		return nil
	}
	s.markKnownGroupMemberLeft(context.Background(), group.ChatID, target.TelegramUserID, gotgbot.ChatMemberStatusLeft)

	_, _ = s.getSender(b).SendMessage(group.ChatID, fmt.Sprintf("🦆 %s покидает наш уютный пруд и улетает в тёплые края.", moderationTargetDisplayName(target)), nil)
	return nil
}

func (s *telegramService) requireGroupModerationAccess(b *gotgbot.Bot, ctx *ext.Context) (db.TelegramGroup, bool) {
	group := db.TelegramGroup{}
	if ctx == nil || ctx.EffectiveChat == nil {
		return group, false
	}
	if !isGroupChat(ctx.EffectiveChat) {
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Эта команда доступна только в группе.", nil)
		return group, false
	}
	if b == nil || ctx.EffectiveUser == nil || ctx.EffectiveChat == nil || s.queries == nil {
		return group, false
	}

	loaded, err := s.queries.GetTelegramGroupByChatID(context.Background(), ctx.EffectiveChat.Id)
	if err != nil || !loaded.IsActive || !loaded.IsInitialized {
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Группа не инициализирована. Сначала выполни /init.", nil)
		return group, false
	}
	if !s.isGroupModerationCommandsEnabled(context.Background(), loaded.ChatID) {
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Команды модерации выключены в настройках группы.", nil)
		return group, false
	}
	if loaded.OwnerTelegramUserID != ctx.EffectiveUser.Id {
		_, _ = s.getSender(b).SendMessage(ctx.EffectiveChat.Id, "Сейчас эту команду может выполнять только владелец группы.", nil)
		return group, false
	}
	return loaded, true
}

func (s *telegramService) isGroupModerationCommandsEnabled(ctx context.Context, chatID int64) bool {
	reader, ok := s.queries.(groupModerationCommandsConfigReader)
	if !ok {
		return true
	}
	enabled, err := reader.GetTelegramGroupModerationCommandsEnabledByChatID(ctx, chatID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("failed to read moderation_commands_enabled, fallback to enabled", "chat_id", chatID, "error", err)
		}
		return true
	}
	return enabled
}

func (s *telegramService) ensureBotCanRestrictMembers(ctx context.Context, b *gotgbot.Bot, chatID int64) bool {
	botMember, err := s.getRawChatMember(ctx, b, chatID, b.Id)
	if err != nil {
		return false
	}
	return canRestrictMembers(botMember)
}

func (s *telegramService) resolveMuteTargetAndDuration(ctx context.Context, b *gotgbot.Bot, chatID int64, msg *gotgbot.Message, argsRaw string) (moderationTarget, time.Duration, error) {
	replyTarget, hasReply := moderationTargetFromReply(msg)
	args := strings.TrimSpace(argsRaw)

	if hasReply {
		duration, durationErr := parseModerationDuration(args)
		if durationErr == nil {
			return replyTarget, duration, nil
		}

		targetRaw, durationRaw, ok := splitTargetAndRemainder(args)
		if !ok {
			return moderationTarget{}, 0, durationErr
		}
		target, err := s.resolveModerationTarget(ctx, b, chatID, targetRaw)
		if err != nil {
			return moderationTarget{}, 0, err
		}
		duration, err = parseModerationDuration(durationRaw)
		if err != nil {
			return moderationTarget{}, 0, err
		}
		return target, duration, nil
	}

	targetRaw, durationRaw, ok := splitTargetAndRemainder(args)
	if !ok {
		return moderationTarget{}, 0, errors.New("неверный формат, используй /mute ник|@username|id время")
	}
	target, err := s.resolveModerationTarget(ctx, b, chatID, targetRaw)
	if err != nil {
		return moderationTarget{}, 0, err
	}
	duration, err := parseModerationDuration(durationRaw)
	if err != nil {
		return moderationTarget{}, 0, err
	}
	return target, duration, nil
}

func (s *telegramService) resolveModerationTargetForSimpleCommand(ctx context.Context, b *gotgbot.Bot, chatID int64, msg *gotgbot.Message, argsRaw string, command string) (moderationTarget, error) {
	args := strings.TrimSpace(argsRaw)
	if args != "" {
		targetRaw := strings.Fields(args)[0]
		return s.resolveModerationTarget(ctx, b, chatID, targetRaw)
	}

	target, ok := moderationTargetFromReply(msg)
	if ok {
		return target, nil
	}
	return moderationTarget{}, fmt.Errorf("неверный формат, используй %s в reply или %s ник|@username|id", command, command)
}

func (s *telegramService) resolveModerationTarget(ctx context.Context, b *gotgbot.Bot, chatID int64, raw string) (moderationTarget, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return moderationTarget{}, errors.New("укажи цель: ник, @username или telegram id")
	}

	if strings.HasPrefix(input, "@") {
		username, ok := normalizeTelegramUsernameInput(input)
		if !ok {
			return moderationTarget{}, errors.New("неверный @username, используй формат @username")
		}
		tgID, err := s.resolveTelegramIDByUsername(ctx, b, chatID, username)
		if err != nil {
			return moderationTarget{}, fmt.Errorf("не удалось найти telegram id для @%s", username)
		}
		return moderationTarget{
			TelegramUserID: tgID,
			Source:         "username",
			Username:       username,
		}, nil
	}

	if tgID, err := strconv.ParseInt(input, 10, 64); err == nil {
		if tgID <= 0 {
			return moderationTarget{}, errors.New("telegram id должен быть положительным числом")
		}
		return moderationTarget{
			TelegramUserID: tgID,
			Source:         "id",
		}, nil
	}

	account, err := s.queries.GetUserAccountByS21Login(ctx, input)
	if err != nil {
		return moderationTarget{}, fmt.Errorf("не удалось найти telegram id для ника %s", input)
	}

	tgID, err := strconv.ParseInt(strings.TrimSpace(account.ExternalID), 10, 64)
	if err != nil || tgID <= 0 {
		return moderationTarget{}, fmt.Errorf("для ника %s не найден привязанный telegram id", input)
	}
	return moderationTarget{
		TelegramUserID: tgID,
		Source:         "login",
		Login:          strings.TrimSpace(account.S21Login),
		Username:       strings.TrimSpace(account.Username.String),
	}, nil
}

func (s *telegramService) resolveTelegramIDByUsername(ctx context.Context, b *gotgbot.Bot, chatID int64, username string) (int64, error) {
	if resolver, ok := s.queries.(telegramUserAccountByUsernameResolver); ok {
		account, err := resolver.GetTelegramUserAccountByUsername(ctx, username)
		if err == nil {
			tgID, convErr := strconv.ParseInt(strings.TrimSpace(account.ExternalID), 10, 64)
			if convErr == nil && tgID > 0 {
				return tgID, nil
			}
		}
	}

	known, err := s.queries.ListTelegramGroupKnownMembers(ctx, chatID)
	if err != nil {
		return 0, err
	}
	for _, row := range known {
		if row.TelegramUserID <= 0 || row.IsBot {
			continue
		}
		_, uname, identityErr := s.getChatMemberIdentity(ctx, b, chatID, row.TelegramUserID)
		if identityErr != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(uname), username) {
			return row.TelegramUserID, nil
		}
	}
	return 0, errors.New("telegram id not found")
}

func moderationTargetFromReply(msg *gotgbot.Message) (moderationTarget, bool) {
	if msg == nil || msg.ReplyToMessage == nil || msg.ReplyToMessage.From == nil || msg.ReplyToMessage.From.Id <= 0 {
		return moderationTarget{}, false
	}
	target := moderationTarget{
		TelegramUserID: msg.ReplyToMessage.From.Id,
		Source:         "reply",
		Username:       strings.TrimSpace(msg.ReplyToMessage.From.Username),
	}
	return target, true
}

func extractCommandArgs(msg *gotgbot.Message) string {
	if msg == nil {
		return ""
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Caption)
	}
	if text == "" {
		return ""
	}
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
}

func splitTargetAndRemainder(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return "", "", false
	}
	target := parts[0]
	remainder := strings.TrimSpace(strings.TrimPrefix(trimmed, target))
	if remainder == "" {
		return "", "", false
	}
	return target, remainder, true
}

func parseModerationDuration(raw string) (time.Duration, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return 0, errors.New("укажи длительность мута, пример: 10m, 2h, 1d, 30s")
	}

	matches := moderationDurationPattern.FindStringSubmatch(normalized)
	if len(matches) != 3 {
		return 0, errors.New("неверный формат времени, пример: 10m, 2h, 1d, 30s")
	}

	value, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("длительность должна быть больше нуля")
	}

	unit := []rune(matches[2])[0]
	var base time.Duration
	switch unit {
	case 'd', 'д':
		base = 24 * time.Hour
	case 'h', 'ч':
		base = time.Hour
	case 'm', 'м':
		base = time.Minute
	case 's', 'с':
		base = time.Second
	default:
		return 0, errors.New("неверная единица времени, используй d/h/m/s или д/ч/м/с")
	}

	maxValue := int64((1<<63 - 1) / int64(base))
	if value > maxValue {
		return 0, errors.New("слишком большая длительность")
	}

	duration := time.Duration(value) * base
	if duration <= 0 {
		return 0, errors.New("длительность должна быть больше нуля")
	}
	return duration, nil
}

func normalizeTelegramUsernameInput(raw string) (string, bool) {
	username := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
	if len(username) < 5 || len(username) > 32 {
		return "", false
	}

	for i, r := range username {
		if i == 0 && !isASCIIAlpha(r) {
			return "", false
		}
		if !isASCIIAlphaNumeric(r) && r != '_' {
			return "", false
		}
	}
	return strings.ToLower(username), true
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIAlphaNumeric(r rune) bool {
	return isASCIIAlpha(r) || (r >= '0' && r <= '9')
}

func describeModerationTarget(target moderationTarget) string {
	idLabel := fmt.Sprintf("ID %d", target.TelegramUserID)
	switch target.Source {
	case "login":
		if strings.TrimSpace(target.Login) != "" {
			return fmt.Sprintf("%s (%s)", target.Login, idLabel)
		}
	case "username":
		if strings.TrimSpace(target.Username) != "" {
			return fmt.Sprintf("@%s (%s)", target.Username, idLabel)
		}
	case "reply":
		if strings.TrimSpace(target.Username) != "" {
			return fmt.Sprintf("@%s (%s)", target.Username, idLabel)
		}
	}
	return idLabel
}

func moderationTargetDisplayName(target moderationTarget) string {
	switch target.Source {
	case "login":
		if strings.TrimSpace(target.Login) != "" {
			return target.Login
		}
	case "username", "reply":
		if strings.TrimSpace(target.Username) != "" {
			return "@" + target.Username
		}
	}
	return fmt.Sprintf("ID %d", target.TelegramUserID)
}

func formatModerationTargetOpenMessageLink(target moderationTarget) string {
	return fmt.Sprintf(
		`<a href="tg://openmessage?user_id=%d">%s</a>`,
		target.TelegramUserID,
		html.EscapeString(moderationTargetDisplayName(target)),
	)
}

func formatModerationDurationRussian(duration time.Duration) string {
	if duration%(24*time.Hour) == 0 {
		value := int64(duration / (24 * time.Hour))
		return fmt.Sprintf("%d %s", value, russianPlural(value, "день", "дня", "дней"))
	}
	if duration%time.Hour == 0 {
		value := int64(duration / time.Hour)
		return fmt.Sprintf("%d %s", value, russianPlural(value, "час", "часа", "часов"))
	}
	if duration%time.Minute == 0 {
		value := int64(duration / time.Minute)
		return fmt.Sprintf("%d %s", value, russianPlural(value, "минута", "минуты", "минут"))
	}
	value := int64(duration / time.Second)
	return fmt.Sprintf("%d %s", value, russianPlural(value, "секунда", "секунды", "секунд"))
}

func russianPlural(value int64, one, few, many string) string {
	value = absInt64(value) % 100
	if value >= 11 && value <= 14 {
		return many
	}
	switch value % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *telegramService) restrictChatMemberForDuration(ctx context.Context, b *gotgbot.Bot, chatID, userID int64, duration time.Duration) (time.Time, error) {
	if b == nil {
		return time.Time{}, errors.New("bot is nil")
	}

	untilUTC := time.Now().UTC().Add(duration)
	resp, err := b.RequestWithContext(ctx, "restrictChatMember", map[string]any{
		"chat_id":                          chatID,
		"user_id":                          userID,
		"permissions":                      mutedChatPermissions(),
		"use_independent_chat_permissions": true,
		"until_date":                       untilUTC.Unix(),
	}, nil)
	if err != nil {
		return time.Time{}, err
	}
	if err := decodeTelegramBoolResponse("restrictChatMember", resp); err != nil {
		return time.Time{}, err
	}
	return untilUTC, nil
}

func (s *telegramService) unrestrictChatMember(ctx context.Context, b *gotgbot.Bot, chatID, userID int64) error {
	if b == nil {
		return errors.New("bot is nil")
	}

	resp, err := b.RequestWithContext(ctx, "restrictChatMember", map[string]any{
		"chat_id":                          chatID,
		"user_id":                          userID,
		"permissions":                      unmutedChatPermissions(),
		"use_independent_chat_permissions": true,
		"until_date":                       0,
	}, nil)
	if err != nil {
		return err
	}
	return decodeTelegramBoolResponse("restrictChatMember", resp)
}

func (s *telegramService) banChatMember(ctx context.Context, b *gotgbot.Bot, chatID, userID int64) error {
	if b == nil {
		return errors.New("bot is nil")
	}
	resp, err := b.RequestWithContext(ctx, "banChatMember", map[string]any{
		"chat_id":         chatID,
		"user_id":         userID,
		"revoke_messages": true,
	}, nil)
	if err != nil {
		return err
	}
	return decodeTelegramBoolResponse("banChatMember", resp)
}

func (s *telegramService) unbanChatMember(ctx context.Context, b *gotgbot.Bot, chatID, userID int64, onlyIfBanned bool) error {
	if b == nil {
		return errors.New("bot is nil")
	}
	resp, err := b.RequestWithContext(ctx, "unbanChatMember", map[string]any{
		"chat_id":        chatID,
		"user_id":        userID,
		"only_if_banned": onlyIfBanned,
	}, nil)
	if err != nil {
		return err
	}
	return decodeTelegramBoolResponse("unbanChatMember", resp)
}

func decodeTelegramBoolResponse(method string, raw json.RawMessage) error {
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		return fmt.Errorf("failed to decode %s response: %w", method, err)
	}
	if !ok {
		return fmt.Errorf("%s returned false", method)
	}
	return nil
}

func mutedChatPermissions() map[string]any {
	return map[string]any{
		"can_send_messages":         false,
		"can_send_audios":           false,
		"can_send_documents":        false,
		"can_send_photos":           false,
		"can_send_videos":           false,
		"can_send_video_notes":      false,
		"can_send_voice_notes":      false,
		"can_send_polls":            false,
		"can_send_other_messages":   false,
		"can_add_web_page_previews": false,
	}
}

func unmutedChatPermissions() map[string]any {
	return map[string]any{
		"can_send_messages":         true,
		"can_send_audios":           true,
		"can_send_documents":        true,
		"can_send_photos":           true,
		"can_send_videos":           true,
		"can_send_video_notes":      true,
		"can_send_voice_notes":      true,
		"can_send_polls":            true,
		"can_send_other_messages":   true,
		"can_add_web_page_previews": true,
	}
}
