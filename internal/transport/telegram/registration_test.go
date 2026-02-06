package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbMock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions"
	"github.com/vgy789/noemx21-bot/internal/service"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func prepareRegistrationTest(t *testing.T) (*telegramService, *serviceMock.MockStudentService, *dbMock.MockQuerier, *gomock.Controller) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ctrl := gomock.NewController(t)
	mockStudentSvc := serviceMock.NewMockStudentService(ctrl)
	mockQuerier := dbMock.NewMockQuerier(ctrl)
	mockRCClient := rocketchat.NewClient("", "", "")

	service := NewTelegramService(cfg, logger, mockStudentSvc, mockQuerier, mockRCClient, nil)
	ts := service.(*telegramService)

	// Override engine with test settings
	repo := fsm.NewMemoryStateRepository()
	parser := fsm.NewFlowParser("../../../docs/specs/flows", logger)
	ts.engine = fsm.NewEngine(parser, repo, logger, ts.engine.Registry(), ts.engine.Sanitizer())

	// Register actions and aliases in test engine
	registrar := actions.NewRegistrar(cfg, logger, mockStudentSvc, mockQuerier, mockRCClient, nil)
	registrar.RegisterAll(ts.engine.Registry(), ts.engine.AddAlias)

	return ts, mockStudentSvc, mockQuerier, ctrl
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
		assert.Contains(t, render.Text, "Неверный формат")
	})
}

func TestRegistration_APIErrors(t *testing.T) {
	ts, _, _, ctrl := prepareRegistrationTest(t)
	defer ctrl.Finish()
	ctx := context.Background()
	userID := int64(200)

	t.Run("wrong parallel", func(t *testing.T) {
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, nil)
		ts.engine.Registry().Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
			return "", map[string]interface{}{"api_status": 200, "user": map[string]interface{}{"status": "ACTIVE", "parallelName": "Discovery"}}, nil
		})

		render, err := ts.engine.Process(ctx, userID, "discovery")
		require.NoError(t, err)
		require.NotNil(t, render)
		assert.Contains(t, render.Text, "Регистрация доступна только для студентов основы")
	})

	t.Run("user not found", func(t *testing.T) {
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateInputLogin, nil)
		// Mock valid_school21_user to return 404
		ts.engine.Registry().Register("validate_school21_user", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
			return "", map[string]interface{}{"api_status": 404}, nil
		})

		render, err := ts.engine.Process(ctx, userID, "maslenok")
		require.NoError(t, err)
		require.NotNil(t, render)
		assert.Contains(t, render.Text, "Пользователь не найден в School21")
	})
}

func TestRegistration_UniquenessAndOTP(t *testing.T) {
	ts, mockStudentSvc, mockQueries, ctrl := prepareRegistrationTest(t)
	defer ctrl.Finish()
	ctx := context.Background()
	userID := int64(300)

	t.Run("login taken", func(t *testing.T) {
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateAwaitingOTP, map[string]interface{}{
			"s21_login": "otheruser",
		})

		mockQueries.EXPECT().GetUserAccountByStudentId(gomock.Any(), "otheruser").Return(db.UserAccount{ExternalID: "555"}, nil)
		mockQueries.EXPECT().GetValidAuthVerificationCode(gomock.Any(), gomock.Any()).Return(db.AuthVerificationCode{Code: "123456"}, nil)
		mockQueries.EXPECT().DeleteAuthVerificationCode(gomock.Any(), gomock.Any()).Return(nil)

		render, err := ts.engine.Process(ctx, userID, "123456")
		require.NoError(t, err)
		assert.Contains(t, render.Text, "Пользователь уже авторизован")
	})

	t.Run("success", func(t *testing.T) {
		userID = 301
		_ = ts.engine.InitState(ctx, userID, fsm.FlowRegistration, fsm.StateAwaitingOTP, map[string]interface{}{
			"s21_login": "newuser",
		})

		mockQueries.EXPECT().GetUserAccountByStudentId(gomock.Any(), "newuser").Return(db.UserAccount{}, fmt.Errorf("not found"))
		mockQueries.EXPECT().GetValidAuthVerificationCode(gomock.Any(), gomock.Any()).Return(db.AuthVerificationCode{Code: "654321"}, nil)
		mockQueries.EXPECT().DeleteAuthVerificationCode(gomock.Any(), gomock.Any()).Return(nil)
		mockQueries.EXPECT().CreateUserAccount(gomock.Any(), gomock.Any()).Return(db.UserAccount{ID: 1}, nil)
		mockQueries.EXPECT().UpsertUserBotSettings(gomock.Any(), gomock.Any()).Return(db.UserBotSetting{ID: 1}, nil)
		mockStudentSvc.EXPECT().GetProfileByExternalID(gomock.Any(), db.EnumPlatformTelegram, "301").Return(&service.StudentProfile{Login: "newuser"}, nil)

		render, err := ts.engine.Process(ctx, userID, "654321")
		require.NoError(t, err)
		// After successful registration, user should be in MAIN_MENU
		assert.Contains(t, render.Text, "Личный кабинет")
	})
}
