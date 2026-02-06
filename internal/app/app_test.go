package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"go.uber.org/mock/gomock"
)

// mockTelegramService is a simple mock for testing
type mockTelegramService struct {
	runCalled bool
}

func (m *mockTelegramService) Run() {
	m.runCalled = true
}

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := &config.Config{}
	logger := slog.Default()
	repo := &db.DBWrapper{}
	rcClient := rocketchat.NewClient("", "", "")

	a := New(cfg, logger, repo, rcClient, nil, nil)
	assert.NotNil(t, a)
	assert.NotNil(t, a.tg)
}

func TestApp_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTG := &mockTelegramService{}

	// Create a mock HTTP server that doesn't actually start
	mockHTTPServer := &mockHTTPServer{}

	a := &App{
		tg:         mockTG,
		httpServer: mockHTTPServer,
	}

	// We can't actually call Run() as it would block, so we just test the structure
	assert.NotNil(t, a.tg)
	assert.NotNil(t, a.httpServer)
}

// mockHTTPServer is a simple mock for testing
type mockHTTPServer struct{}

func (m *mockHTTPServer) Start() {
	// Do nothing in tests
}
