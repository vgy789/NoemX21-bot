package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

const (
	memberTagFormatLogin         = "login"
	memberTagFormatLoginLevel    = "login_level"
	memberTagFormatLoginCampusEn = "login_campus_en"
	memberTagFormatLoginCampusRu = "login_campus_ru"
	memberTagMaxRunes            = 16
)

type rawChatMember struct {
	Status         string `json:"status"`
	IsMember       bool   `json:"is_member,omitempty"`
	Tag            string `json:"tag,omitempty"`
	CanManageTags  bool   `json:"can_manage_tags,omitempty"`
	CanEditTag     bool   `json:"can_edit_tag,omitempty"`
	CanRestrict    bool   `json:"can_restrict_members,omitempty"`
	CanInviteUsers bool   `json:"can_invite_users,omitempty"`
	User           struct {
		ID    int64 `json:"id"`
		IsBot bool  `json:"is_bot"`
	} `json:"user"`
}

type telegramMemberTagRunner struct {
	svc *telegramService
	bot *gotgbot.Bot
}

func (s *telegramService) newMemberTagRunner(bot *gotgbot.Bot) fsm.MemberTagRunner {
	if s == nil || bot == nil {
		return nil
	}
	return &telegramMemberTagRunner{svc: s, bot: bot}
}

func (r *telegramMemberTagRunner) RunGroupMemberTags(ctx context.Context, ownerTelegramUserID, chatID int64, mode fsm.MemberTagRunMode) (fsm.MemberTagRunResult, error) {
	result, _, err := r.svc.runGroupMemberTagsWithRollback(ctx, r.bot, ownerTelegramUserID, chatID, mode)
	return result, err
}

func (r *telegramMemberTagRunner) RunGroupMemberTagsWithRollback(ctx context.Context, ownerTelegramUserID, chatID int64, mode fsm.MemberTagRunMode) (fsm.MemberTagRunResult, []fsm.MemberTagRollbackEntry, error) {
	return r.svc.runGroupMemberTagsWithRollback(ctx, r.bot, ownerTelegramUserID, chatID, mode)
}

func (r *telegramMemberTagRunner) RollbackGroupMemberTags(ctx context.Context, ownerTelegramUserID, chatID int64, entries []fsm.MemberTagRollbackEntry) (fsm.MemberTagRollbackResult, error) {
	return r.svc.rollbackGroupMemberTags(ctx, r.bot, ownerTelegramUserID, chatID, entries)
}

func (r *telegramMemberTagRunner) SyncMemberTagsForRegisteredUser(ctx context.Context, telegramUserID int64) error {
	return r.svc.syncMemberTagsForRegisteredUser(ctx, r.bot, telegramUserID)
}

func (s *telegramService) captureKnownMembersFromGroupMessage(ctx context.Context, b *gotgbot.Bot, msg *gotgbot.Message) {
	if s == nil || s.queries == nil || msg == nil {
		return
	}
	if !isGroupChat(&msg.Chat) {
		return
	}

	chatID := msg.Chat.Id
	if msg.From != nil {
		s.upsertKnownGroupMember(ctx, chatID, msg.From.Id, msg.From.IsBot, gotgbot.ChatMemberStatusMember, true)
		if !msg.From.IsBot {
			s.tryAutoModerationConsistencyForSender(ctx, b, chatID, msg.From.Id)
		}
	}

	for _, member := range msg.NewChatMembers {
		m := member
		s.upsertKnownGroupMember(ctx, chatID, m.Id, m.IsBot, gotgbot.ChatMemberStatusMember, true)
		if !m.IsBot {
			s.tryAutoAssignMemberTag(ctx, b, chatID, m.Id)
		}
	}

	if msg.LeftChatMember != nil {
		s.markKnownGroupMemberLeft(ctx, chatID, msg.LeftChatMember.Id, gotgbot.ChatMemberStatusLeft)
	}

	if msg.ChatOwnerChanged != nil {
		if err := s.queries.UnlinkTelegramGroupOwner(ctx, chatID); err != nil {
			s.log.Warn("failed to unlink group owner from owner-changed message event", "chat_id", chatID, "error", err)
		}
	}
}

func (s *telegramService) tryAutoModerationConsistencyForSender(ctx context.Context, b *gotgbot.Bot, chatID, telegramUserID int64) {
	if s == nil || s.queries == nil || b == nil || chatID == 0 || telegramUserID == 0 {
		return
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil || !group.IsActive || !group.IsInitialized {
		return
	}

	if group.DefenderEnabled && group.DefenderRecheckKnownMembers {
		s.tryAutoDefenderForKnownGroup(ctx, b, group, telegramUserID)
	}
	s.tryAutoAssignMemberTagForKnownGroup(ctx, b, group, telegramUserID)
}

func (s *telegramService) upsertKnownGroupMember(ctx context.Context, chatID, userID int64, isBot bool, status string, isMember bool) {
	if s == nil || s.queries == nil || chatID == 0 || userID == 0 {
		return
	}
	_, err := s.queries.UpsertTelegramGroupMember(ctx, db.UpsertTelegramGroupMemberParams{
		ChatID:         chatID,
		TelegramUserID: userID,
		IsMember:       isMember,
		IsBot:          isBot,
		LastStatus:     strings.TrimSpace(status),
		LastSeenAt:     nowTimestamptz(),
	})
	if err != nil {
		s.log.Debug("failed to upsert known telegram group member", "error_type", safeTelegramErrorType(err))
	}
}

func (s *telegramService) markKnownGroupMemberLeft(ctx context.Context, chatID, userID int64, status string) {
	if s == nil || s.queries == nil || chatID == 0 || userID == 0 {
		return
	}
	if err := s.queries.MarkTelegramGroupMemberLeft(ctx, db.MarkTelegramGroupMemberLeftParams{
		ChatID:         chatID,
		TelegramUserID: userID,
		LastStatus:     strings.TrimSpace(status),
		LastSeenAt:     nowTimestamptz(),
	}); err != nil {
		s.log.Debug("failed to mark known telegram group member left", "error_type", safeTelegramErrorType(err))
	}

	// Ensure the member is recorded even if there was no prior row for mark-left update.
	s.upsertKnownGroupMember(ctx, chatID, userID, false, status, false)
}

func (s *telegramService) runGroupMemberTagsWithRollback(ctx context.Context, b *gotgbot.Bot, ownerTelegramUserID, chatID int64, mode fsm.MemberTagRunMode) (fsm.MemberTagRunResult, []fsm.MemberTagRollbackEntry, error) {
	result := fsm.MemberTagRunResult{}
	rollbackByUser := map[int64]string{}
	recordRollback := func(userID int64, previousTag string) {
		if _, exists := rollbackByUser[userID]; exists {
			return
		}
		rollbackByUser[userID] = previousTag
	}

	if s == nil || s.queries == nil || s.userSvc == nil || b == nil {
		return result, nil, errors.New("member tags dependencies are not ready")
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return result, nil, fmt.Errorf("failed to load group: %w", err)
	}
	if group.OwnerTelegramUserID != ownerTelegramUserID || !group.IsActive || !group.IsInitialized {
		return result, nil, errors.New("group access denied")
	}

	knownMembers, err := s.queries.ListTelegramGroupKnownMembers(ctx, chatID)
	if err != nil {
		return result, nil, fmt.Errorf("failed to list known members: %w", err)
	}
	if len(knownMembers) == 0 {
		return result, nil, nil
	}

	botMember, err := s.getRawChatMember(ctx, b, chatID, b.Id)
	if err != nil {
		return result, nil, fmt.Errorf("failed to verify bot rights: %w", err)
	}
	if !canEditMemberTags(botMember) {
		result.SkippedNoRights = len(knownMembers)
		return result, nil, nil
	}

	tagFormat := normalizeMemberTagFormat(group.MemberTagFormat)
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
			continue
		}
		if !isRegularMemberForTag(memberState) {
			result.SkippedNotMember++
			continue
		}

		previousTag := memberState.Tag
		currentTag := strings.TrimSpace(previousTag)
		if mode == fsm.MemberTagRunModeKeepExisting && currentTag != "" {
			result.SkippedExisting++
			continue
		}

		if mode == fsm.MemberTagRunModeClearAndApply && currentTag != "" {
			if err := s.setChatMemberTag(ctx, b, chatID, known.TelegramUserID, ""); err != nil {
				result.Errors++
				continue
			}
			recordRollback(known.TelegramUserID, previousTag)
			currentTag = ""
		}

		profile, _, suppressed := s.resolveMemberTagProfile(ctx, known.TelegramUserID)
		if profile == nil || suppressed {
			result.SkippedUnregistered++
			continue
		}

		tag := buildMemberTag(profile, tagFormat)
		if tag == "" {
			result.SkippedUnregistered++
			continue
		}
		if currentTag == tag {
			result.SkippedExisting++
			continue
		}

		if err := s.setChatMemberTag(ctx, b, chatID, known.TelegramUserID, tag); err != nil {
			result.Errors++
			continue
		}
		s.recordManagedMemberTag(ctx, chatID, known.TelegramUserID, tag)

		recordRollback(known.TelegramUserID, previousTag)
		result.Updated++
	}

	return result, mapToSortedRollbackEntries(rollbackByUser), nil
}

func (s *telegramService) rollbackGroupMemberTags(ctx context.Context, b *gotgbot.Bot, ownerTelegramUserID, chatID int64, entries []fsm.MemberTagRollbackEntry) (fsm.MemberTagRollbackResult, error) {
	result := fsm.MemberTagRollbackResult{}
	if len(entries) == 0 {
		return result, nil
	}
	if s == nil || s.queries == nil || b == nil {
		return result, errors.New("member tags dependencies are not ready")
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return result, fmt.Errorf("failed to load group: %w", err)
	}
	if group.OwnerTelegramUserID != ownerTelegramUserID || !group.IsActive || !group.IsInitialized {
		return result, errors.New("group access denied")
	}

	botMember, err := s.getRawChatMember(ctx, b, chatID, b.Id)
	if err != nil {
		return result, fmt.Errorf("failed to verify bot rights: %w", err)
	}
	if !canEditMemberTags(botMember) {
		result.SkippedNoRights = len(entries)
		return result, nil
	}

	for _, entry := range entries {
		if entry.TelegramUserID <= 0 {
			continue
		}

		memberState, err := s.getRawChatMember(ctx, b, chatID, entry.TelegramUserID)
		if err != nil {
			result.Errors++
			continue
		}
		if !isRawMemberActive(memberState) {
			s.markKnownGroupMemberLeft(ctx, chatID, entry.TelegramUserID, memberState.Status)
			result.SkippedNotMember++
			continue
		}
		if !isRegularMemberForTag(memberState) {
			result.SkippedNotMember++
			continue
		}

		if memberState.Tag == entry.PreviousTag {
			result.Restored++
			continue
		}

		if err := s.setChatMemberTag(ctx, b, chatID, entry.TelegramUserID, entry.PreviousTag); err != nil {
			result.Errors++
			continue
		}
		result.Restored++
	}

	return result, nil
}

func (s *telegramService) syncMemberTagsForRegisteredUser(ctx context.Context, b *gotgbot.Bot, telegramUserID int64) error {
	if s == nil || s.queries == nil || s.userSvc == nil || b == nil || telegramUserID == 0 {
		return nil
	}

	profile, err := s.userSvc.GetProfileByTelegramID(ctx, telegramUserID)
	if err != nil {
		return nil
	}
	suppressed := s.isMemberTagSuppressed(ctx, telegramUserID, profile.Login)
	if suppressed {
		return nil
	}

	groups, err := s.queries.ListMemberTagGroupsByTelegramUser(ctx, telegramUserID)
	if err != nil {
		return fmt.Errorf("failed to list groups by user: %w", err)
	}

	for _, group := range groups {
		botMember, err := s.getRawChatMember(ctx, b, group.ChatID, b.Id)
		if err != nil {
			continue
		}
		if !canEditMemberTags(botMember) {
			continue
		}

		targetMember, err := s.getRawChatMember(ctx, b, group.ChatID, telegramUserID)
		if err != nil {
			continue
		}
		if !isRawMemberActive(targetMember) {
			s.markKnownGroupMemberLeft(ctx, group.ChatID, telegramUserID, targetMember.Status)
			continue
		}
		if !isRegularMemberForTag(targetMember) {
			continue
		}
		if strings.TrimSpace(targetMember.Tag) != "" {
			continue
		}

		tag := buildMemberTag(profile, normalizeMemberTagFormat(group.MemberTagFormat))
		if tag == "" {
			continue
		}
		if err := s.setChatMemberTag(ctx, b, group.ChatID, telegramUserID, tag); err != nil {
			continue
		}
		s.recordManagedMemberTag(ctx, group.ChatID, telegramUserID, tag)
	}

	return nil
}

func (s *telegramService) tryAutoAssignMemberTag(ctx context.Context, b *gotgbot.Bot, chatID, telegramUserID int64) {
	if s == nil || s.queries == nil || s.userSvc == nil || b == nil || chatID == 0 || telegramUserID == 0 {
		return
	}

	group, err := s.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil {
		return
	}
	s.tryAutoAssignMemberTagForKnownGroup(ctx, b, group, telegramUserID)
}

func (s *telegramService) tryAutoAssignMemberTagForKnownGroup(ctx context.Context, b *gotgbot.Bot, group db.TelegramGroup, telegramUserID int64) {
	if s == nil || s.queries == nil || s.userSvc == nil || b == nil || group.ChatID == 0 || telegramUserID == 0 {
		return
	}
	if !group.IsActive || !group.IsInitialized {
		return
	}
	botMember, err := s.getRawChatMember(ctx, b, group.ChatID, b.Id)
	if err != nil || !canEditMemberTags(botMember) {
		return
	}

	targetMember, err := s.getRawChatMember(ctx, b, group.ChatID, telegramUserID)
	if err != nil {
		return
	}
	if !isRawMemberActive(targetMember) {
		s.markKnownGroupMemberLeft(ctx, group.ChatID, telegramUserID, targetMember.Status)
		return
	}
	if !isRegularMemberForTag(targetMember) {
		return
	}
	if strings.TrimSpace(targetMember.Tag) != "" {
		return
	}
	var profile *service.UserProfile
	var imported, suppressed bool
	if group.MemberTagsEnabled {
		profile, imported, suppressed = s.resolveMemberTagProfile(ctx, telegramUserID)
	} else {
		profile, suppressed = s.resolveImportedMemberTagProfile(ctx, telegramUserID)
		imported = profile != nil
	}
	if profile == nil || suppressed || (!group.MemberTagsEnabled && !imported) {
		return
	}

	tag := buildMemberTag(profile, normalizeMemberTagFormat(group.MemberTagFormat))
	if tag == "" {
		return
	}

	if err := s.setChatMemberTag(ctx, b, group.ChatID, telegramUserID, tag); err != nil {
		s.log.Debug("failed to auto-assign member tag", "error_type", safeTelegramErrorType(err))
		return
	}
	s.recordManagedMemberTag(ctx, group.ChatID, telegramUserID, tag)
}

func (s *telegramService) recordManagedMemberTag(ctx context.Context, chatID, telegramUserID int64, tag string) {
	if s == nil || s.queries == nil || chatID == 0 || telegramUserID == 0 || tag == "" {
		return
	}
	queries, ok := s.queries.(*db.Queries)
	if !ok {
		return
	}
	_ = queries.EnqueueLegacyMemberTag(ctx, db.EnqueueLegacyMemberTagParams{ChatID: chatID, TelegramUserID: telegramUserID})
	_ = queries.MarkLegacyMemberTagApplied(ctx, db.MarkLegacyMemberTagAppliedParams{
		ChatID: chatID, TelegramUserID: telegramUserID, LastAppliedTag: tag,
	})
}

func (s *telegramService) resolveMemberTagProfile(ctx context.Context, telegramUserID int64) (*service.UserProfile, bool, bool) {
	if s == nil || s.queries == nil || telegramUserID <= 0 {
		return nil, false, false
	}
	if s.userSvc != nil {
		if profile, err := s.userSvc.GetProfileByTelegramID(ctx, telegramUserID); err == nil && profile != nil {
			return profile, false, s.isMemberTagSuppressed(ctx, telegramUserID, profile.Login)
		}
	}
	profile, suppressed := s.resolveImportedMemberTagProfile(ctx, telegramUserID)
	return profile, profile != nil, suppressed
}

func (s *telegramService) resolveImportedMemberTagProfile(ctx context.Context, telegramUserID int64) (*service.UserProfile, bool) {
	queries, ok := s.queries.(*db.Queries)
	if !ok {
		return nil, false
	}
	mapping, err := queries.GetLegacyMemberTagMapping(ctx, telegramUserID)
	if err != nil {
		return nil, false
	}
	status := db.EnumStudentStatusEXPELLED
	if mapping.ActiveSnapshot {
		status = db.EnumStudentStatusACTIVE
	}
	profile := &service.UserProfile{Login: mapping.S21Login, Status: status}
	if stats, statsErr := queries.GetParticipantStatsCache(ctx, mapping.S21Login); statsErr == nil {
		profile.Status = stats.Status
		profile.Level = stats.Level
		profile.Exp = stats.ExpValue
		profile.PRP = stats.Prp
		profile.CRP = stats.Crp
		profile.Coins = stats.Coins
		if shortName, ok := stats.CampusShortNameEn.(string); ok {
			profile.CampusShortNameEn = shortName
		}
		if shortName, ok := stats.CampusShortNameRu.(string); ok {
			profile.CampusShortNameRu = shortName
		}
		if campusName, ok := stats.CampusName.(string); ok {
			profile.CampusName = campusName
		}
		if stats.CoalitionName.Valid {
			profile.CoalitionName = stats.CoalitionName.String
		}
	}
	return profile, s.isMemberTagSuppressed(ctx, telegramUserID, mapping.S21Login)
}

func (s *telegramService) isMemberTagSuppressed(ctx context.Context, telegramUserID int64, login string) bool {
	queries, ok := s.queries.(*db.Queries)
	if !ok {
		return false
	}
	suppressed, _ := queries.IsLegacyMemberTagSuppressed(ctx, db.IsLegacyMemberTagSuppressedParams{
		TelegramUserID: pgtype.Int8{Int64: telegramUserID, Valid: true}, Lower: strings.ToLower(login),
	})
	return suppressed
}

func (s *telegramService) getRawChatMember(ctx context.Context, b *gotgbot.Bot, chatID, userID int64) (rawChatMember, error) {
	if b == nil {
		return rawChatMember{}, errors.New("bot is nil")
	}

	resp, err := b.RequestWithContext(ctx, "getChatMember", map[string]any{
		"chat_id": chatID,
		"user_id": userID,
	}, nil)
	if err != nil {
		return rawChatMember{}, err
	}

	var member rawChatMember
	if err := json.Unmarshal(resp, &member); err != nil {
		return rawChatMember{}, fmt.Errorf("failed to decode getChatMember response: %w", err)
	}
	if member.User.ID == 0 {
		member.User.ID = userID
	}
	return member, nil
}

func (s *telegramService) setChatMemberTag(ctx context.Context, b *gotgbot.Bot, chatID, userID int64, tag string) error {
	if b == nil {
		return errors.New("bot is nil")
	}

	resp, err := b.RequestWithContext(ctx, "setChatMemberTag", map[string]any{
		"chat_id": chatID,
		"user_id": userID,
		"tag":     tag,
	}, nil)
	if err != nil {
		return err
	}

	var ok bool
	if err := json.Unmarshal(resp, &ok); err != nil {
		return fmt.Errorf("failed to decode setChatMemberTag response: %w", err)
	}
	if !ok {
		return errors.New("setChatMemberTag returned false")
	}
	return nil
}

func canEditMemberTags(member rawChatMember) bool {
	switch strings.TrimSpace(member.Status) {
	case gotgbot.ChatMemberStatusOwner:
		return true
	case gotgbot.ChatMemberStatusAdministrator:
		return member.CanManageTags || member.CanEditTag
	default:
		return false
	}
}

func isRawMemberActive(member rawChatMember) bool {
	switch strings.TrimSpace(member.Status) {
	case gotgbot.ChatMemberStatusOwner, gotgbot.ChatMemberStatusAdministrator, gotgbot.ChatMemberStatusMember:
		return true
	case gotgbot.ChatMemberStatusRestricted:
		return member.IsMember
	default:
		return false
	}
}

func isRegularMemberForTag(member rawChatMember) bool {
	switch strings.TrimSpace(member.Status) {
	case gotgbot.ChatMemberStatusMember:
		return true
	case gotgbot.ChatMemberStatusRestricted:
		return member.IsMember
	default:
		return false
	}
}

func isChatMemberActive(member gotgbot.ChatMember) bool {
	if member == nil {
		return false
	}
	switch member.GetStatus() {
	case gotgbot.ChatMemberStatusOwner, gotgbot.ChatMemberStatusAdministrator, gotgbot.ChatMemberStatusMember:
		return true
	case gotgbot.ChatMemberStatusRestricted:
		switch typed := member.(type) {
		case gotgbot.ChatMemberRestricted:
			return typed.IsMember
		case *gotgbot.ChatMemberRestricted:
			return typed.IsMember
		default:
			return true
		}
	default:
		return false
	}
}

func normalizeMemberTagFormat(format string) string {
	switch strings.TrimSpace(format) {
	case memberTagFormatLoginLevel, memberTagFormatLoginCampusEn, memberTagFormatLoginCampusRu:
		return strings.TrimSpace(format)
	}
	return memberTagFormatLogin
}

func buildMemberTag(profile *service.UserProfile, format string) string {
	if profile == nil {
		return ""
	}
	login := strings.TrimSpace(profile.Login)
	if login == "" {
		return ""
	}
	if profile.Status != db.EnumStudentStatusACTIVE {
		login += "~"
	}
	if runeCount(login) > memberTagMaxRunes {
		return trimRunes(login, memberTagMaxRunes)
	}

	suffix := ""
	switch normalizeMemberTagFormat(format) {
	case memberTagFormatLoginLevel:
		suffix = fmt.Sprintf(" [%d]", profile.Level)
	case memberTagFormatLoginCampusEn:
		if shortName := strings.TrimSpace(profile.CampusShortNameEn); shortName != "" {
			suffix = " " + shortName
		}
	case memberTagFormatLoginCampusRu:
		if shortName := strings.TrimSpace(profile.CampusShortNameRu); shortName != "" {
			suffix = " " + shortName
		}
	}

	if runeCount(login+suffix) > memberTagMaxRunes {
		suffix = ""
	}
	return login + suffix
}

func trimRunes(value string, limit int) string {
	if limit <= 0 || value == "" {
		return ""
	}
	r := []rune(value)
	if len(r) <= limit {
		return value
	}
	return string(r[:limit])
}

func runeCount(value string) int {
	return len([]rune(value))
}

func mapToSortedRollbackEntries(values map[int64]string) []fsm.MemberTagRollbackEntry {
	if len(values) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	entries := make([]fsm.MemberTagRollbackEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, fsm.MemberTagRollbackEntry{
			TelegramUserID: id,
			PreviousTag:    values[id],
		})
	}
	return entries
}

func nowTimestamptz() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}
