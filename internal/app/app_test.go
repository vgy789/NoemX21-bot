package app

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/sync/gitsync"
	"go.uber.org/mock/gomock"
)

// mockTelegramService is a simple mock for testing
type mockTelegramService struct {
	runCalled         bool
	runWebhookCalled  bool
	webhookHandlerRet http.Handler
}

func (m *mockTelegramService) Run() {
	m.runCalled = true
}

func (m *mockTelegramService) RunWebhook() error {
	m.runWebhookCalled = true
	return nil
}

func (m *mockTelegramService) GetWebhookHandler() http.Handler {
	return m.webhookHandlerRet
}

// mockStarter returns nil from Start()
type mockStarter struct{}

func (m *mockStarter) Start() error {
	return nil
}

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cfg := &config.Config{}
	logger := slog.Default()
	repo := &db.DBWrapper{}
	rcClient := rocketchat.NewClient("", "", "")

	gitSync := gitsync.New(cfg.GitSync, nil, logger)
	campusSvc := &mockStarter{}
	scheduleGen := &mockStarter{}
	a := New(cfg, logger, repo, rcClient, nil, nil, gitSync, campusSvc, scheduleGen)
	assert.NotNil(t, a)
	assert.NotNil(t, a.tg)
}

func TestApp_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTG := &mockTelegramService{}
	mockHTTPServer := &mockHTTPServer{}
	mockGitSync := &mockStarter{}
	mockCampusSvc := &mockStarter{}
	mockScheduleGen := &mockStarter{}

	a := &App{
		tg:          mockTG,
		httpServer:  mockHTTPServer,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         &config.Config{},
		log:         slog.Default(),
	}

	a.Run()

	assert.True(t, mockTG.runCalled)
}

func TestApp_Run_WebhookMode(t *testing.T) {
	mockTG := &mockTelegramService{}
	mockHTTPServer := &mockHTTPServer{}
	mockGitSync := &mockStarter{}
	mockCampusSvc := &mockStarter{}
	mockScheduleGen := &mockStarter{}

	cfg := &config.Config{}
	cfg.Telegram.Webhook.Enabled = true
	cfg.Telegram.Webhook.ListenPath = "/webhook"
	cfg.Telegram.Webhook.ListenPort = 8080

	a := &App{
		tg:          mockTG,
		httpServer:  mockHTTPServer,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         cfg,
		log:         slog.Default(),
	}

	// Run in goroutine since it blocks
	done := make(chan struct{})
	go func() {
		a.Run()
		close(done)
	}()

	// Give it a moment to execute
	select {
	case <-done:
		// Completed
	case <-make(chan struct{}, 1):
		// Still running
	}

	assert.True(t, mockTG.runWebhookCalled)
}

func TestApp_Run_GitSyncError(t *testing.T) {
	mockTG := &mockTelegramService{}
	mockHTTPServer := &mockHTTPServer{}
	mockGitSync := &mockStarterError{}
	mockCampusSvc := &mockStarter{}
	mockScheduleGen := &mockStarter{}

	a := &App{
		tg:          mockTG,
		httpServer:  mockHTTPServer,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         &config.Config{},
		log:         slog.Default(),
	}

	// Should not panic, just log error
	done := make(chan struct{})
	go func() {
		a.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-make(chan struct{}, 1):
	}
}

func TestApp_Run_CampusSvcError(t *testing.T) {
	mockTG := &mockTelegramService{}
	mockHTTPServer := &mockHTTPServer{}
	mockGitSync := &mockStarter{}
	mockCampusSvc := &mockStarterError{}
	mockScheduleGen := &mockStarter{}

	a := &App{
		tg:          mockTG,
		httpServer:  mockHTTPServer,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         &config.Config{},
		log:         slog.Default(),
	}

	done := make(chan struct{})
	go func() {
		a.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-make(chan struct{}, 1):
	}
}

// mockStarterError returns error from Start()
type mockStarterError struct{}

func (m *mockStarterError) Start() error {
	return assert.AnError
}

// mockHTTPServer is a simple mock for testing
type mockHTTPServer struct{}

func (m *mockHTTPServer) Start() {
	// Do nothing in tests
}

func (m *mockHTTPServer) AddHandler(path string, handler http.Handler) {
	// Do nothing in tests
}
