package registration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbmock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// fakeUserSvc implements service.UserService for tests
type fakeUserSvc struct{}

func (f *fakeUserSvc) GetProfileByTelegramID(ctx context.Context, tgID int64) (*service.UserProfile, error) {
	return &service.UserProfile{Login: "testlogin"}, nil
}
func (f *fakeUserSvc) GetProfileByExternalID(ctx context.Context, platform db.EnumPlatform, externalID string) (*service.UserProfile, error) {
	return &service.UserProfile{Login: "testlogin"}, nil
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

func newMockRocketChatClient(handler http.Handler) (*rocketchat.Client, func()) {
	server := httptest.NewServer(handler)
	return rocketchat.NewClientWithHTTPClient(server.URL, "token", "bot-user", server.Client()), server.Close
}

func TestLoadUserProfile_LogsUpsertError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)

	// Expect GetUserAccountByExternalId to return a user account with s21 login
	mockQ.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{S21Login: "testlogin"}, nil)

	// Provide valid platform credentials for credential service
	mockQ.EXPECT().GetPlatformCredentials(gomock.Any(), "school").Return(db.PlatformCredential{
		AccessToken:     pgtype.Text{String: "token", Valid: true},
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, nil)

	// Expect UpsertParticipantStatsCache to be called and fail
	mockQ.EXPECT().UpsertParticipantStatsCache(gomock.Any(), gomock.Any()).Return(fmt.Errorf("db upsert failure"))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/participants/testlogin":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"login":"testlogin","className":null,"parallelName":null,"expValue":100,"level":2,"expToNextLevel":0,"campus":{"id":"","shortName":""},"status":"ACTIVE"}`))
		case r.Method == "GET" && r.URL.Path == "/participants/testlogin/points":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"peerReviewPoints":1,"codeReviewPoints":2,"coins":3}`))
		default:
			http.NotFound(w, r)
		}
	})

	s21Client := newMockS21Client(handler)

	// Create a crypter for credential service (key: 32 bytes hex)
	crypter, err := crypto.NewCrypter("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to create crypter: %v", err)
	}

	// Logger capturing output
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	// Credential service uses mockQ to return valid token
	credSvc := service.NewCredentialService(mockQ, crypter, s21Client, logger)

	// Prepare config with school login used by CredentialService
	cfg := &config.Config{}
	cfg.Init.SchoolLogin = "school"

	registry := fsm.NewLogicRegistry()

	// Create mock OTP provider
	otpProvider := service.NewMockOTPProvider(logger)

	// Register actions (this will register load_user_profile which we want to test)
	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, nil, s21Client, credSvc, otpProvider, nil)

	act, ok := registry.Get("load_user_profile")
	if !ok {
		t.Fatalf("load_user_profile action not registered")
	}

	// Call the action with userID=123
	_, _, err = act(context.Background(), 123, map[string]any{})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}

	// Assert log contains our upsert failure message
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("failed to upsert participant stats cache in registration")) {
		t.Fatalf("expected log to contain upsert error message, got logs:\n%s", out)
	}
}

func TestFindAndVerifyRocketUser_TestModeNoOTP_BypassesEmailVerification(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	cfg := &config.Config{TestModeNoOTP: true}
	registry := fsm.NewLogicRegistry()

	rcClient, cleanup := newMockRocketChatClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.info" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"user":{"_id":"rc123","username":"testlogin"}}`))
	}))
	defer cleanup()

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "testlogin").Return(db.UserAccount{}, fmt.Errorf("not found"))
	mockQ.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "testlogin").Return(db.RegisteredUser{
		S21Login:     "testlogin",
		RocketchatID: "rc123",
		Timezone:     "UTC",
	}, nil)

	Register(
		registry,
		cfg,
		logger,
		mockQ,
		&fakeUserSvc{},
		rcClient,
		nil,
		nil,
		service.NewMockOTPProvider(logger),
		nil,
	)

	act, ok := registry.Get("find_and_verify_rocket_user")
	if !ok {
		t.Fatalf("find_and_verify_rocket_user action not registered")
	}

	_, updates, err := act(context.Background(), 42, map[string]any{"login": "testlogin"})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}

	if updates["email_verified"] != true || updates["rocket_user_found"] != true {
		t.Fatalf("expected email_verified=true and rocket_user_found=true, got %#v", updates)
	}
}

func TestFindAndVerifyRocketUser_ProdMode_RequiresVerifiedEmail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	cfg := &config.Config{TestModeNoOTP: false}
	registry := fsm.NewLogicRegistry()

	rcClient, cleanup := newMockRocketChatClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.info" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"user":{"_id":"rc123","username":"testlogin"}}`))
	}))
	defer cleanup()

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "testlogin").Return(db.UserAccount{}, fmt.Errorf("not found"))

	Register(
		registry,
		cfg,
		logger,
		mockQ,
		&fakeUserSvc{},
		rcClient,
		nil,
		nil,
		service.NewMockOTPProvider(logger),
		nil,
	)

	act, ok := registry.Get("find_and_verify_rocket_user")
	if !ok {
		t.Fatalf("find_and_verify_rocket_user action not registered")
	}

	_, updates, err := act(context.Background(), 42, map[string]any{"login": "testlogin"})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}

	if updates["email_mismatch"] != true || updates["rocket_user_found"] != true {
		t.Fatalf("expected email_mismatch=true and rocket_user_found=true, got %#v", updates)
	}
}
