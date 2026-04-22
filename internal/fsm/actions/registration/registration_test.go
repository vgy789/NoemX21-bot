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

	"github.com/jackc/pgx/v5"
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

type captureMemberTagRunner struct {
	called     bool
	lastUserID int64
}

func (r *captureMemberTagRunner) RunGroupMemberTags(_ context.Context, _, _ int64, _ fsm.MemberTagRunMode) (fsm.MemberTagRunResult, error) {
	return fsm.MemberTagRunResult{}, nil
}

func (r *captureMemberTagRunner) SyncMemberTagsForRegisteredUser(_ context.Context, telegramUserID int64) error {
	r.called = true
	r.lastUserID = telegramUserID
	return nil
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

func newMockRocketClient(handler http.Handler) *rocketchat.Client {
	return rocketchat.NewClientWithHTTPClient("http://example.local/api/v1", "token", "user", &http.Client{
		Transport: mockRoundTripper{handler: handler},
	})
}

func TestLoadUserProfile_LogsUpsertError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)

	// Expect GetUserAccountByExternalId to return a user account with s21 login
	mockQ.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{S21Login: "testlogin"}, nil)

	// Create a crypter for credential service (key: 32 bytes hex)
	crypter, err := crypto.NewCrypter("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to create crypter: %v", err)
	}

	encToken, nonce, _ := crypter.Encrypt([]byte("valid-token"), []byte("school"))

	// Provide valid platform credentials for credential service
	mockQ.EXPECT().GetPlatformCredentials(gomock.Any(), "school").Return(db.PlatformCredential{
		S21Login:        "school",
		AccessTokenEnc:  encToken,
		AccessNonce:     nonce,
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

func TestFindAndVerifyRocketUser_SucceedsWithExactUsernameLookup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "roryraqu").Return(db.UserAccount{}, pgx.ErrNoRows)
	mockQ.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "roryraqu").Return(db.RegisteredUser{
		S21Login:     "roryraqu",
		RocketchatID: "rc123",
		Timezone:     "UTC",
	}, nil)

	rcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users.info" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"user":{"_id":"rc123","username":"roryraqu","emails":[]}}`))
			return
		}
		http.NotFound(w, r)
	})
	rcClient := newMockRocketClient(rcHandler)

	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, rcClient, nil, nil, nil, nil)

	action, ok := registry.Get("find_and_verify_rocket_user")
	if !ok {
		t.Fatalf("find_and_verify_rocket_user action not registered")
	}

	_, updates, err := action(context.Background(), 42, map[string]any{"login": "roryraqu"})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}
	if updates["rocket_user_found"] != true {
		t.Fatalf("expected rocket_user_found=true, got: %#v", updates)
	}
}

func TestFindAndVerifyRocketUser_UnexpectedUsernameSetsAPIError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "roryraqu").Return(db.UserAccount{}, pgx.ErrNoRows)

	rcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users.info" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"user":{"_id":"rc123","username":"someoneelse","emails":[]}}`))
			return
		}
		http.NotFound(w, r)
	})
	rcClient := newMockRocketClient(rcHandler)

	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, rcClient, nil, nil, nil, nil)

	action, ok := registry.Get("find_and_verify_rocket_user")
	if !ok {
		t.Fatalf("find_and_verify_rocket_user action not registered")
	}

	_, updates, err := action(context.Background(), 42, map[string]any{"login": "roryraqu"})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}
	if updates["rocket_api_error"] != true {
		t.Fatalf("expected rocket_api_error=true, got: %#v", updates)
	}
}

func TestFindAndVerifyRocketUser_AlreadyRegisteredResetsSuccessFlags(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "jonnabin").Return(db.UserAccount{
		S21Login:   "jonnabin",
		ExternalID: "999999",
	}, nil)

	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, nil, nil, nil, nil, nil)

	action, ok := registry.Get("find_and_verify_rocket_user")
	if !ok {
		t.Fatalf("find_and_verify_rocket_user action not registered")
	}

	_, updates, err := action(context.Background(), 42, map[string]any{"login": "jonnabin"})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}
	if updates["email_already_registered"] != true {
		t.Fatalf("expected email_already_registered=true, got: %#v", updates)
	}
	if updates["rocket_user_found"] != false {
		t.Fatalf("expected stale success flags to be reset, got: %#v", updates)
	}
}

func TestVerifyRocketChatToken_SuccessSingleProfileRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	var profileRequests int
	rcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/me" {
			profileRequests++
			if r.Header.Get("X-User-Id") != "rc123" || r.Header.Get("X-Auth-Token") != "token1234567890" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"_id":"rc123","username":"roryraqu","emails":[{"address":"roryraqu@student.21-school.ru","verified":true}]}`))
			return
		}
		http.NotFound(w, r)
	})
	rcClient := newMockRocketClient(rcHandler)

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "roryraqu").Return(db.UserAccount{}, pgx.ErrNoRows)
	mockQ.EXPECT().GetRegisteredUserByS21Login(gomock.Any(), "roryraqu").Return(db.RegisteredUser{}, pgx.ErrNoRows)
	mockQ.EXPECT().UpsertRegisteredUser(gomock.Any(), gomock.Any()).Return(db.RegisteredUser{
		S21Login:     "roryraqu",
		RocketchatID: "rc123",
		Timezone:     "UTC",
	}, nil)
	mockQ.EXPECT().CreateUserAccount(gomock.Any(), gomock.Any()).Return(db.UserAccount{ID: 1}, nil)
	mockQ.EXPECT().UpsertUserBotSettings(gomock.Any(), gomock.Any()).Return(db.UserBotSetting{ID: 1}, nil)

	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, rcClient, nil, nil, nil, nil)

	action, ok := registry.Get("verify_rocketchat_token")
	if !ok {
		t.Fatalf("verify_rocketchat_token action not registered")
	}

	_, updates, err := action(context.Background(), 42, map[string]any{
		"login":             "roryraqu",
		"rocket_user_id":    "rc123",
		"rocket_auth_token": "token1234567890",
	})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}
	if updates["rocket_token_verified"] != true {
		t.Fatalf("expected rocket_token_verified=true, got: %#v", updates)
	}
	if profileRequests != 1 {
		t.Fatalf("expected exactly 1 profile request, got %d", profileRequests)
	}
}

func TestVerifyRocketChatToken_InvalidToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	var profileRequests int
	rcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/me" {
			profileRequests++
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false}`))
			return
		}
		http.NotFound(w, r)
	})
	rcClient := newMockRocketClient(rcHandler)

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "roryraqu").Return(db.UserAccount{}, pgx.ErrNoRows)

	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, rcClient, nil, nil, nil, nil)

	action, ok := registry.Get("verify_rocketchat_token")
	if !ok {
		t.Fatalf("verify_rocketchat_token action not registered")
	}

	_, updates, err := action(context.Background(), 42, map[string]any{
		"login":             "roryraqu",
		"rocket_user_id":    "rc123",
		"rocket_auth_token": "bad-token",
	})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}
	if updates["rocket_token_invalid"] != true {
		t.Fatalf("expected rocket_token_invalid=true, got: %#v", updates)
	}
	if profileRequests != 1 {
		t.Fatalf("expected exactly 1 profile request, got %d", profileRequests)
	}
}

func TestVerifyRocketChatToken_UsernameMismatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	rcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/me" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"_id":"rc123","username":"anotheruser","emails":[]}`))
			return
		}
		http.NotFound(w, r)
	})
	rcClient := newMockRocketClient(rcHandler)

	mockQ.EXPECT().GetUserAccountByS21Login(gomock.Any(), "roryraqu").Return(db.UserAccount{}, pgx.ErrNoRows)

	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, rcClient, nil, nil, nil, nil)

	action, ok := registry.Get("verify_rocketchat_token")
	if !ok {
		t.Fatalf("verify_rocketchat_token action not registered")
	}

	_, updates, err := action(context.Background(), 42, map[string]any{
		"login":             "roryraqu",
		"rocket_user_id":    "rc123",
		"rocket_auth_token": "token1234567890",
	})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}
	if updates["rocket_token_invalid"] != true {
		t.Fatalf("expected rocket_token_invalid=true, got: %#v", updates)
	}
}

func TestLoadUserProfile_TriggersMemberTagsSyncForTelegram(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQ := dbmock.NewMockQuerier(ctrl)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	registry := fsm.NewLogicRegistry()
	cfg := &config.Config{}

	mockQ.EXPECT().GetUserAccountByExternalId(gomock.Any(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "1234",
	}).Return(db.UserAccount{S21Login: "testlogin"}, nil)

	otpProvider := service.NewMockOTPProvider(logger)
	Register(registry, cfg, logger, mockQ, &fakeUserSvc{}, nil, nil, nil, otpProvider, nil)

	action, ok := registry.Get("load_user_profile")
	if !ok {
		t.Fatalf("load_user_profile action not registered")
	}

	runner := &captureMemberTagRunner{}
	ctx := context.WithValue(context.Background(), fsm.ContextKeyMemberTagRunner, runner)
	_, updates, err := action(ctx, 1234, map[string]any{})
	if err != nil {
		t.Fatalf("action returned error: %v", err)
	}

	if updates["profile_loaded"] != true {
		t.Fatalf("expected profile_loaded=true, got: %#v", updates)
	}
	if !runner.called || runner.lastUserID != 1234 {
		t.Fatalf("expected member tags sync to be called for 1234, got called=%v user=%d", runner.called, runner.lastUserID)
	}
}
