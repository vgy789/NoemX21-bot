package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/crypto"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbMock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions"
	"github.com/vgy789/noemx21-bot/internal/fsm/setup"
	"github.com/vgy789/noemx21-bot/internal/service"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func prepareRegistrationTest(t *testing.T) (*telegramService, *serviceMock.MockUserService, *dbMock.MockQuerier, *gomock.Controller) {
	return prepareRegistrationTestWithOTP(t, false)
}

func prepareRegistrationTestWithOTP(t *testing.T, useMockOTP bool) (*telegramService, *serviceMock.MockUserService, *dbMock.MockQuerier, *gomock.Controller) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ctrl := gomock.NewController(t)
	mockUserSvc := serviceMock.NewMockUserService(ctrl)
	mockQuerier := dbMock.NewMockQuerier(ctrl)
	mockRCClient := rocketchat.NewClient("", "", "")

	engine := setup.NewFSM(cfg, logger, mockQuerier, mockUserSvc, mockRCClient, nil, nil, "../../../docs/specs/flows", nil)
	ts := NewTelegramService(cfg, logger, mockUserSvc, mockQuerier, nil, engine, nil)

	// Override engine with test settings
	repo := fsm.NewMemoryStateRepository()
	parser := fsm.NewFlowParser("../../../docs/specs/flows", logger)
	ts.engine = fsm.NewEngine(parser, repo, logger, ts.engine.Registry(), ts.engine.Sanitizer())

	// Create OTP provider based on test needs
	var otpProvider service.OTPProvider
	if useMockOTP {
		otpProvider = service.NewMockOTPProvider(logger)
	} else {
		// For tests that need real OTP verification, use mock provider that still requires DB calls
		otpProvider = service.NewMockOTPProvider(logger)
	}

	// Register actions and aliases in test engine
	crypter, _ := crypto.NewCrypter("12345678123456781234567812345678")
	credSvc := service.NewCredentialService(mockQuerier, crypter, nil, logger)
	mockQuerier.EXPECT().GetPlatformCredentials(gomock.Any(), gomock.Any()).Return(db.PlatformCredential{}, fmt.Errorf("not found")).AnyTimes()
	registrar := actions.NewRegistrar(cfg, logger, mockUserSvc, mockQuerier, mockRCClient, nil, credSvc, otpProvider, repo, nil)
	registrar.RegisterAll(ts.engine.Registry(), ts.engine.AddAlias)

	return ts, mockUserSvc, mockQuerier, ctrl
}

func TestRegistration_RegexValidation(t *testing.T) {
	ts, _, _, ctrl := prepareRegistrationTest(t)
	defer ctrl.Finish()
	ctx := context.Background()
	userID := int64(100)

	_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, nil)

	t.Run("invalid regex", func(t *testing.T) {
		render, err := ts.engine.Process(ctx, userID, "abc")
		require.NoError(t, err)
		assert.Contains(t, render.Text, "Непохоже на логин")
	})
}

func TestRegistration_APIErrors(t *testing.T) {
	ts, _, _, ctrl := prepareRegistrationTest(t)
	defer ctrl.Finish()
	ctx := context.Background()
	userID := int64(200)

	t.Run("wrong parallel", func(t *testing.T) {
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, nil)
		ts.engine.Registry().Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", map[string]any{"api_status": 200, "s21_user": map[string]any{"status": "ACTIVE", "parallelName": "Discovery"}}, nil
		})

		render, err := ts.engine.Process(ctx, userID, "discovery")
		require.NoError(t, err)
		require.NotNil(t, render)
		assert.Contains(t, render.Text, "Core Program")
	})

	t.Run("user not found", func(t *testing.T) {
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, nil)
		// Mock valid_school21_user to return 404
		ts.engine.Registry().Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
			return "", map[string]any{"api_status": 404}, nil
		})

		render, err := ts.engine.Process(ctx, userID, "maslenok")
		require.NoError(t, err)
		require.NotNil(t, render)
		assert.Contains(t, render.Text, "Пустое гнездо")
	})
}

func TestRegistration_UniquenessAndOTP(t *testing.T) {
	ts, mockUserSvc, mockQueries, ctrl := prepareRegistrationTest(t)
	defer ctrl.Finish()
	ctx := context.Background()
	userID := int64(300)

	t.Run("login taken - mock accepts any code", func(t *testing.T) {
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateAwaitingOTP, map[string]any{
			"s21_login": "otheruser",
		})

		// MockOTPProvider accepts any 6-digit code without DB calls
		// Setup expectations for account lookup and profile loading
		mockQueries.EXPECT().GetUserAccountByS21Login(gomock.Any(), "otheruser").Return(db.UserAccount{ExternalID: "555"}, nil).AnyTimes()
		mockQueries.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{}, nil).AnyTimes()
		mockQueries.EXPECT().GetMyProfile(gomock.Any(), gomock.Any()).Return(db.GetMyProfileRow{}, nil).AnyTimes()
		mockUserSvc.EXPECT().GetProfileByExternalID(gomock.Any(), gomock.Any(), gomock.Any()).Return(&service.UserProfile{Login: "otheruser"}, nil).AnyTimes()

		// Any 6-digit code should be accepted by MockOTPProvider
		render, err := ts.engine.Process(ctx, userID, "123456")
		require.NoError(t, err)
		// OTP verification succeeds - profile is loaded from existing account
		assert.Contains(t, render.Text, "Этот логин уже используется другим Telegram аккаунтом")
	})

	t.Run("success - mock accepts any code", func(t *testing.T) {
		userID = 301
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateAwaitingOTP, map[string]any{
			"s21_login": "newuser",
		})

		// MockOTPProvider accepts any 6-digit code without DB calls
		mockQueries.EXPECT().GetUserAccountByS21Login(gomock.Any(), "newuser").Return(db.UserAccount{}, fmt.Errorf("not found")).AnyTimes()
		mockQueries.EXPECT().CreateUserAccount(gomock.Any(), gomock.Any()).Return(db.UserAccount{ID: 1}, nil).AnyTimes()
		mockQueries.EXPECT().UpsertUserBotSettings(gomock.Any(), gomock.Any()).Return(db.UserBotSetting{ID: 1}, nil).AnyTimes()
		mockQueries.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{}, nil).AnyTimes()
		mockQueries.EXPECT().GetMyProfile(gomock.Any(), gomock.Any()).Return(db.GetMyProfileRow{}, nil).AnyTimes()
		mockUserSvc.EXPECT().GetProfileByExternalID(gomock.Any(), db.EnumPlatformTelegram, "301").Return(&service.UserProfile{Login: "newuser"}, nil).AnyTimes()

		// Any 6-digit code should be accepted by MockOTPProvider
		render, err := ts.engine.Process(ctx, userID, "654321")
		require.NoError(t, err)
		// After successful registration, user should be in MAIN_MENU
		assert.Contains(t, render.Text, "Главное меню")
	})
}

func TestRegistration_EmailOTPProvisioningFlow(t *testing.T) {
	cfg := &config.Config{}
	cfg.EmailOTP.Enabled = true
	cfg.EmailOTP.SMTPHost = "smtp.test.local"
	cfg.EmailOTP.SMTPPort = 587
	cfg.EmailOTP.SMTPUsername = "bot@test.local"
	cfg.EmailOTP.SMTPPassword = config.Secret("secret")
	cfg.EmailOTP.From = "bot@test.local"

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUserSvc := serviceMock.NewMockUserService(ctrl)
	mockQuerier := dbMock.NewMockQuerier(ctrl)
	mockRCClient := rocketchat.NewClient("", "", "")

	engine := setup.NewFSM(cfg, logger, mockQuerier, mockUserSvc, mockRCClient, nil, nil, "../../../docs/specs/flows", nil)
	ts := NewTelegramService(cfg, logger, mockUserSvc, mockQuerier, nil, engine, nil)

	repo := fsm.NewMemoryStateRepository()
	parser := fsm.NewFlowParser("../../../docs/specs/flows", logger)
	ts.engine = fsm.NewEngine(parser, repo, logger, ts.engine.Registry(), ts.engine.Sanitizer())

	crypter, _ := crypto.NewCrypter("12345678123456781234567812345678")
	credSvc := service.NewCredentialService(mockQuerier, crypter, nil, logger)
	otpSvc := service.NewOTPService(mockQuerier, nil, cfg, logger)
	otpSvc.SetSMTPSender(func(host string, port int, username, password, from, to, msg string, timeout time.Duration) error {
		return nil
	})
	otpProvider := service.NewRealOTPProvider(otpSvc)

	mockQuerier.EXPECT().GetPlatformCredentials(gomock.Any(), gomock.Any()).Return(db.PlatformCredential{}, fmt.Errorf("not found")).AnyTimes()

	registrar := actions.NewRegistrar(cfg, logger, mockUserSvc, mockQuerier, mockRCClient, nil, credSvc, otpProvider, repo, nil)
	registrar.RegisterAll(ts.engine.Registry(), ts.engine.AddAlias)

	ts.engine.Registry().Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"api_status": 200,
			"s21_login":  "newuser",
			"s21_user": map[string]any{
				"status":       "ACTIVE",
				"parallelName": "Core program",
			},
		}, nil
	})

	ctx := context.Background()
	userID := int64(401)

	require.NoError(t, ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, map[string]any{
		"otp_email_enabled": true,
		"otp_email_from":    cfg.EmailOTP.From,
	}))

	gomock.InOrder(
		mockQuerier.EXPECT().
			GetLastAuthVerificationCode(gomock.Any(), gomock.Any()).
			Return(db.AuthVerificationCode{}, fmt.Errorf("no rows in result set")),
		mockQuerier.EXPECT().
			DeleteAllAuthVerificationCodes(gomock.Any(), gomock.Any()).
			Return(nil),
		mockQuerier.EXPECT().
			GetRegisteredUserByS21Login(gomock.Any(), "newuser").
			Return(db.RegisteredUser{}, fmt.Errorf("no rows in result set")),
		mockQuerier.EXPECT().
			UpsertRegisteredUser(gomock.Any(), gomock.Any()).
			Return(db.RegisteredUser{S21Login: "newuser", Timezone: "UTC"}, nil),
		mockQuerier.EXPECT().
			CreateAuthVerificationCode(gomock.Any(), gomock.Any()).
			Return(db.AuthVerificationCode{}, nil),
		mockQuerier.EXPECT().
			GetRegisteredUserByS21Login(gomock.Any(), "newuser").
			Return(db.RegisteredUser{S21Login: "newuser", Timezone: "UTC"}, nil),
	)

	render, err := ts.engine.Process(ctx, userID, "newuser")
	require.NoError(t, err)
	require.NotNil(t, render)
	assert.Contains(t, render.Text, "Выбери способ подтверждения")

	render, err = ts.engine.Process(ctx, userID, "auth_email")
	require.NoError(t, err)
	require.NotNil(t, render)
	assert.Contains(t, render.Text, "Лови код")
	assert.Contains(t, render.Text, "Письмо должно прийти с адреса")

	state, err := ts.engine.Repo().GetState(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, fsm.StateAwaitingOTP, state.CurrentState)
	assert.Equal(t, "newuser", state.Context["s21_login"])
	assert.Equal(t, "email", state.Context["otp_delivery_method"])
}

func TestRegistration_RocketChatOTPDoesNotShowEmailHint(t *testing.T) {
	ts, _, _, ctrl := prepareRegistrationTest(t)
	defer ctrl.Finish()

	ctx := context.Background()
	userID := int64(402)

	ts.engine.Registry().Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"api_status": 200,
			"s21_login":  "newuser",
			"s21_user": map[string]any{
				"status":       "ACTIVE",
				"parallelName": "Core program",
			},
		}, nil
	})
	ts.engine.Registry().Register("find_and_verify_rocket_user", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"rocket_user_found": true,
		}, nil
	})

	require.NoError(t, ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, nil))

	render, err := ts.engine.Process(ctx, userID, "newuser")
	require.NoError(t, err)
	require.NotNil(t, render)
	assert.Contains(t, render.Text, "Выбери способ подтверждения")

	render, err = ts.engine.Process(ctx, userID, "auth_rocketchat")
	require.NoError(t, err)
	require.NotNil(t, render)
	assert.Contains(t, render.Text, "Лови код")
	assert.NotContains(t, render.Text, "Письмо должно прийти с адреса")
	assert.NotContains(t, render.Text, "Спам")

	state, err := ts.engine.Repo().GetState(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, fsm.StateAwaitingOTP, state.CurrentState)
	assert.Equal(t, "rocketchat", state.Context["otp_delivery_method"])
}
