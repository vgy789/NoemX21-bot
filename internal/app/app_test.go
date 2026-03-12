package app

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/sync/gitsync"
	"go.uber.org/mock/gomock"
)

// mockTelegramService is a simple mock for testing
type mockTelegramService struct {
	runCalled          atomic.Bool
	runWebhookCalled   atomic.Bool
	runCalledCh        chan struct{}
	runWebhookCalledCh chan struct{}
}

func (m *mockTelegramService) Run(ctx context.Context) error { //nolint:revive,stylecheck // test helper
	m.runCalled.Store(true)
	if m.runCalledCh != nil {
		select {
		case m.runCalledCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *mockTelegramService) RunWebhook(ctx context.Context) error { //nolint:revive,stylecheck // test helper
	m.runWebhookCalled.Store(true)
	if m.runWebhookCalledCh != nil {
		select {
		case m.runWebhookCalledCh <- struct{}{}:
		default:
		}
	}
	return nil
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
	a := New(cfg, logger, repo, rcClient, nil, nil, gitSync, campusSvc, scheduleGen, nil, nil)
	assert.NotNil(t, a)
	assert.NotNil(t, a.tg)
}

func TestApp_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTG := &mockTelegramService{runCalledCh: make(chan struct{}, 1)}
	mockGitSync := &mockStarter{}
	mockCampusSvc := &mockStarter{}
	mockScheduleGen := &mockStarter{}

	a := &App{
		tg:          mockTG,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         &config.Config{},
		log:         slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = a.Run(ctx)
		close(done)
	}()
	select {
	case <-mockTG.runCalledCh:
	case <-time.After(time.Second):
		t.Fatal("telegram Run was not called")
	}
	cancel()
	<-done

	assert.True(t, mockTG.runCalled.Load())
}

func TestApp_Run_WebhookMode(t *testing.T) {
	mockTG := &mockTelegramService{runWebhookCalledCh: make(chan struct{}, 1)}
	mockGitSync := &mockStarter{}
	mockCampusSvc := &mockStarter{}
	mockScheduleGen := &mockStarter{}

	cfg := &config.Config{}
	cfg.Telegram.Webhook.Enabled = true
	cfg.Telegram.Webhook.ListenPath = "/webhook"
	cfg.Telegram.Webhook.ListenPort = 8080

	a := &App{
		tg:          mockTG,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         cfg,
		log:         slog.Default(),
	}

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = a.Run(ctx)
		close(done)
	}()
	select {
	case <-mockTG.runWebhookCalledCh:
	case <-time.After(time.Second):
		t.Fatal("telegram RunWebhook was not called")
	}
	cancel()
	<-done

	assert.True(t, mockTG.runWebhookCalled.Load())
}

func TestApp_Run_GitSyncError(t *testing.T) {
	mockTG := &mockTelegramService{runCalledCh: make(chan struct{}, 1)}
	mockGitSync := &mockStarterError{}
	mockCampusSvc := &mockStarter{}
	mockScheduleGen := &mockStarter{}

	a := &App{
		tg:          mockTG,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         &config.Config{},
		log:         slog.Default(),
	}

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = a.Run(ctx)
		close(done)
	}()
	select {
	case <-mockTG.runCalledCh:
	case <-time.After(time.Second):
		t.Fatal("telegram Run was not called")
	}
	cancel()
	<-done
}

func TestApp_Run_CampusSvcError(t *testing.T) {
	mockTG := &mockTelegramService{runCalledCh: make(chan struct{}, 1)}
	mockGitSync := &mockStarter{}
	mockCampusSvc := &mockStarterError{}
	mockScheduleGen := &mockStarter{}

	a := &App{
		tg:          mockTG,
		gitSync:     mockGitSync,
		campusSvc:   mockCampusSvc,
		scheduleGen: mockScheduleGen,
		cfg:         &config.Config{},
		log:         slog.Default(),
	}

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = a.Run(ctx)
		close(done)
	}()
	select {
	case <-mockTG.runCalledCh:
	case <-time.After(time.Second):
		t.Fatal("telegram Run was not called")
	}
	cancel()
	<-done
}

// mockStarterError returns error from Start()
type mockStarterError struct{}

func (m *mockStarterError) Start() error {
	return assert.AnError
}
