package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/service"
	"go.uber.org/mock/gomock"
)

func TestWebhookHandler_Register(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQuerier := mock.NewMockQuerier(ctrl)
	apiKeySvc := service.NewApiKeyService(mockQuerier)
	log := slog.Default()
	handler := NewWebhookHandler(apiKeySvc, mockQuerier, log)

	t.Run("method_not_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/register", nil)
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("missing_secret", func(t *testing.T) {
		body := RegisterRequest{ExternalID: "123", Login: "test"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/register", bytes.NewReader(jsonBody))
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid_secret", func(t *testing.T) {
		body := RegisterRequest{ExternalID: "123", Login: "test"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/register", bytes.NewReader(jsonBody))
		req.Header.Set("X-Secret", "invalid_key")
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid_secret_user_registered", func(t *testing.T) {
		validKey := "noemx_sk_validkey123"

		// Mock API key validation
		mockQuerier.EXPECT().
			GetApiKeyByHash(gomock.Any(), gomock.Any()).
			Return(db.ApiKey{
				ID:            1,
				UserAccountID: 999,
			}, nil)

		// Mock user lookup
		mockQuerier.EXPECT().
			GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: "123456",
			}).
			Return(db.UserAccount{
				ID:       1,
				S21Login: "testuser",
			}, nil)

		body := RegisterRequest{ExternalID: "123456", Login: "testuser"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/register", bytes.NewReader(jsonBody))
		req.Header.Set("X-Secret", validKey)
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp RegisterResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.True(t, resp.Registered)
	})

	t.Run("valid_secret_user_not_registered", func(t *testing.T) {
		validKey := "noemx_sk_validkey123"

		// Mock API key validation
		mockQuerier.EXPECT().
			GetApiKeyByHash(gomock.Any(), gomock.Any()).
			Return(db.ApiKey{
				ID:            1,
				UserAccountID: 999,
			}, nil)

		// Mock user lookup - different login
		mockQuerier.EXPECT().
			GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: "123456",
			}).
			Return(db.UserAccount{
				ID:       1,
				S21Login: "differentuser",
			}, nil)

		body := RegisterRequest{ExternalID: "123456", Login: "testuser"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/register", bytes.NewReader(jsonBody))
		req.Header.Set("X-Secret", validKey)
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp RegisterResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.False(t, resp.Registered)
	})

	t.Run("valid_secret_user_not_found", func(t *testing.T) {
		validKey := "noemx_sk_validkey123"

		// Mock API key validation
		mockQuerier.EXPECT().
			GetApiKeyByHash(gomock.Any(), gomock.Any()).
			Return(db.ApiKey{
				ID:            1,
				UserAccountID: 999,
			}, nil)

		// Mock user lookup - not found
		mockQuerier.EXPECT().
			GetUserAccountByExternalId(gomock.Any(), gomock.Any()).
			Return(db.UserAccount{}, &noRowsError{})

		body := RegisterRequest{ExternalID: "999999", Login: "testuser"}
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/register", bytes.NewReader(jsonBody))
		req.Header.Set("X-Secret", validKey)
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp RegisterResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.False(t, resp.Registered)
	})

	t.Run("bad_request_body", func(t *testing.T) {
		validKey := "noemx_sk_validkey123"

		// Mock API key validation
		mockQuerier.EXPECT().
			GetApiKeyByHash(gomock.Any(), gomock.Any()).
			Return(db.ApiKey{
				ID:            1,
				UserAccountID: 999,
			}, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/register", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("X-Secret", validKey)
		w := httptest.NewRecorder()

		handler.Register(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// noRowsError simulates a "no rows" database error
type noRowsError struct{}

func (e *noRowsError) Error() string {
	return "no rows in result set"
}

func (e *noRowsError) Unwrap() error {
	return context.DeadlineExceeded // Just to satisfy error interface
}
