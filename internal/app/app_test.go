package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

func TestApp_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTG := mock.NewMockTelegramService(ctrl)

	// Expect Run to be called once
	mockTG.EXPECT().Run().Times(1)

	a := &App{
		tg: mockTG,
	}

	a.Run()
}

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.Default()
	repo := &db.DBWrapper{}

	a := New(cfg, logger, repo)
	assert.NotNil(t, a)
	assert.NotNil(t, a.tg)
	// Minimal dependencies to satisfy New
	// We use nil for some because they aren't dereferenced in New (except for NewTelegramService calls)
	// But student service takes db.Querier, so we can pass nil or a mock.

	// app.New calls service.NewStudentService(repo)
	// then calls telegram.NewTelegramService(...)

	// Since telegram.NewTelegramService calls fsm.NewFlowParser, it might try to read files.
	// This might fail in test environment if paths are wrong.
}
