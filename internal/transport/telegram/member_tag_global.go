package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const globalMemberTagBatch = int32(1)

func (r *telegramMemberTagRunner) PreviewGlobalMemberTags(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	groups, candidates, err := r.svc.globalMemberTagScope(ctx, r.bot, ownerID)
	return fsm.GlobalMemberTagRunStatus{EligibleGroups: int32(len(groups)), CandidateProfiles: int32(len(candidates)), TotalItems: int64(len(groups) * len(candidates)), State: "preview"}, err
}

func (r *telegramMemberTagRunner) StartGlobalMemberTags(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	if !r.svc.isConfiguredBotOwner(ctx, ownerID) {
		return fsm.GlobalMemberTagRunStatus{}, errors.New("global member-tag access denied")
	}
	if active, err := r.svc.queries.GetActiveGlobalMemberTagRun(ctx); err == nil {
		return globalRunStatus(active), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	groups, candidates, err := r.svc.globalMemberTagScope(ctx, r.bot, ownerID)
	if err != nil {
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	run, err := r.svc.queries.CreateGlobalMemberTagRun(ctx, db.CreateGlobalMemberTagRunParams{
		OwnerTelegramUserID: ownerID, EligibleGroups: int32(len(groups)), CandidateProfiles: int32(len(candidates)),
	})
	if err != nil {
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	for _, group := range groups {
		if _, err := r.svc.queries.EnqueueGlobalMemberTagRunGroup(ctx, db.EnqueueGlobalMemberTagRunGroupParams{RunID: run.ID, ChatID: group.ChatID}); err != nil {
			_ = r.svc.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "cancelled"})
			return fsm.GlobalMemberTagRunStatus{}, err
		}
	}
	run.TotalItems = int64(len(groups) * len(candidates))
	if err := r.svc.queries.SetGlobalMemberTagRunTotal(ctx, db.SetGlobalMemberTagRunTotalParams{ID: run.ID, TotalItems: run.TotalItems}); err != nil {
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	return globalRunStatus(run), nil
}

func (r *telegramMemberTagRunner) GlobalMemberTagStatus(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	if !r.svc.isConfiguredBotOwner(ctx, ownerID) {
		return fsm.GlobalMemberTagRunStatus{}, errors.New("global member-tag access denied")
	}
	run, err := r.svc.queries.GetLatestGlobalMemberTagRun(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return fsm.GlobalMemberTagRunStatus{State: "idle"}, nil
	}
	return globalRunStatus(run), err
}

func (r *telegramMemberTagRunner) CancelGlobalMemberTags(ctx context.Context, ownerID int64) error {
	if !r.svc.isConfiguredBotOwner(ctx, ownerID) {
		return errors.New("global member-tag access denied")
	}
	run, err := r.svc.queries.GetActiveGlobalMemberTagRun(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = r.svc.queries.RequestCancelGlobalMemberTagRun(ctx, db.RequestCancelGlobalMemberTagRunParams{ID: run.ID, OwnerTelegramUserID: ownerID})
	return err
}

func (s *telegramService) globalMemberTagScope(ctx context.Context, bot *gotgbot.Bot, ownerID int64) ([]db.TelegramGroup, []int64, error) {
	if s == nil || bot == nil || !s.isConfiguredBotOwner(ctx, ownerID) {
		return nil, nil, errors.New("global member-tag access denied")
	}
	lister, ok := s.queries.(activeInitializedTelegramGroupsLister)
	if !ok {
		return nil, nil, errors.New("group listing unavailable")
	}
	all, err := lister.ListActiveInitializedTelegramGroups(ctx)
	if err != nil {
		return nil, nil, err
	}
	groups := make([]db.TelegramGroup, 0, len(all))
	for _, group := range all {
		if !group.MemberTagsEnabled {
			continue
		}
		count, err := bot.GetChatMemberCountWithContext(ctx, group.ChatID, nil)
		if err != nil || count <= 10 {
			continue
		}
		member, err := s.getRawChatMember(ctx, bot, group.ChatID, bot.Id)
		if err != nil || !canEditMemberTags(member) {
			continue
		}
		groups = append(groups, group)
	}
	candidates, err := s.queries.ListGlobalMemberTagCandidateIDs(ctx)
	return groups, candidates, err
}

func (s *telegramService) startGlobalMemberTagWorker(ctx context.Context, bot *gotgbot.Bot) {
	if s == nil || s.queries == nil || bot == nil {
		return
	}
	s.globalMemberTagOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.runGlobalMemberTagStep(ctx, bot)
				}
			}
		}()
	})
}

func (s *telegramService) runGlobalMemberTagStep(ctx context.Context, bot *gotgbot.Bot) {
	run, err := s.queries.GetActiveGlobalMemberTagRun(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = s.queries.DeleteExpiredGlobalMemberTagRuns(ctx)
		return
	}
	if err != nil {
		return
	}
	if run.State == "cancelling" {
		_ = s.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "cancelled"})
		return
	}
	items, err := s.queries.ListDueGlobalMemberTagRunItems(ctx, globalMemberTagBatch)
	if err != nil {
		return
	}
	if len(items) == 0 {
		remaining, countErr := s.queries.CountGlobalMemberTagRunItems(ctx, run.ID)
		if countErr == nil && remaining == 0 {
			_ = s.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "completed"})
			_, _ = s.getSender(bot).SendMessage(run.OwnerTelegramUserID, "Глобальная проставка тегов завершена. Откройте настройки для итогового отчёта.", nil)
		}
		return
	}
	s.processGlobalMemberTagItem(ctx, bot, items[0])
}

type globalItemResult struct{ discovered, verified, updated, preserved, notMember, noRights, failed bool }

func (s *telegramService) processGlobalMemberTagItem(ctx context.Context, bot *gotgbot.Bot, item db.GlobalMemberTagRunItem) {
	finish := func(result globalItemResult) {
		_ = s.queries.CompleteGlobalMemberTagRunItem(ctx, db.CompleteGlobalMemberTagRunItemParams{
			RunID: item.RunID, ChatID: item.ChatID, TelegramUserID: item.TelegramUserID,
			Column4: result.discovered, Column5: result.verified, Column6: result.updated, Column7: result.preserved,
			Column8: result.notMember, Column9: result.noRights, Column10: result.failed,
		})
	}
	retry := func(cause error) {
		if item.AttemptCount >= 4 {
			finish(globalItemResult{failed: true})
			return
		}
		delay := int64(1 << min(item.AttemptCount, 4))
		var telegramErr *gotgbot.TelegramError
		if errors.As(cause, &telegramErr) && telegramErr.ResponseParams != nil && telegramErr.ResponseParams.RetryAfter > delay {
			delay = telegramErr.ResponseParams.RetryAfter
		}
		_ = s.queries.RetryGlobalMemberTagRunItem(ctx, db.RetryGlobalMemberTagRunItemParams{RunID: item.RunID, ChatID: item.ChatID, TelegramUserID: item.TelegramUserID, Column4: delay})
	}
	group, err := s.queries.GetTelegramGroupByChatID(ctx, item.ChatID)
	if err != nil || !group.IsActive || !group.IsInitialized || !group.MemberTagsEnabled {
		finish(globalItemResult{noRights: true})
		return
	}
	member, err := s.getRawChatMember(ctx, bot, item.ChatID, item.TelegramUserID)
	if err != nil {
		retry(err)
		return
	}
	known, _ := s.queries.IsTelegramGroupMemberKnown(ctx, db.IsTelegramGroupMemberKnownParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
	active := isRawMemberActive(member)
	if active || known {
		if _, err := s.queries.UpsertTelegramGroupMember(ctx, db.UpsertTelegramGroupMemberParams{
			ChatID: item.ChatID, TelegramUserID: item.TelegramUserID, IsMember: active, IsBot: member.User.IsBot,
			LastStatus: member.Status, LastSeenAt: nowTimestamptz(),
		}); err != nil {
			retry(err)
			return
		}
	}
	if !active || !isRegularMemberForTag(member) {
		finish(globalItemResult{notMember: true})
		return
	}
	result := globalItemResult{discovered: !known, verified: known}
	profile, _, suppressed := s.resolveMemberTagProfile(ctx, item.TelegramUserID)
	if profile == nil || suppressed {
		finish(result)
		return
	}
	tag := buildMemberTag(profile, normalizeMemberTagFormat(group.MemberTagFormat))
	if tag == "" || member.Tag == tag {
		finish(result)
		return
	}
	if member.Tag != "" {
		managed, queueErr := s.queries.GetLegacyMemberTagQueueItem(ctx, db.GetLegacyMemberTagQueueItemParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		if queueErr != nil || managed.LastAppliedTag != member.Tag {
			result.preserved = true
			finish(result)
			return
		}
	}
	if err := s.setChatMemberTag(ctx, bot, item.ChatID, item.TelegramUserID, tag); err != nil {
		retry(err)
		return
	}
	s.recordManagedMemberTag(ctx, item.ChatID, item.TelegramUserID, tag)
	result.updated = true
	finish(result)
}

func globalRunStatus(run db.GlobalMemberTagRun) fsm.GlobalMemberTagRunStatus {
	return fsm.GlobalMemberTagRunStatus{ID: run.ID, State: run.State, EligibleGroups: run.EligibleGroups, CandidateProfiles: run.CandidateProfiles,
		TotalItems: run.TotalItems, ProcessedItems: run.ProcessedItems, DiscoveredMembers: run.DiscoveredMembers,
		VerifiedMembers: run.VerifiedMembers, UpdatedTags: run.UpdatedTags, PreservedTags: run.PreservedTags,
		NotMembers: run.NotMembers, SkippedNoRights: run.SkippedNoRights, Errors: run.ErrorCount}
}

var _ fsm.GlobalMemberTagRunner = (*telegramMemberTagRunner)(nil)
