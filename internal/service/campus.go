package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

type CampusService struct {
	queries  db.Querier
	s21      *s21.Client
	config   *config.Config
	log      *slog.Logger
	cron     *cron.Cron
	credsSvc *CredentialService
}

func NewCampusService(queries db.Querier, s21Client *s21.Client, cfg *config.Config, log *slog.Logger, credsSvc *CredentialService) *CampusService {
	return &CampusService{
		queries:  queries,
		s21:      s21Client,
		config:   cfg,
		log:      log.With("service", "campus"),
		cron:     cron.New(),
		credsSvc: credsSvc,
	}
}

func (s *CampusService) Start() error {
	// Schedule once a week
	_, err := s.cron.AddFunc("@weekly", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.UpdateCampuses(ctx); err != nil {
			s.log.Error("failed to update campuses", "error", err)
		}
	})
	if err != nil {
		return err
	}
	s.cron.Start()

	// Initial update in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.UpdateCampuses(ctx); err != nil {
			s.log.Error("initial campus update failed", "error", err)
		}
	}()

	return nil
}

func (s *CampusService) Stop() {
	s.cron.Stop()
}

func (s *CampusService) UpdateCampuses(ctx context.Context) error {
	s.log.Info("updating campuses from S21 API")

	if s.config.Init.SchoolLogin == "" {
		return fmt.Errorf("SCHOOL21_USER_LOGIN is empty, cannot update campuses")
	}

	// 1. Get token
	token, err := s.credsSvc.GetValidToken(ctx, s.config.Init.SchoolLogin)
	if err != nil {
		return fmt.Errorf("failed to get valid token: %w", err)
	}

	// 2. Fetch campuses
	campuses, err := s.s21.GetCampuses(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to fetch campuses: %w", err)
	}

	// 3. Upsert into DB
	for _, c := range campuses {
		id := pgtype.UUID{}
		if err := id.Scan(c.ID); err != nil {
			s.log.Error("invalid campus ID", "id", c.ID, "error", err)
			continue
		}

		_, err := s.queries.UpsertCampus(ctx, db.UpsertCampusParams{
			ID:        id,
			ShortName: c.ShortName,
			FullName:  c.FullName,
			Timezone:  pgtype.Text{String: c.Timezone, Valid: true},
			IsActive:  true,
		})
		if err != nil {
			s.log.Error("failed to upsert campus", "id", c.ID, "name", c.ShortName, "error", err)
		}
	}

	s.log.Info("campuses updated", "count", len(campuses))
	return nil
}

// EnsureCampusPresent checks whether a campus with given UUID exists in DB.
// If it's missing, it fetches campuses from S21 API (using provided token) and upserts the matching campus.
func EnsureCampusPresent(ctx context.Context, queries db.Querier, s21Client *s21.Client, token string, log *slog.Logger, campusID string) error {
	if campusID == "" {
		return nil
	}

	var id pgtype.UUID
	if err := id.Scan(campusID); err != nil {
		log.Error("invalid campus id", "id", campusID, "error", err)
		return err
	}

	// Check if campus already exists
	if _, err := queries.GetCampusByID(ctx, id); err == nil {
		return nil
	}

	// Need token to call API
	if token == "" {
		log.Warn("no token provided to EnsureCampusPresent, skipping campus fetch", "campus_id", campusID)
		return nil
	}

	campuses, err := s21Client.GetCampuses(ctx, token)
	if err != nil {
		log.Error("failed to fetch campuses from S21 API", "error", err)
		return err
	}

	for _, c := range campuses {
		if c.ID == campusID {
			var cid pgtype.UUID
			if err := cid.Scan(c.ID); err != nil {
				log.Error("invalid campus id from API", "id", c.ID, "error", err)
				return err
			}
			_, err := queries.UpsertCampus(ctx, db.UpsertCampusParams{
				ID:        cid,
				ShortName: c.ShortName,
				FullName:  c.FullName,
				Timezone:  pgtype.Text{String: c.Timezone, Valid: true},
				IsActive:  true,
			})
			if err != nil {
				log.Error("failed to upsert campus", "id", c.ID, "error", err)
				return err
			}
			log.Info("campus upserted from API", "id", c.ID, "name", c.ShortName)
			return nil
		}
	}

	log.Warn("campus id not found in S21 API response", "campus_id", campusID)
	return nil
}
