package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

type prrStatusSyncStub struct {
	calls []prrStatusSyncCall
	err   error
}

type prrStatusSyncCall struct {
	reviewRequestID int64
	status          string
}

func (s *prrStatusSyncStub) SyncReviewRequestStatus(_ context.Context, reviewRequestID int64, status string) error {
	s.calls = append(s.calls, prrStatusSyncCall{
		reviewRequestID: reviewRequestID,
		status:          status,
	})
	return s.err
}

func TestNewCampusService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}

	svc := NewCampusService(q, nil, cfg, log, nil)
	require.NotNil(t, svc)
}

func TestCampusService_Start(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}

	svc := NewCampusService(q, nil, cfg, log, nil)
	err := svc.Start()
	assert.NoError(t, err)
	svc.cron.Stop()
}

func TestCampusService_UpdateCampuses_noLogin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{} // SchoolLogin empty

	svc := NewCampusService(q, nil, cfg, log, nil)
	err := svc.UpdateCampuses(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SCHOOL21_USER_LOGIN")
}

func TestCampusService_UpdateCampuses_getCredsFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "school").
		Return(db.PlatformCredential{}, fmt.Errorf("no rows"))

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Need a real CredentialService that uses the mockRepo
	credSvc := NewCredentialService(mockRepo, nil, nil, log)
	svc := NewCampusService(mockRepo, nil, cfg, log, credSvc)

	err := svc.UpdateCampuses(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get credentials")
}

func TestCampusService_UpdateCampuses_decryptFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, err := crypto.NewCrypter(hexKey)
	require.NoError(t, err)
	// Tampered credentials so Decrypt fails
	creds := db.PlatformCredential{
		S21Login:      "school",
		PasswordEnc:   []byte("bad"),
		PasswordNonce: []byte("bad"),
	}

	mockRepo := mock.NewMockQuerier(ctrl)
	mockRepo.EXPECT().
		GetPlatformCredentials(gomock.Any(), "school").
		Return(creds, nil)

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	seeder := NewCredentialService(mockRepo, crypter, nil, log)
	svc := NewCampusService(mockRepo, nil, cfg, log, seeder)

	err = svc.UpdateCampuses(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt")
}

// --- EnsureCampusPresent tests ---

func TestEnsureCampusPresent_EmptyID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)
	// No expectations: should return early

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	if err := EnsureCampusPresent(context.Background(), mockQ, &s21.Client{}, "", logger, ""); err != nil {
		t.Fatalf("expected nil error for empty campus id, got: %v", err)
	}
}

func TestEnsureCampusPresent_CampusExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)
	// If campus exists, GetCampusByID returns without error
	mockQ.EXPECT().GetCampusByID(gomock.Any(), gomock.Any()).Return(db.GetCampusByIDRow{ID: pgtype.UUID{}}, nil)

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use a valid UUID so pgtype.UUID.Scan succeeds and the mock is invoked
	if err := EnsureCampusPresent(context.Background(), mockQ, &s21.Client{}, "token", logger, "ff19a3a7-12f5-4332-9582-624519c3eaea"); err != nil {
		t.Fatalf("expected nil error when campus exists, got: %v", err)
	}
}

func TestEnsureCampusPresent_FetchAndUpsert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)

	// First call: GetCampusByID -> not found
	mockQ.EXPECT().GetCampusByID(gomock.Any(), gomock.Any()).Return(db.GetCampusByIDRow{}, fmt.Errorf("not found"))
	// Expect UpsertCampus to be called when matching campus found in API
	mockQ.EXPECT().UpsertCampus(gomock.Any(), gomock.Any()).Return(db.Campuse{}, nil)

	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := s21.CampusesResponse{
			Campuses: []s21.CampusV1DTO{{ID: "ff19a3a7-12f5-4332-9582-624519c3eaea", ShortName: "TST", FullName: "Test Campus", Timezone: "UTC"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	client := newMockS21Client(mockHandler)

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	if err := EnsureCampusPresent(context.Background(), mockQ, client, "token", logger, "ff19a3a7-12f5-4332-9582-624519c3eaea"); err != nil {
		t.Fatalf("expected nil error when campus fetched and upserted, got: %v", err)
	}
}

func TestEnsureCampusPresent_NotFoundInAPI(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)

	// GetCampusByID returns not found
	mockQ.EXPECT().GetCampusByID(gomock.Any(), gomock.Any()).Return(db.GetCampusByIDRow{}, fmt.Errorf("not found"))

	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := s21.CampusesResponse{
			Campuses: []s21.CampusV1DTO{{ID: "11111111-1111-1111-1111-111111111111", ShortName: "X", FullName: "X", Timezone: "UTC"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	client := newMockS21Client(mockHandler)

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	if err := EnsureCampusPresent(context.Background(), mockQ, client, "token", logger, "ff19a3a7-12f5-4332-9582-624519c3eaea"); err != nil {
		t.Fatalf("expected nil error when campus not found in API, got: %v", err)
	}
}

func TestCampusService_Stop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{}

	svc := NewCampusService(q, nil, cfg, log, nil)
	err := svc.Start()
	assert.NoError(t, err)
	svc.Stop()
	// Should not panic
}

func TestCampusService_CleanupOutdatedReviewRequests_SyncClosedStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, err := crypto.NewCrypter(hexKey)
	require.NoError(t, err)

	tokenEnc, tokenNonce, err := crypter.Encrypt([]byte("cleanup-token"), []byte("school"))
	require.NoError(t, err)

	q.EXPECT().GetPlatformCredentials(gomock.Any(), "school").Return(db.PlatformCredential{
		S21Login:        "school",
		AccessTokenEnc:  tokenEnc,
		AccessNonce:     tokenNonce,
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(2 * time.Hour), Valid: true},
	}, nil)
	q.EXPECT().GetReviewRequestsForCleanup(gomock.Any(), gomock.Any()).Return([]db.GetReviewRequestsForCleanupRow{
		{ID: 901, RequesterS21Login: "alice", ProjectID: 111},
	}, nil)
	q.EXPECT().CloseReviewRequestByID(gomock.Any(), int64(901)).Return(nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := s21.ParticipantProjectsV1DTO{
			Projects: []s21.ParticipantProjectV1DTO{
				{ID: 222, Status: "IN_REVIEWS"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"

	credSvc := NewCredentialService(q, crypter, nil, log)
	svc := NewCampusService(q, newMockS21Client(handler), cfg, log, credSvc)
	syncer := &prrStatusSyncStub{}
	svc.SetPRRStatusBroadcaster(syncer)

	err = svc.CleanupOutdatedReviewRequests(context.Background())
	require.NoError(t, err)
	require.Len(t, syncer.calls, 1)
	assert.Equal(t, int64(901), syncer.calls[0].reviewRequestID)
	assert.Equal(t, "CLOSED", syncer.calls[0].status)
}

func TestCampusService_CleanupOutdatedReviewRequests_ProjectStillInReviews(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	q := mock.NewMockQuerier(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	hexKey := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	crypter, err := crypto.NewCrypter(hexKey)
	require.NoError(t, err)

	tokenEnc, tokenNonce, err := crypter.Encrypt([]byte("cleanup-token"), []byte("school"))
	require.NoError(t, err)

	q.EXPECT().GetPlatformCredentials(gomock.Any(), "school").Return(db.PlatformCredential{
		S21Login:        "school",
		AccessTokenEnc:  tokenEnc,
		AccessNonce:     tokenNonce,
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(2 * time.Hour), Valid: true},
	}, nil)
	q.EXPECT().GetReviewRequestsForCleanup(gomock.Any(), gomock.Any()).Return([]db.GetReviewRequestsForCleanupRow{
		{ID: 902, RequesterS21Login: "bob", ProjectID: 333},
	}, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := s21.ParticipantProjectsV1DTO{
			Projects: []s21.ParticipantProjectV1DTO{
				{ID: 333, Status: "IN_REVIEWS"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"

	credSvc := NewCredentialService(q, crypter, nil, log)
	svc := NewCampusService(q, newMockS21Client(handler), cfg, log, credSvc)
	syncer := &prrStatusSyncStub{}
	svc.SetPRRStatusBroadcaster(syncer)

	err = svc.CleanupOutdatedReviewRequests(context.Background())
	require.NoError(t, err)
	assert.Empty(t, syncer.calls)
}

func TestEnsureCampusPresent_InvalidUUID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	err := EnsureCampusPresent(context.Background(), mockQ, &s21.Client{}, "token", logger, "invalid-uuid")
	assert.Error(t, err)
}

func TestEnsureCampusPresent_NoToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := mock.NewMockQuerier(ctrl)
	mockQ.EXPECT().GetCampusByID(gomock.Any(), gomock.Any()).Return(db.GetCampusByIDRow{}, fmt.Errorf("not found"))

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	err := EnsureCampusPresent(context.Background(), mockQ, &s21.Client{}, "", logger, "ff19a3a7-12f5-4332-9582-624519c3eaea")
	assert.NoError(t, err) // Should return nil with warning log
}

type mockRoundTripper struct {
	handler http.Handler
}

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	m.handler.ServeHTTP(rr, req)
	return rr.Result(), nil
}

func newMockS21Client(handler http.Handler) *s21.Client {
	return s21.NewClientWithHTTPClient("http://example.local", &http.Client{
		Transport: mockRoundTripper{handler: handler},
	})
}
