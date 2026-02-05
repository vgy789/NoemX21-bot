package telegram

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	serviceMock "github.com/vgy789/noemx21-bot/internal/service/mock"
	"go.uber.org/mock/gomock"
)

// dbtxMock implements db.DBTX for testing
type dbtxMock struct {
	ctrl *gomock.Controller
}

func (m *dbtxMock) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Exec", ctx, query, args)
	ret0, _ := ret[0].(pgconn.CommandTag)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (m *dbtxMock) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Query", ctx, query, args)
	ret0, _ := ret[0].(pgx.Rows)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (m *dbtxMock) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "QueryRow", ctx, query, args)
	return ret[0].(pgx.Row)
}

func TestNewTelegramService(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockStudentSvc := serviceMock.NewMockStudentService(ctrl)
	mockDBTX := &dbtxMock{ctrl: ctrl}
	mockQueries := db.New(mockDBTX)
	mockRCClient := rocketchat.NewClient("", "", "")

	service := NewTelegramService(cfg, logger, mockStudentSvc, mockQueries, mockRCClient)

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
