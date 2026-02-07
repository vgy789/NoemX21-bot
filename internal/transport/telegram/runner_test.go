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
	"github.com/vgy789/noemx21-bot/internal/fsm/setup"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func TestNewTelegramService(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockStudentSvc := serviceMock.NewMockStudentService(ctrl)
	mockQuerier := dbMock.NewMockQuerier(ctrl)
	mockRCClient := rocketchat.NewClient("", "", "")

	engine := setup.NewFSM(cfg, logger, mockQuerier, mockStudentSvc, mockRCClient, nil, nil, "docs/specs/flows")
	svc := NewTelegramService(cfg, logger, mockStudentSvc, engine)
	ts, ok := svc.(*telegramService)
	require.True(t, ok, "NewTelegramService did not return *telegramService")
	// Use memory repo for tests
	ts.engine = fsm.NewEngine(ts.engine.Parser(), fsm.NewMemoryStateRepository(), logger, ts.engine.Registry(), ts.engine.Sanitizer())

	require.NotNil(t, svc)
	require.True(t, ok, "NewTelegramService did not return *telegramService")

	assert.NotNil(t, ts.log)
	assert.NotNil(t, ts.engine, "FSM engine should be initialized")
	assert.Equal(t, mockStudentSvc, ts.studentSvc)

	t.Run("test registry: is_user_registered", func(t *testing.T) {
		mockStudentSvc.EXPECT().GetProfileByExternalID(gomock.Any(), db.EnumPlatformTelegram, "1").Return(nil, nil)
		action, _ := ts.engine.Registry().Get("is_user_registered")
		_, res, err := action(context.Background(), 1, nil)
		assert.NoError(t, err)
		assert.True(t, res["registered"].(bool))
	})

	t.Run("test registry: input:set_ru", func(t *testing.T) {
		// Expect call to check account existence - return error (not found) for this test
		mockQuerier.EXPECT().GetUserAccountByExternalId(gomock.Any(), gomock.Any()).Return(db.UserAccount{}, fmt.Errorf("not found"))

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
