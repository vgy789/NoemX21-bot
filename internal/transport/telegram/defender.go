package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	defenderSourceAutoJoin  = "auto_join"
	defenderSourceManualRun = "manual_run"
	defenderSourcePreview   = "preview"

	defenderActionRemoved          = "removed"
	defenderActionSkippedWhitelist = "skipped_whitelist"
	defenderActionSkippedNoRights  = "skipped_no_rights"
	defenderActionSkippedNotMember = "skipped_not_member"

	defenderReasonUnregistered = "unregistered"
	defenderReasonBlocked      = "blocked"
	defenderReasonExpelled     = "expelled"
	defenderReasonWhitelist    = "whitelist"
	defenderReasonBotRights    = "bot_rights"
	defenderReasonNotMember    = "not_member"
)

type defenderDecision struct {
	ShouldRemove bool
	Reason       string
	RemovedAs    string
}

type telegramDefenderRunner struct {
	svc *telegramService
	bot *gotgbot.Bot
}

func (s *telegramService) newDefenderRunner(bot *gotgbot.Bot) fsm.DefenderRunner {
	if s == nil || bot == nil {
		return nil
	}
	return &telegramDefenderRunner{svc: s, bot: bot}
}

func (r *telegramDefenderRunner) RunGroupDefender(ctx context.Context, ownerTelegramUserID, chatID int64) (fsm.DefenderRunResult, error) {
	return r.svc.runGroupDefender(ctx, r.bot, ownerTelegramUserID, chatID)
}

func (r *telegramDefenderRunner) PreviewGroupDefenderCandidates(ctx context.Context, ownerTelegramUserID, chatID int64) ([]fsm.DefenderPreviewItem, error) {
	return r.svc.previewGroupDefenderCandidates(ctx, r.bot, ownerTelegramUserID, chatID)
}

func (r *telegramDefenderRunner) ResolveGroupMemberIdentity(ctx context.Context, ownerTelegramUserID, chatID, telegramUserID int64) (string, string, error) {
	if r == nil || r.svc == nil || r.svc.queries == nil || r.bot == nil {
		return "", "", errors.New("defender dependencies are not ready")
	}
	group, err := r.svc.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return "", "", fmt.Errorf("failed to load group: %w", err)
	}
	if group.OwnerTelegramUserID != ownerTelegramUserID || !group.IsActive || !group.IsInitialized {
		return "", "", errors.New("group access denied")
	}
	return r.svc.getChatMemberIdentity(ctx, r.bot, chatID, telegramUserID)
}

func (s *telegramService) runGroupDefender(ctx context.Context, b *gotgbot.Bot, ownerTelegramUserID, chatID int64) (fsm.DefenderRunResult, error) {
	result := fsm.DefenderRunResult{}
	if s == nil || s.queries == nil || s.userSvc == nil || b == nil {
		return result, errors.New("defender dependencies are not ready")
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return result, fmt.Errorf("failed to load group: %w", err)
	}
	if group.OwnerTelegramUserID != ownerTelegramUserID || !group.IsActive || !group.IsInitialized {
		return result, errors.New("group access denied")
	}

	knownMembers, err := s.queries.ListTelegramGroupKnownMembers(ctx, chatID)
	if err != nil {
		return result, fmt.Errorf("failed to list known members: %w", err)
	}
	if len(knownMembers) == 0 {
		return result, nil
	}

	botMember, err := s.getRawChatMember(ctx, b, chatID, b.Id)
	if err != nil {
		return result, fmt.Errorf("failed to verify bot rights: %w", err)
	}
	canKick := canRestrictMembers(botMember)

	for _, known := range knownMembers {
		if known.IsBot {
			continue
		}

		memberState, err := s.getRawChatMember(ctx, b, chatID, known.TelegramUserID)
		if err != nil {
			result.Errors++
			continue
		}
		if !isRawMemberActive(memberState) {
			s.markKnownGroupMemberLeft(ctx, chatID, known.TelegramUserID, memberState.Status)
			result.SkippedNotMember++
			s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionSkippedNotMember, defenderReasonNotMember, "inactive")
			continue
		}
		if !isRegularMemberForDefender(memberState) {
			result.SkippedNotMember++
			s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionSkippedNotMember, defenderReasonNotMember, memberState.Status)
			continue
		}

		isWhitelisted, err := s.queries.ExistsTelegramGroupWhitelist(ctx, db.ExistsTelegramGroupWhitelistParams{
			ChatID:         chatID,
			TelegramUserID: known.TelegramUserID,
		})
		if err != nil {
			result.Errors++
			continue
		}
		if isWhitelisted {
			result.SkippedWhitelist++
			s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionSkippedWhitelist, defenderReasonWhitelist, "")
			continue
		}

		decision, err := s.evaluateDefenderDecision(ctx, known.TelegramUserID, group.DefenderRemoveBlocked)
		if err != nil {
			result.Errors++
			continue
		}
		if !decision.ShouldRemove {
			continue
		}

		if !canKick {
			result.SkippedNoRights++
			s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionSkippedNoRights, defenderReasonBotRights, "")
			continue
		}

		if err := s.kickChatMember(ctx, b, chatID, known.TelegramUserID); err != nil {
			result.Errors++
			s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionSkippedNoRights, defenderReasonBotRights, err.Error())
			continue
		}
		s.markKnownGroupMemberLeft(ctx, chatID, known.TelegramUserID, gotgbot.ChatMemberStatusLeft)
		s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionRemoved, decision.Reason, "")
		result.Removed++
		if decision.RemovedAs == defenderReasonUnregistered {
			result.SkippedUnregistered++
		}
		if decision.RemovedAs == defenderReasonBlocked || decision.RemovedAs == defenderReasonExpelled {
			result.SkippedBlocked++
		}
	}

	return result, nil
}

func (s *telegramService) previewGroupDefenderCandidates(ctx context.Context, b *gotgbot.Bot, ownerTelegramUserID, chatID int64) ([]fsm.DefenderPreviewItem, error) {
	if s == nil || s.queries == nil || s.userSvc == nil || b == nil {
		return nil, errors.New("defender dependencies are not ready")
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to load group: %w", err)
	}
	if group.OwnerTelegramUserID != ownerTelegramUserID || !group.IsActive || !group.IsInitialized {
		return nil, errors.New("group access denied")
	}

	knownMembers, err := s.queries.ListTelegramGroupKnownMembers(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to list known members: %w", err)
	}

	items := make([]fsm.DefenderPreviewItem, 0)
	for _, known := range knownMembers {
		if known.IsBot {
			continue
		}

		memberState, err := s.getRawChatMember(ctx, b, chatID, known.TelegramUserID)
		if err != nil {
			continue
		}
		if !isRawMemberActive(memberState) || !isRegularMemberForDefender(memberState) {
			continue
		}

		isWhitelisted, err := s.queries.ExistsTelegramGroupWhitelist(ctx, db.ExistsTelegramGroupWhitelistParams{
			ChatID:         chatID,
			TelegramUserID: known.TelegramUserID,
		})
		if err != nil || isWhitelisted {
			continue
		}

		decision, err := s.evaluateDefenderDecision(ctx, known.TelegramUserID, group.DefenderRemoveBlocked)
		if err != nil || !decision.ShouldRemove {
			continue
		}

		items = append(items, fsm.DefenderPreviewItem{
			TelegramUserID: known.TelegramUserID,
			DisplayName:    "",
			Username:       "",
			Reason:         decision.Reason,
		})
		if name, username, err := s.getChatMemberIdentity(ctx, b, chatID, known.TelegramUserID); err == nil {
			items[len(items)-1].DisplayName = name
			items[len(items)-1].Username = username
		}
	}

	return items, nil
}

func (s *telegramService) getChatMemberIdentity(ctx context.Context, b *gotgbot.Bot, chatID, userID int64) (string, string, error) {
	if b == nil {
		return "", "", errors.New("bot is nil")
	}
	resp, err := b.RequestWithContext(ctx, "getChatMember", map[string]any{
		"chat_id": chatID,
		"user_id": userID,
	}, nil)
	if err != nil {
		return "", "", err
	}

	var decoded struct {
		User struct {
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(resp, &decoded); err != nil {
		return "", "", err
	}

	name := strings.TrimSpace(strings.TrimSpace(decoded.User.FirstName) + " " + strings.TrimSpace(decoded.User.LastName))
	return name, strings.TrimSpace(decoded.User.Username), nil
}

func (s *telegramService) tryAutoDefenderForKnownGroup(ctx context.Context, b *gotgbot.Bot, group db.TelegramGroup, telegramUserID int64) {
	if s == nil || s.queries == nil || s.userSvc == nil || b == nil || telegramUserID == 0 {
		return
	}
	if !group.IsActive || !group.IsInitialized || !group.DefenderEnabled {
		return
	}

	memberState, err := s.getRawChatMember(ctx, b, group.ChatID, telegramUserID)
	if err != nil {
		return
	}
	if !isRawMemberActive(memberState) {
		s.markKnownGroupMemberLeft(ctx, group.ChatID, telegramUserID, memberState.Status)
		s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionSkippedNotMember, defenderReasonNotMember, "inactive")
		return
	}
	if !isRegularMemberForDefender(memberState) {
		s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionSkippedNotMember, defenderReasonNotMember, memberState.Status)
		return
	}

	isWhitelisted, err := s.queries.ExistsTelegramGroupWhitelist(ctx, db.ExistsTelegramGroupWhitelistParams{
		ChatID:         group.ChatID,
		TelegramUserID: telegramUserID,
	})
	if err != nil {
		return
	}
	if isWhitelisted {
		s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionSkippedWhitelist, defenderReasonWhitelist, "")
		return
	}

	decision, err := s.evaluateDefenderDecision(ctx, telegramUserID, group.DefenderRemoveBlocked)
	if err != nil {
		return
	}
	if !decision.ShouldRemove {
		return
	}

	botMember, err := s.getRawChatMember(ctx, b, group.ChatID, b.Id)
	if err != nil {
		return
	}
	if !canRestrictMembers(botMember) {
		s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionSkippedNoRights, defenderReasonBotRights, "")
		return
	}

	if err := s.kickChatMember(ctx, b, group.ChatID, telegramUserID); err != nil {
		s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionSkippedNoRights, defenderReasonBotRights, err.Error())
		return
	}
	s.markKnownGroupMemberLeft(ctx, group.ChatID, telegramUserID, gotgbot.ChatMemberStatusLeft)
	s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionRemoved, decision.Reason, "")
}

func (s *telegramService) evaluateDefenderDecision(ctx context.Context, telegramUserID int64, removeBlocked bool) (defenderDecision, error) {
	profile, err := s.userSvc.GetProfileByTelegramID(ctx, telegramUserID)
	if err != nil {
		if isUnregisteredProfileErr(err) {
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonUnregistered, RemovedAs: defenderReasonUnregistered}, nil
		}
		return defenderDecision{}, err
	}
	if removeBlocked {
		switch profile.Status {
		case db.EnumStudentStatusBLOCKED:
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonBlocked, RemovedAs: defenderReasonBlocked}, nil
		case db.EnumStudentStatusEXPELLED:
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonExpelled, RemovedAs: defenderReasonExpelled}, nil
		}
	}
	return defenderDecision{}, nil
}

func isUnregisteredProfileErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "user account not found") ||
		strings.Contains(msg, "user profile not found") ||
		strings.Contains(msg, "no rows")
}

func isRegularMemberForDefender(member rawChatMember) bool {
	switch strings.TrimSpace(member.Status) {
	case gotgbot.ChatMemberStatusMember:
		return true
	case gotgbot.ChatMemberStatusRestricted:
		return member.IsMember
	default:
		return false
	}
}

func canRestrictMembers(member rawChatMember) bool {
	switch strings.TrimSpace(member.Status) {
	case gotgbot.ChatMemberStatusOwner:
		return true
	case gotgbot.ChatMemberStatusAdministrator:
		return member.CanRestrict
	default:
		return false
	}
}

func (s *telegramService) kickChatMember(ctx context.Context, b *gotgbot.Bot, chatID, userID int64) error {
	if b == nil {
		return errors.New("bot is nil")
	}

	banResp, err := b.RequestWithContext(ctx, "banChatMember", map[string]any{
		"chat_id":         chatID,
		"user_id":         userID,
		"revoke_messages": true,
	}, nil)
	if err != nil {
		return err
	}
	var banned bool
	if err := json.Unmarshal(banResp, &banned); err != nil {
		return fmt.Errorf("failed to decode banChatMember response: %w", err)
	}
	if !banned {
		return errors.New("banChatMember returned false")
	}

	unbanResp, err := b.RequestWithContext(ctx, "unbanChatMember", map[string]any{
		"chat_id":        chatID,
		"user_id":        userID,
		"only_if_banned": true,
	}, nil)
	if err != nil {
		return err
	}
	var unbanned bool
	if err := json.Unmarshal(unbanResp, &unbanned); err != nil {
		return fmt.Errorf("failed to decode unbanChatMember response: %w", err)
	}
	if !unbanned {
		return errors.New("unbanChatMember returned false")
	}
	return nil
}

func (s *telegramService) logDefenderAction(ctx context.Context, chatID int64, actionSource string, telegramUserID int64, action, reason, details string) {
	if s == nil || s.queries == nil || chatID == 0 {
		return
	}
	if err := s.queries.InsertTelegramGroupLog(ctx, db.InsertTelegramGroupLogParams{
		ChatID:         chatID,
		Source:         strings.TrimSpace(actionSource),
		TelegramUserID: telegramUserID,
		Action:         strings.TrimSpace(action),
		Reason:         strings.TrimSpace(reason),
		Details:        strings.TrimSpace(details),
	}); err != nil {
		s.log.Debug("failed to save telegram group log", "chat_id", chatID, "telegram_user_id", telegramUserID, "action", action, "error", err)
	}
}
