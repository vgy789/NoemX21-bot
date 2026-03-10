package statistics

import (
	"context"
	"errors"
	"testing"

	"github.com/vgy789/noemx21-bot/internal/fsm"
)

type fakeMemberTagRunner struct {
	called     bool
	lastUserID int64
	err        error
}

func (f *fakeMemberTagRunner) RunGroupMemberTags(_ context.Context, _, _ int64, _ fsm.MemberTagRunMode) (fsm.MemberTagRunResult, error) {
	return fsm.MemberTagRunResult{}, nil
}

func (f *fakeMemberTagRunner) SyncMemberTagsForRegisteredUser(_ context.Context, telegramUserID int64) error {
	f.called = true
	f.lastUserID = telegramUserID
	return f.err
}

func TestTriggerMemberTagSyncOnLevelChange(t *testing.T) {
	runner := &fakeMemberTagRunner{}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	triggerMemberTagSyncOnLevelChange(ctx, nil, 12345, 10, 11)

	if !runner.called {
		t.Fatalf("expected runner to be called")
	}
	if runner.lastUserID != 12345 {
		t.Fatalf("expected userID=12345, got %d", runner.lastUserID)
	}
}

func TestTriggerMemberTagSyncOnLevelChange_NoChange(t *testing.T) {
	runner := &fakeMemberTagRunner{}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	triggerMemberTagSyncOnLevelChange(ctx, nil, 12345, 11, 11)

	if runner.called {
		t.Fatalf("runner should not be called when level unchanged")
	}
}

func TestTriggerMemberTagSyncOnLevelChange_RunnerErrorIgnored(t *testing.T) {
	runner := &fakeMemberTagRunner{err: errors.New("boom")}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)

	triggerMemberTagSyncOnLevelChange(ctx, nil, 12345, 10, 12)

	if !runner.called {
		t.Fatalf("expected runner to be called")
	}
}
