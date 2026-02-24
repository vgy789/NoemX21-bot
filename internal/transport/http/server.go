package http

import (
	"context"
	"errors"
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
	mux    *http.ServeMux
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
		mux:    mux,
		log:    log,
	}
}

// AddHandler registers a new HTTP handler at the specified path.
func (s *Server) AddHandler(path string, handler http.Handler) {
	s.mux.Handle(path, handler)
	s.log.Info("registered http handler", "path", path)
}

// Start runs HTTP server until context cancellation or fatal error.
func (s *Server) Start(ctx context.Context) error {
	s.log.Info("starting http server", "addr", s.server.Addr)

	errCh := make(chan error, 1)
	go func() {
		err := s.server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
