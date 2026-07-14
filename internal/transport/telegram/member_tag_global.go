package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/jackc/pgx/v5"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const globalMemberTagBatch = int32(1)

const globalMemberTagItemTimeout = 5 * time.Second

func (r *telegramMemberTagRunner) PreviewGlobalMemberTags(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	return fsm.GlobalMemberTagRunStatus{State: "retired"}, nil
}

func (r *telegramMemberTagRunner) previewGlobalMemberTagsRetiredCode(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	groups, candidates, err := r.svc.globalMemberTagScope(ctx, r.bot, ownerID)
	return fsm.GlobalMemberTagRunStatus{EligibleGroups: int32(len(groups)), CandidateProfiles: int32(len(candidates)), TotalItems: int64(len(groups) * len(candidates)), State: "preview"}, err
}

func (r *telegramMemberTagRunner) StartGlobalMemberTags(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	return fsm.GlobalMemberTagRunStatus{State: "retired"}, nil
}

func (r *telegramMemberTagRunner) startGlobalMemberTagsRetiredCode(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
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
		_ = r.svc.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "cancelled"})
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	if err := r.svc.queries.ActivateGlobalMemberTagRun(ctx, run.ID); err != nil {
		_ = r.svc.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "cancelled"})
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	run.State = "running"
	return globalRunStatus(run), nil
}

func (r *telegramMemberTagRunner) GlobalMemberTagStatus(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	return fsm.GlobalMemberTagRunStatus{State: "retired"}, nil
}

func (r *telegramMemberTagRunner) globalMemberTagStatusRetiredCode(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
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
	return nil
}

func (r *telegramMemberTagRunner) cancelGlobalMemberTagsRetiredCode(ctx context.Context, ownerID int64) error {
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

func (r *telegramMemberTagRunner) StartGroupMemberTagDiscovery(ctx context.Context, ownerID, chatID int64) (fsm.GlobalMemberTagRunStatus, error) {
	return fsm.GlobalMemberTagRunStatus{State: "retired"}, nil
}

func (r *telegramMemberTagRunner) startGroupMemberTagDiscoveryRetiredCode(ctx context.Context, ownerID, chatID int64) (fsm.GlobalMemberTagRunStatus, error) {
	if r == nil || r.svc == nil || r.bot == nil || ownerID <= 0 || chatID == 0 {
		return fsm.GlobalMemberTagRunStatus{}, errors.New("group member-tag dependencies are not ready")
	}
	group, err := r.svc.queries.GetTelegramGroupByChatID(ctx, chatID)
	if err != nil || !group.IsActive || !group.IsInitialized || group.OwnerTelegramUserID != ownerID {
		return fsm.GlobalMemberTagRunStatus{}, errors.New("group member-tag access denied")
	}
	botMember, err := r.svc.getRawChatMember(ctx, r.bot, chatID, r.bot.Id)
	if err != nil || !canEditMemberTags(botMember) {
		return fsm.GlobalMemberTagRunStatus{}, errors.New("bot cannot manage group member tags")
	}
	if active, activeErr := r.svc.queries.GetActiveGlobalMemberTagRun(ctx); activeErr == nil {
		if active.OwnerTelegramUserID == ownerID {
			return globalRunStatus(active), nil
		}
		return fsm.GlobalMemberTagRunStatus{}, errors.New("another member-tag scan is running")
	} else if !errors.Is(activeErr, pgx.ErrNoRows) {
		return fsm.GlobalMemberTagRunStatus{}, activeErr
	}
	candidates, err := r.svc.queries.ListGlobalMemberTagCandidateIDs(ctx)
	if err != nil {
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	run, err := r.svc.queries.CreateGlobalMemberTagRun(ctx, db.CreateGlobalMemberTagRunParams{
		OwnerTelegramUserID: ownerID, EligibleGroups: 1, CandidateProfiles: int32(len(candidates)),
	})
	if err != nil {
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	cancelRun := func() {
		_ = r.svc.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "cancelled"})
	}
	if _, err := r.svc.queries.EnqueueGlobalMemberTagRunGroup(ctx, db.EnqueueGlobalMemberTagRunGroupParams{RunID: run.ID, ChatID: chatID}); err != nil {
		cancelRun()
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	run.TotalItems = int64(len(candidates))
	if err := r.svc.queries.SetGlobalMemberTagRunTotal(ctx, db.SetGlobalMemberTagRunTotalParams{ID: run.ID, TotalItems: run.TotalItems}); err != nil {
		cancelRun()
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	if err := r.svc.queries.ActivateGlobalMemberTagRun(ctx, run.ID); err != nil {
		cancelRun()
		return fsm.GlobalMemberTagRunStatus{}, err
	}
	run.State = "running"
	return globalRunStatus(run), nil
}

func (r *telegramMemberTagRunner) GroupMemberTagDiscoveryStatus(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	return fsm.GlobalMemberTagRunStatus{State: "retired"}, nil
}

func (r *telegramMemberTagRunner) groupMemberTagDiscoveryStatusRetiredCode(ctx context.Context, ownerID int64) (fsm.GlobalMemberTagRunStatus, error) {
	run, err := r.svc.queries.GetLatestGlobalMemberTagRunByOwner(ctx, ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fsm.GlobalMemberTagRunStatus{State: "idle"}, nil
	}
	return globalRunStatus(run), err
}

func (r *telegramMemberTagRunner) CancelGroupMemberTagDiscovery(ctx context.Context, ownerID int64) error {
	return nil
}

func (r *telegramMemberTagRunner) cancelGroupMemberTagDiscoveryRetiredCode(ctx context.Context, ownerID int64) error {
	run, err := r.svc.queries.GetActiveGlobalMemberTagRun(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if run.OwnerTelegramUserID != ownerID {
		return errors.New("group member-tag access denied")
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
	return
}

func (s *telegramService) startGlobalMemberTagWorkerRetiredCode(ctx context.Context, bot *gotgbot.Bot) {
	if s == nil || s.queries == nil || bot == nil {
		return
	}
	s.globalMemberTagOnce.Do(func() {
		if s.log != nil {
			s.log.Info("global member-tag worker started")
		}
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
		if s.log != nil {
			s.log.Warn("global member-tag worker failed to load active run", "error_type", safeTelegramErrorType(err))
		}
		return
	}
	if run.State == "preparing" {
		return
	}
	if run.State == "cancelling" {
		_ = s.queries.FinishGlobalMemberTagRun(ctx, db.FinishGlobalMemberTagRunParams{ID: run.ID, State: "cancelled"})
		return
	}
	items, err := s.queries.ListDueGlobalMemberTagRunItems(ctx, globalMemberTagBatch)
	if err != nil {
		if s.log != nil {
			s.log.Warn("global member-tag worker failed to load work", "error_type", safeTelegramErrorType(err))
		}
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
	completed := s.processGlobalMemberTagItem(ctx, bot, items[0])
	if completed && run.TotalItems > 0 {
		beforePercent := run.ProcessedItems * 100 / run.TotalItems
		afterPercent := (run.ProcessedItems + 1) * 100 / run.TotalItems
		crossedFirstPercent := beforePercent < 1 && afterPercent >= 1
		crossedTenPercent := beforePercent/10 < afterPercent/10
		if (crossedFirstPercent || crossedTenPercent) && afterPercent < 100 {
			_, _ = s.getSender(bot).SendMessage(run.OwnerTelegramUserID,
				fmt.Sprintf("Проставка тегов: %d%% (%d/%d).", afterPercent, run.ProcessedItems+1, run.TotalItems), nil)
		}
	}
}

type globalItemResult struct{ discovered, verified, updated, preserved, notMember, noRights, failed bool }

func (s *telegramService) processGlobalMemberTagItem(ctx context.Context, bot *gotgbot.Bot, item db.GlobalMemberTagRunItem) bool {
	finish := func(result globalItemResult) bool {
		return s.queries.CompleteGlobalMemberTagRunItem(ctx, db.CompleteGlobalMemberTagRunItemParams{
			RunID: item.RunID, ChatID: item.ChatID, TelegramUserID: item.TelegramUserID,
			Column4: result.discovered, Column5: result.verified, Column6: result.updated, Column7: result.preserved,
			Column8: result.notMember, Column9: result.noRights, Column10: result.failed,
		}) == nil
	}
	retry := func(cause error) bool {
		if item.AttemptCount >= 4 {
			return finish(globalItemResult{failed: true})
		}
		delay := int64(1 << min(item.AttemptCount, 4))
		var telegramErr *gotgbot.TelegramError
		if errors.As(cause, &telegramErr) && telegramErr.ResponseParams != nil && telegramErr.ResponseParams.RetryAfter > delay {
			delay = telegramErr.ResponseParams.RetryAfter
		}
		_ = s.queries.RetryGlobalMemberTagRunItem(ctx, db.RetryGlobalMemberTagRunItemParams{RunID: item.RunID, ChatID: item.ChatID, TelegramUserID: item.TelegramUserID, Column4: delay})
		return false
	}
	group, err := s.queries.GetTelegramGroupByChatID(ctx, item.ChatID)
	if err != nil || !group.IsActive || !group.IsInitialized || !group.MemberTagsEnabled {
		return finish(globalItemResult{noRights: true})
	}
	known, _ := s.queries.IsTelegramGroupMemberKnown(ctx, db.IsTelegramGroupMemberKnownParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
	requestCtx, cancelRequest := context.WithTimeout(ctx, globalMemberTagItemTimeout)
	member, err := s.getRawChatMember(requestCtx, bot, item.ChatID, item.TelegramUserID)
	cancelRequest()
	if err != nil {
		if isTelegramMemberAbsentError(err) {
			if known {
				s.markKnownGroupMemberLeft(ctx, item.ChatID, item.TelegramUserID, gotgbot.ChatMemberStatusLeft)
			}
			return finish(globalItemResult{notMember: true})
		}
		if isPermanentGlobalTelegramError(err) {
			return finish(globalItemResult{failed: true})
		}
		return retry(err)
	}
	active := isRawMemberActive(member)
	if active || known {
		if _, err := s.queries.UpsertTelegramGroupMember(ctx, db.UpsertTelegramGroupMemberParams{
			ChatID: item.ChatID, TelegramUserID: item.TelegramUserID, IsMember: active, IsBot: member.User.IsBot,
			LastStatus: member.Status, LastSeenAt: nowTimestamptz(),
		}); err != nil {
			return retry(err)
		}
	}
	if !active || !isRegularMemberForTag(member) {
		return finish(globalItemResult{notMember: true})
	}
	result := globalItemResult{discovered: !known, verified: known}
	profile, _, suppressed := s.resolveMemberTagProfile(ctx, item.TelegramUserID)
	if profile == nil || suppressed {
		return finish(result)
	}
	tag := buildMemberTag(profile, normalizeMemberTagFormat(group.MemberTagFormat))
	if tag == "" || member.Tag == tag {
		return finish(result)
	}
	if member.Tag != "" {
		managed, queueErr := s.queries.GetLegacyMemberTagQueueItem(ctx, db.GetLegacyMemberTagQueueItemParams{ChatID: item.ChatID, TelegramUserID: item.TelegramUserID})
		if queueErr != nil || managed.LastAppliedTag != member.Tag {
			result.preserved = true
			return finish(result)
		}
	}
	requestCtx, cancelRequest = context.WithTimeout(ctx, globalMemberTagItemTimeout)
	err = s.setChatMemberTag(requestCtx, bot, item.ChatID, item.TelegramUserID, tag)
	cancelRequest()
	if err != nil {
		if isPermanentGlobalTelegramError(err) {
			return finish(globalItemResult{failed: true})
		}
		return retry(err)
	}
	s.recordManagedMemberTag(ctx, item.ChatID, item.TelegramUserID, tag)
	result.updated = true
	return finish(result)
}

func isTelegramMemberAbsentError(err error) bool {
	var telegramErr *gotgbot.TelegramError
	if !errors.As(err, &telegramErr) || telegramErr.Code != 400 {
		return false
	}
	description := strings.ToLower(telegramErr.Description)
	return strings.Contains(description, "user not found") ||
		strings.Contains(description, "participant_id_invalid") ||
		strings.Contains(description, "member not found")
}

func isPermanentGlobalTelegramError(err error) bool {
	var telegramErr *gotgbot.TelegramError
	if !errors.As(err, &telegramErr) {
		return false
	}
	return telegramErr.Code != 429 && telegramErr.Code < 500
}

func globalRunStatus(run db.GlobalMemberTagRun) fsm.GlobalMemberTagRunStatus {
	return fsm.GlobalMemberTagRunStatus{ID: run.ID, State: run.State, EligibleGroups: run.EligibleGroups, CandidateProfiles: run.CandidateProfiles,
		TotalItems: run.TotalItems, ProcessedItems: run.ProcessedItems, DiscoveredMembers: run.DiscoveredMembers,
		VerifiedMembers: run.VerifiedMembers, UpdatedTags: run.UpdatedTags, PreservedTags: run.PreservedTags,
		NotMembers: run.NotMembers, SkippedNoRights: run.SkippedNoRights, Errors: run.ErrorCount}
}

var _ fsm.GlobalMemberTagRunner = (*telegramMemberTagRunner)(nil)
