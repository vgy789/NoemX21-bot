package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	prrSync  PRRStatusBroadcaster
}

const (
	reviewRequestCleanupSchedule = "@every 60m"
	reviewRequestCleanupBatch    = int32(500)
	reviewRequestCleanupStaleFor = time.Hour
)

// PRRStatusBroadcaster syncs review request status to external channels.
type PRRStatusBroadcaster interface {
	SyncReviewRequestStatus(ctx context.Context, reviewRequestID int64, status string) error
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

	if err := s.scheduleReviewCleanup(); err != nil {
		return err
	}

	s.cron.Start()

	// Initial update in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.UpdateCampuses(ctx); err != nil {
			msg := "initial campus update failed"
			if strings.Contains(err.Error(), "status 401") {
				msg += " (check SCHOOL21_USER_LOGIN/PASSWORD credentials)"
			}
			s.log.Warn(msg, "error", err)
		}
	}()

	return nil
}

func (s *CampusService) Stop() {
	s.cron.Stop()
}

// SetPRRStatusBroadcaster configures optional group notifications sync.
func (s *CampusService) SetPRRStatusBroadcaster(syncer PRRStatusBroadcaster) {
	if s == nil {
		return
	}
	s.prrSync = syncer
}

func (s *CampusService) scheduleReviewCleanup() error {
	_, err := s.cron.AddFunc(reviewRequestCleanupSchedule, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.CleanupOutdatedReviewRequests(ctx); err != nil {
			s.log.Warn("review cleanup failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule review cleanup: %w", err)
	}
	return nil
}

func (s *CampusService) CleanupOutdatedReviewRequests(ctx context.Context) error {
	if s.queries == nil || s.s21 == nil || s.credsSvc == nil || s.config == nil || strings.TrimSpace(s.config.Init.SchoolLogin) == "" {
		return nil
	}

	token, err := s.credsSvc.GetValidToken(ctx, s.config.Init.SchoolLogin)
	if err != nil {
		return fmt.Errorf("failed to get token for review cleanup: %w", err)
	}

	rows, err := s.queries.GetReviewRequestsForCleanup(ctx, db.GetReviewRequestsForCleanupParams{
		UpdatedAt: pgtype.Timestamptz{
			Time:  time.Now().Add(-reviewRequestCleanupStaleFor),
			Valid: true,
		},
		Limit: reviewRequestCleanupBatch,
	})
	if err != nil {
		return fmt.Errorf("failed to load review cleanup candidates: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	projectsByLogin := make(map[string]map[int64]bool, len(rows))
	for _, row := range rows {
		login := strings.TrimSpace(row.RequesterS21Login)
		if login == "" {
			continue
		}
		if _, exists := projectsByLogin[login]; exists {
			continue
		}

		resp, apiErr := s.s21.GetParticipantProjects(ctx, token, login, 1000, 0, "IN_REVIEWS")
		if apiErr != nil {
			s.log.Warn("review cleanup: failed to fetch IN_REVIEWS projects", "login", login, "error", apiErr)
			continue
		}

		set := map[int64]bool{}
		if resp != nil {
			set = make(map[int64]bool, len(resp.Projects))
			for _, p := range resp.Projects {
				if strings.EqualFold(strings.TrimSpace(p.Status), "IN_REVIEWS") {
					set[p.ID] = true
				}
			}
		}
		projectsByLogin[login] = set
	}

	closed := 0
	for _, row := range rows {
		login := strings.TrimSpace(row.RequesterS21Login)
		if login == "" {
			continue
		}
		currentProjects, ok := projectsByLogin[login]
		if !ok {
			continue
		}
		if currentProjects[row.ProjectID] {
			continue
		}
		if err := s.queries.CloseReviewRequestByID(ctx, row.ID); err != nil {
			s.log.Warn("review cleanup: failed to close outdated request", "request_id", row.ID, "login", login, "project_id", row.ProjectID, "error", err)
			continue
		}
		if s.prrSync != nil {
			if syncErr := s.prrSync.SyncReviewRequestStatus(ctx, row.ID, "CLOSED"); syncErr != nil {
				s.log.Warn("review cleanup: failed to sync closed status to PRR groups", "request_id", row.ID, "error", syncErr)
			}
		}
		closed++
	}

	if closed > 0 {
		s.log.Info("review cleanup done", "checked", len(rows), "closed", closed)
	}
	return nil
}

func (s *CampusService) UpdateCampuses(ctx context.Context) error {
	s.log.Info("updating campuses from S21 API")

	if s.config == nil || s.config.Init.SchoolLogin == "" {
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
			ID:             id,
			ShortName:      c.ShortName,
			FullName:       c.FullName,
			Timezone:       pgtype.Text{String: c.Timezone, Valid: true},
			IsActive:       true,
			LeaderName:     pgtype.Text{Valid: false},
			LeaderFormLink: pgtype.Text{Valid: false},
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
				ID:             cid,
				ShortName:      c.ShortName,
				FullName:       c.FullName,
				Timezone:       pgtype.Text{String: c.Timezone, Valid: true},
				IsActive:       true,
				LeaderName:     pgtype.Text{Valid: false},
				LeaderFormLink: pgtype.Text{Valid: false},
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
