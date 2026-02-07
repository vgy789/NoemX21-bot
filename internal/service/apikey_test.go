package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

func TestApiKeyService_GenerateApiKey(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	svc := NewApiKeyService(mockRepo)
	ctx := context.Background()
	userAccountID := int64(123)

	t.Run("success", func(t *testing.T) {
		// Expect revoke old keys
		mockRepo.EXPECT().
			RevokeOldApiKeys(ctx, userAccountID).
			Return(nil)

		// Expect create new key
		mockRepo.EXPECT().
			CreateApiKey(ctx, gomock.Any()).
			DoAndReturn(func(ctx context.Context, params db.CreateApiKeyParams) (db.ApiKey, error) {
				assert.Equal(t, userAccountID, params.UserAccountID)
				assert.NotEmpty(t, params.KeyHash)
				assert.NotEmpty(t, params.Prefix)
				assert.True(t, strings.HasPrefix(params.Prefix, "noemx_sk_"))

				return db.ApiKey{
					ID:            1,
					UserAccountID: userAccountID,
					KeyHash:       params.KeyHash,
					Prefix:        params.Prefix,
					CreatedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
				}, nil
			})

		rawKey, err := svc.GenerateApiKey(ctx, userAccountID)
		assert.NoError(t, err)
		assert.NotEmpty(t, rawKey)
		assert.True(t, strings.HasPrefix(rawKey, "noemx_sk_"))
		assert.Greater(t, len(rawKey), 20) // Should be long enough
	})

	t.Run("revoke_error", func(t *testing.T) {
		mockRepo.EXPECT().
			RevokeOldApiKeys(ctx, userAccountID).
			Return(assert.AnError)

		rawKey, err := svc.GenerateApiKey(ctx, userAccountID)
		assert.Error(t, err)
		assert.Empty(t, rawKey)
		assert.Contains(t, err.Error(), "failed to revoke old keys")
	})

	t.Run("create_error", func(t *testing.T) {
		mockRepo.EXPECT().RevokeOldApiKeys(ctx, userAccountID).Return(nil)
		mockRepo.EXPECT().CreateApiKey(ctx, gomock.Any()).Return(db.ApiKey{}, assert.AnError)

		rawKey, err := svc.GenerateApiKey(ctx, userAccountID)
		assert.Error(t, err)
		assert.Empty(t, rawKey)
		assert.Contains(t, err.Error(), "failed to create api key")
	})
}

func TestApiKeyService_ValidateApiKey(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	svc := NewApiKeyService(mockRepo)
	ctx := context.Background()

	t.Run("valid_key", func(t *testing.T) {
		rawKey := "noemx_sk_1234567890abcdef"

		mockRepo.EXPECT().
			GetApiKeyByHash(ctx, gomock.Any()).
			Return(db.ApiKey{
				ID:            1,
				UserAccountID: 123,
				KeyHash:       "somehash",
				Prefix:        "noemx_sk_1234",
				CreatedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
			}, nil)

		apiKey, valid, err := svc.ValidateApiKey(ctx, rawKey)
		assert.NoError(t, err)
		assert.True(t, valid)
		assert.NotNil(t, apiKey)
		assert.Equal(t, int64(123), apiKey.UserAccountID)
	})

	t.Run("invalid_prefix", func(t *testing.T) {
		rawKey := "invalid_key"

		apiKey, valid, err := svc.ValidateApiKey(ctx, rawKey)
		assert.NoError(t, err)
		assert.False(t, valid)
		assert.Nil(t, apiKey)
	})

	t.Run("key_not_found", func(t *testing.T) {
		rawKey := "noemx_sk_notfound"

		mockRepo.EXPECT().
			GetApiKeyByHash(ctx, gomock.Any()).
			Return(db.ApiKey{}, assert.AnError)

		apiKey, valid, err := svc.ValidateApiKey(ctx, rawKey)
		assert.Error(t, err)
		assert.False(t, valid)
		assert.Nil(t, apiKey)
	})

	t.Run("key_not_found_no_rows", func(t *testing.T) {
		rawKey := "noemx_sk_notfound"

		mockRepo.EXPECT().
			GetApiKeyByHash(ctx, gomock.Any()).
			Return(db.ApiKey{}, &mockNoRowsError{})

		apiKey, valid, err := svc.ValidateApiKey(ctx, rawKey)
		assert.NoError(t, err)
		assert.False(t, valid)
		assert.Nil(t, apiKey)
	})
}

func TestApiKeyService_GetActiveApiKey(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	svc := NewApiKeyService(mockRepo)
	ctx := context.Background()
	userAccountID := int64(123)

	t.Run("has_active_key", func(t *testing.T) {
		mockRepo.EXPECT().
			GetActiveApiKey(ctx, userAccountID).
			Return(db.ApiKey{
				ID:            1,
				UserAccountID: userAccountID,
				Prefix:        "noemx_sk_abcd",
				CreatedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
			}, nil)

		prefix, err := svc.GetActiveApiKey(ctx, userAccountID)
		assert.NoError(t, err)
		assert.Equal(t, "noemx_sk_abcd...", prefix)
	})

	t.Run("no_active_key", func(t *testing.T) {
		mockRepo.EXPECT().
			GetActiveApiKey(ctx, userAccountID).
			Return(db.ApiKey{}, &mockNoRowsError{})

		prefix, err := svc.GetActiveApiKey(ctx, userAccountID)
		assert.NoError(t, err)
		assert.Empty(t, prefix)
	})

	t.Run("database_error", func(t *testing.T) {
		mockRepo.EXPECT().
			GetActiveApiKey(ctx, userAccountID).
			Return(db.ApiKey{}, assert.AnError)

		prefix, err := svc.GetActiveApiKey(ctx, userAccountID)
		assert.Error(t, err)
		assert.Empty(t, prefix)
	})
}

// mockNoRowsError simulates a "no rows" database error
type mockNoRowsError struct{}

func (e *mockNoRowsError) Error() string {
	return "no rows in result set"
}
