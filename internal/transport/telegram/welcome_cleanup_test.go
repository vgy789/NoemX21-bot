package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

type fakeWelcomeDeletionStore struct {
	due     []db.ListDueTelegramGroupWelcomeMessagesRow
	deleted []db.DeleteTelegramGroupWelcomeMessageParams
	retried []db.RetryTelegramGroupWelcomeMessageDeletionParams
}

func (f *fakeWelcomeDeletionStore) ListDueTelegramGroupWelcomeMessages(context.Context, int32) ([]db.ListDueTelegramGroupWelcomeMessagesRow, error) {
	return f.due, nil
}

func (f *fakeWelcomeDeletionStore) DeleteTelegramGroupWelcomeMessage(_ context.Context, arg db.DeleteTelegramGroupWelcomeMessageParams) error {
	f.deleted = append(f.deleted, arg)
	return nil
}

func (f *fakeWelcomeDeletionStore) RetryTelegramGroupWelcomeMessageDeletion(_ context.Context, arg db.RetryTelegramGroupWelcomeMessageDeletionParams) error {
	f.retried = append(f.retried, arg)
	return nil
}

type cleanupSender struct {
	recordingSender
	deleteResult bool
	deleteErr    error
	chatIDs      []int64
}

func (s *cleanupSender) SendMessage(chatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
	s.chatIDs = append(s.chatIDs, chatID)
	return s.recordingSender.SendMessage(chatID, text, opts)
}

func (s *cleanupSender) DeleteMessage(_ int64, messageID int64) (bool, error) {
	s.deleted = append(s.deleted, messageID)
	return s.deleteResult, s.deleteErr
}

func TestRunWelcomeCleanupBatch_DeletesDueMessage(t *testing.T) {
	store := &fakeWelcomeDeletionStore{due: []db.ListDueTelegramGroupWelcomeMessagesRow{{ChatID: -1001, MessageID: 44}}}
	sender := &cleanupSender{deleteResult: true}
	svc := &telegramService{}

	svc.runWelcomeCleanupBatch(context.Background(), store, sender)

	require.Equal(t, []int64{44}, sender.deleted)
	require.Equal(t, []db.DeleteTelegramGroupWelcomeMessageParams{{ChatID: -1001, MessageID: 44}}, store.deleted)
	assert.Empty(t, store.retried)
}

func TestRunWelcomeCleanupBatch_AcknowledgesAlreadyAbsentMessage(t *testing.T) {
	store := &fakeWelcomeDeletionStore{due: []db.ListDueTelegramGroupWelcomeMessagesRow{{ChatID: -1001, MessageID: 45}}}
	sender := &cleanupSender{deleteErr: errors.New("Bad Request: message to delete not found")}

	(&telegramService{}).runWelcomeCleanupBatch(context.Background(), store, sender)

	require.Len(t, store.deleted, 1)
	assert.Empty(t, store.retried)
}

func TestRunWelcomeCleanupBatch_RetriesAndNotifiesOwner(t *testing.T) {
	store := &fakeWelcomeDeletionStore{due: []db.ListDueTelegramGroupWelcomeMessagesRow{{
		ChatID: -1001, MessageID: 46, OwnerTelegramUserID: 7001, AttemptCount: 0,
	}}}
	sender := &cleanupSender{deleteErr: errors.New("synthetic telegram failure")}

	(&telegramService{}).runWelcomeCleanupBatch(context.Background(), store, sender)

	require.Equal(t, []db.RetryTelegramGroupWelcomeMessageDeletionParams{{ChatID: -1001, MessageID: 46}}, store.retried)
	require.Equal(t, []int64{7001}, sender.chatIDs)
	require.Len(t, sender.texts, 1)
	assert.NotContains(t, sender.texts[0], "-1001")
	assert.NotContains(t, sender.texts[0], "46")
}
