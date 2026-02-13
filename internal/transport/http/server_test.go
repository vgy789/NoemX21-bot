package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

func TestNewServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := mock.NewMockQuerier(ctrl)
	cfg := &config.Config{}
	cfg.APIServer.Port = 8081
	log := slog.Default()

	srv := NewServer(cfg, log, queries)
	require.NotNil(t, srv)
	assert.NotNil(t, srv.server)
	assert.Equal(t, ":8081", srv.server.Addr)
}

func TestServer_Start_Stop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := mock.NewMockQuerier(ctrl)
	cfg := &config.Config{}
	log := slog.Default()
	srv := NewServer(cfg, log, queries)

	// Use :0 to get a free port
	srv.server.Addr = ":0"

	done := make(chan struct{})
	go func() {
		srv.Start()
		close(done)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Stop(ctx)
	assert.NoError(t, err)

	<-done
}

func TestServer_Register_route(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queries := mock.NewMockQuerier(ctrl)
	cfg := &config.Config{}
	log := slog.Default()
	srv := NewServer(cfg, log, queries)

	// Verify handler is registered: request to register endpoint
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/webhook/register", nil)
	rw := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rw.Code)
}
