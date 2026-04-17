package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	defenderSourceAutoJoin  = "auto_join"
	defenderSourceManualRun = "manual_run"

	defenderBanDefaultSec = int32(24 * 60 * 60)
	defenderBanMinSec     = int32(5 * 60)
	defenderBanMaxSec     = int32(30 * 24 * 60 * 60)

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
	defenderReasonCampusFilter = "campus_filter"
	defenderReasonTribeFilter  = "tribe_filter"
	defenderReasonCampusTarget = "campus_selected"
	defenderReasonTribeTarget  = "tribe_selected"
)

type defenderDecision struct {
	ShouldRemove bool
	Reason       string
	RemovedAs    string
}

type defenderFilterConfig struct {
	CampusIDs     map[string]struct{}
	TribeIDs      map[string]map[int16]struct{}
	TribeNames    map[string]map[int16]string
	HasCampusRule bool
	HasTribeRule  bool
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
	filters, err := s.loadDefenderFilterConfig(ctx, group.ChatID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("defender: failed to load filter config, fallback to base rules", "chat_id", group.ChatID, "error", err)
		}
		filters = emptyDefenderFilterConfig()
	}
	manualFilter, hasManualFilter := fsm.DefenderManualFilterFromContext(ctx)

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

		decision, err := s.evaluateDefenderDecision(ctx, known.TelegramUserID, group, filters, manualFilter, hasManualFilter)
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

		banDurationSec := normalizeDefenderBanDurationSec(group.DefenderBanDurationSec)
		untilUTC, err := s.banChatMemberForDuration(ctx, b, chatID, known.TelegramUserID, banDurationSec)
		if err != nil {
			result.Errors++
			s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionSkippedNoRights, defenderReasonBotRights, err.Error())
			continue
		}
		s.markKnownGroupMemberLeft(ctx, chatID, known.TelegramUserID, gotgbot.ChatMemberStatusBanned)
		s.logDefenderAction(ctx, chatID, defenderSourceManualRun, known.TelegramUserID, defenderActionRemoved, decision.Reason, formatDefenderBanDetails(banDurationSec, untilUTC))
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
	filters, err := s.loadDefenderFilterConfig(ctx, group.ChatID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("defender: failed to load filter config for preview, fallback to base rules", "chat_id", group.ChatID, "error", err)
		}
		filters = emptyDefenderFilterConfig()
	}
	manualFilter, hasManualFilter := fsm.DefenderManualFilterFromContext(ctx)

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

		decision, err := s.evaluateDefenderDecision(ctx, known.TelegramUserID, group, filters, manualFilter, hasManualFilter)
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

	filters, err := s.loadDefenderFilterConfig(ctx, group.ChatID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("defender: failed to load filter config for auto join, fallback to base rules", "chat_id", group.ChatID, "error", err)
		}
		filters = emptyDefenderFilterConfig()
	}
	decision, err := s.evaluateDefenderDecision(ctx, telegramUserID, group, filters, fsm.DefenderManualFilter{}, false)
	if err != nil {
		if s.log != nil {
			s.log.Warn("defender: failed to evaluate auto decision", "chat_id", group.ChatID, "user_id", telegramUserID, "error", err)
		}
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

	banDurationSec := normalizeDefenderBanDurationSec(group.DefenderBanDurationSec)
	untilUTC, err := s.banChatMemberForDuration(ctx, b, group.ChatID, telegramUserID, banDurationSec)
	if err != nil {
		s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionSkippedNoRights, defenderReasonBotRights, err.Error())
		return
	}
	s.markKnownGroupMemberLeft(ctx, group.ChatID, telegramUserID, gotgbot.ChatMemberStatusBanned)
	s.logDefenderAction(ctx, group.ChatID, defenderSourceAutoJoin, telegramUserID, defenderActionRemoved, decision.Reason, formatDefenderBanDetails(banDurationSec, untilUTC))
}

func (s *telegramService) evaluateDefenderDecision(
	ctx context.Context,
	telegramUserID int64,
	group db.TelegramGroup,
	filters defenderFilterConfig,
	manualFilter fsm.DefenderManualFilter,
	hasManualFilter bool,
) (defenderDecision, error) {
	profile, err := s.userSvc.GetProfileByTelegramID(ctx, telegramUserID)
	if err != nil {
		if isUnregisteredProfileErr(err) {
			if hasManualFilter && manualFilter.Scope != fsm.DefenderManualScopeUnregistered {
				return defenderDecision{}, nil
			}
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonUnregistered, RemovedAs: defenderReasonUnregistered}, nil
		}
		return defenderDecision{}, err
	}

	profileCampus := pgtype.UUID{}
	if err := profileCampus.Scan(strings.TrimSpace(profile.CampusID)); err != nil {
		profileCampus = pgtype.UUID{}
	}
	profileCampusKey := uuidToString(profileCampus)

	if hasManualFilter {
		switch manualFilter.Scope {
		case fsm.DefenderManualScopeUnregistered:
			return defenderDecision{}, nil
		case fsm.DefenderManualScopeBlocked:
			switch profile.Status {
			case db.EnumStudentStatusBLOCKED:
				return defenderDecision{ShouldRemove: true, Reason: defenderReasonBlocked, RemovedAs: defenderReasonBlocked}, nil
			case db.EnumStudentStatusEXPELLED:
				return defenderDecision{ShouldRemove: true, Reason: defenderReasonExpelled, RemovedAs: defenderReasonExpelled}, nil
			default:
				return defenderDecision{}, nil
			}
		case fsm.DefenderManualScopeCampus:
			targetCampus := pgtype.UUID{}
			if err := targetCampus.Scan(strings.TrimSpace(manualFilter.CampusID)); err != nil || !targetCampus.Valid {
				return defenderDecision{}, nil
			}
			if profileCampus.Valid && profileCampus == targetCampus {
				return defenderDecision{ShouldRemove: true, Reason: defenderReasonCampusTarget, RemovedAs: defenderReasonCampusTarget}, nil
			}
			return defenderDecision{}, nil
		case fsm.DefenderManualScopeTribe:
			targetCampus := pgtype.UUID{}
			if err := targetCampus.Scan(strings.TrimSpace(manualFilter.CampusID)); err != nil || !targetCampus.Valid {
				return defenderDecision{}, nil
			}
			if !profileCampus.Valid || profileCampus != targetCampus {
				return defenderDecision{}, nil
			}
			targetName := strings.TrimSpace(filters.TribeNames[uuidToString(targetCampus)][manualFilter.TribeID])
			if targetName == "" {
				return defenderDecision{}, nil
			}
			if strings.EqualFold(strings.TrimSpace(profile.CoalitionName), targetName) {
				return defenderDecision{ShouldRemove: true, Reason: defenderReasonTribeTarget, RemovedAs: defenderReasonTribeTarget}, nil
			}
			return defenderDecision{}, nil
		}
	}

	if filters.HasCampusRule {
		if !profileCampus.Valid {
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonCampusFilter, RemovedAs: defenderReasonCampusFilter}, nil
		}
		if _, ok := filters.CampusIDs[profileCampusKey]; !ok {
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonCampusFilter, RemovedAs: defenderReasonCampusFilter}, nil
		}
	}

	if filters.HasTribeRule {
		if !profileCampus.Valid {
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonTribeFilter, RemovedAs: defenderReasonTribeFilter}, nil
		}
		selectedTribes := filters.TribeIDs[profileCampusKey]
		if len(selectedTribes) == 0 {
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonTribeFilter, RemovedAs: defenderReasonTribeFilter}, nil
		}
		coalitionName := strings.TrimSpace(profile.CoalitionName)
		match := false
		for tribeID := range selectedTribes {
			targetName := strings.TrimSpace(filters.TribeNames[profileCampusKey][tribeID])
			if targetName != "" && strings.EqualFold(coalitionName, targetName) {
				match = true
				break
			}
		}
		if !match {
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonTribeFilter, RemovedAs: defenderReasonTribeFilter}, nil
		}
	}

	if group.DefenderRemoveBlocked {
		switch profile.Status {
		case db.EnumStudentStatusBLOCKED:
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonBlocked, RemovedAs: defenderReasonBlocked}, nil
		case db.EnumStudentStatusEXPELLED:
			return defenderDecision{ShouldRemove: true, Reason: defenderReasonExpelled, RemovedAs: defenderReasonExpelled}, nil
		}
	}
	return defenderDecision{}, nil
}

func (s *telegramService) loadDefenderFilterConfig(ctx context.Context, chatID int64) (defenderFilterConfig, error) {
	cfg := defenderFilterConfig{
		CampusIDs:  map[string]struct{}{},
		TribeIDs:   map[string]map[int16]struct{}{},
		TribeNames: map[string]map[int16]string{},
	}

	campusRows, err := s.queries.ListTelegramGroupDefenderCampusFilters(ctx, chatID)
	if err != nil {
		return cfg, fmt.Errorf("failed to load defender campus filters: %w", err)
	}
	for _, row := range campusRows {
		key := uuidToString(row.CampusID)
		if key == "" {
			continue
		}
		cfg.CampusIDs[key] = struct{}{}
	}
	cfg.HasCampusRule = len(cfg.CampusIDs) > 0

	tribeRows, err := s.queries.ListTelegramGroupDefenderTribeFilters(ctx, chatID)
	if err != nil {
		return cfg, fmt.Errorf("failed to load defender tribe filters: %w", err)
	}
	for _, row := range tribeRows {
		key := uuidToString(row.CampusID)
		if key == "" {
			continue
		}
		if _, ok := cfg.TribeIDs[key]; !ok {
			cfg.TribeIDs[key] = map[int16]struct{}{}
		}
		cfg.TribeIDs[key][row.CoalitionID] = struct{}{}
	}
	cfg.HasTribeRule = len(tribeRows) > 0

	for campusKey := range cfg.TribeIDs {
		campusID := pgtype.UUID{}
		if err := campusID.Scan(campusKey); err != nil || !campusID.Valid {
			continue
		}
		coalitions, err := s.queries.ListCoalitionsByCampus(ctx, campusID)
		if err != nil {
			return cfg, fmt.Errorf("failed to load coalitions by campus: %w", err)
		}
		if _, ok := cfg.TribeNames[campusKey]; !ok {
			cfg.TribeNames[campusKey] = map[int16]string{}
		}
		for _, row := range coalitions {
			cfg.TribeNames[campusKey][row.ID] = strings.TrimSpace(row.Name)
		}
	}

	return cfg, nil
}

func emptyDefenderFilterConfig() defenderFilterConfig {
	return defenderFilterConfig{
		CampusIDs:  map[string]struct{}{},
		TribeIDs:   map[string]map[int16]struct{}{},
		TribeNames: map[string]map[int16]string{},
	}
}

func uuidToString(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	b := v.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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

func normalizeDefenderBanDurationSec(sec int32) int32 {
	if sec < defenderBanMinSec || sec > defenderBanMaxSec {
		return defenderBanDefaultSec
	}
	return sec
}

func formatDefenderBanDetails(durationSec int32, untilUTC time.Time) string {
	return fmt.Sprintf("duration_sec=%d; until_utc=%s", durationSec, untilUTC.UTC().Format(time.RFC3339))
}

func (s *telegramService) banChatMemberForDuration(ctx context.Context, b *gotgbot.Bot, chatID, userID int64, durationSec int32) (time.Time, error) {
	if b == nil {
		return time.Time{}, errors.New("bot is nil")
	}
	safeDuration := normalizeDefenderBanDurationSec(durationSec)
	untilUTC := time.Now().UTC().Add(time.Duration(safeDuration) * time.Second)

	banResp, err := b.RequestWithContext(ctx, "banChatMember", map[string]any{
		"chat_id":         chatID,
		"user_id":         userID,
		"revoke_messages": true,
		"until_date":      untilUTC.Unix(),
	}, nil)
	if err != nil {
		return time.Time{}, err
	}
	var banned bool
	if err := json.Unmarshal(banResp, &banned); err != nil {
		return time.Time{}, fmt.Errorf("failed to decode banChatMember response: %w", err)
	}
	if !banned {
		return time.Time{}, errors.New("banChatMember returned false")
	}
	return untilUTC, nil
}

func (s *telegramService) logDefenderAction(ctx context.Context, chatID int64, actionSource string, telegramUserID int64, action, reason, details string) {
	if s == nil || s.queries == nil || chatID == 0 {
		return
	}
	_ = ctx // Defender logs must not depend on update/request context cancellation.
	params := db.InsertTelegramGroupLogParams{
		ChatID:         chatID,
		Source:         strings.TrimSpace(actionSource),
		TelegramUserID: telegramUserID,
		Action:         strings.TrimSpace(action),
		Reason:         strings.TrimSpace(reason),
		Details:        strings.TrimSpace(details),
	}

	tryInsert := func() error {
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.queries.InsertTelegramGroupLog(writeCtx, params)
	}

	err := tryInsert()
	if err != nil {
		// One retry helps in transient DB/network hiccups, especially for update handlers.
		time.Sleep(100 * time.Millisecond)
		err = tryInsert()
	}
	if err != nil {
		if s.log != nil {
			s.log.Warn("defender: failed to save telegram group log",
				"chat_id", chatID,
				"telegram_user_id", telegramUserID,
				"source", actionSource,
				"action", action,
				"reason", reason,
				"error", err,
			)
		}
	}
}
