package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type Server struct {
	server *http.Server
	log    *slog.Logger
}

func NewServer(cfg *config.Config, log *slog.Logger, queries db.Querier) *Server {
	apiKeyService := service.NewApiKeyService(queries)
	handler := NewWebhookHandler(apiKeyService, queries, log)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/register", handler.Register)

	port := cfg.APIServer.Port

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return &Server{
		server: srv,
		log:    log,
	}
}

func (s *Server) Start() {
	s.log.Info("starting http server", "addr", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.log.Error("http server failed", "error", err)
	}
}

func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
