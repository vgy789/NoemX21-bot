package telegram

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func TestNewTelegramService(t *testing.T) {
	cfg := &config.TelegramBot{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockStudentSvc := serviceMock.NewMockStudentService(ctrl)

	service := NewTelegramService(cfg, logger, mockStudentSvc)

	require.NotNil(t, service)

	ts, ok := service.(*telegramService)
	require.True(t, ok, "NewTelegramService did not return *telegramService")

	assert.Equal(t, cfg, ts.cfg)
	assert.Equal(t, logger, ts.log)
	assert.NotNil(t, ts.engine, "FSM engine should be initialized")
	assert.Equal(t, mockStudentSvc, ts.studentSvc)

	t.Run("test registry: is_user_registered", func(t *testing.T) {
		mockStudentSvc.EXPECT().GetProfileByTelegramID(gomock.Any(), int64(1)).Return(nil, nil)
		action, _ := ts.engine.Registry().Get("is_user_registered")
		_, res, err := action(context.Background(), 1, nil)
		assert.NoError(t, err)
		assert.True(t, res["registered"].(bool))
	})

	t.Run("test registry: input:set_ru", func(t *testing.T) {
		action, _ := ts.engine.Registry().Get("input:set_ru")
		_, res, err := action(context.Background(), 1, nil)
		assert.NoError(t, err)
		assert.Equal(t, fsm.LangRu, res["language"])
	})
}

func TestTelegramService_GetSender(t *testing.T) {
	s := &telegramService{}

	t.Run("default sender", func(t *testing.T) {
		sender := s.getSender(nil)
		assert.IsType(t, &DefaultSender{}, sender)
	})

	t.Run("provided sender", func(t *testing.T) {
		mockSender := &DefaultSender{} // or any Sender implementation
		s.sender = mockSender
		sender := s.getSender(nil)
		assert.Equal(t, mockSender, sender)
	})
}
