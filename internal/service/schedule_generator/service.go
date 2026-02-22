package schedule_generator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/pkg/imgcache"
	"github.com/vgy789/noemx21-bot/internal/pkg/schedule"
)

// ScheduleInvalidator defines the interface for invalidating cached schedule file IDs.
type ScheduleInvalidator interface {
	InvalidateScheduleFileID(campusShortName string)
}

// Service handles periodic generation of room schedule images.
type Service struct {
	queries     db.Querier
	cfg         *config.Config
	stopCh      chan struct{}
	log         *slog.Logger
	imgCache    *imgcache.Store
	invalidator ScheduleInvalidator

	regenMu         sync.Mutex
	regenInProgress bool
	regenPending    bool
}

// New creates a new schedule generation service.
func New(cfg *config.Config, log *slog.Logger, queries db.Querier, cache *imgcache.Store) *Service {
	return &Service{
		queries:  queries,
		cfg:      cfg,
		log:      log.With("service", "schedule_generator"),
		stopCh:   make(chan struct{}),
		imgCache: cache,
	}
}

// SetInvalidator sets the invalidator callback for cached file IDs.
func (s *Service) SetInvalidator(invalidator ScheduleInvalidator) {
	s.invalidator = invalidator
}

// Start initiates the background generation process.
func (s *Service) Start() error {
	if !s.cfg.ScheduleImages.Enabled {
		s.log.Info("schedule image generation is disabled")
		return nil
	}

	s.log.Info("starting schedule image generator", "interval", s.cfg.ScheduleImages.Interval)

	go func() {
		ticker := time.NewTicker(s.cfg.ScheduleImages.Interval)
		defer ticker.Stop()

		// Initial run
		s.generate()

		for {
			select {
			case <-ticker.C:
				s.generate()
			case <-s.stopCh:
				s.log.Info("stopping schedule image generator")
				return
			}
		}
	}()

	return nil
}

// Stop signals the background service to stop.
func (s *Service) Stop() {
	close(s.stopCh)
}

// ForceRegenerate triggers immediate schedule image regeneration for all campuses.
// Called when bookings are created or cancelled.
func (s *Service) ForceRegenerate() {
	s.log.Info("force-triggering schedule image generation due to booking changes")

	s.regenMu.Lock()
	if s.regenInProgress {
		s.regenPending = true
		s.regenMu.Unlock()
		return
	}
	s.regenInProgress = true
	s.regenMu.Unlock()

	go s.runRegenerationLoop()
}

func (s *Service) runRegenerationLoop() {
	for {
		s.generate()

		s.regenMu.Lock()
		if s.regenPending {
			s.regenPending = false
			s.regenMu.Unlock()
			continue
		}
		s.regenInProgress = false
		s.regenMu.Unlock()
		return
	}
}

func (s *Service) generate() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s.log.Debug("starting schedule image generation cycle")

	campuses, err := s.queries.GetAllActiveCampuses(ctx)
	if err != nil {
		s.log.Error("failed to get active campuses", "error", err)
		return
	}

	for _, campus := range campuses {
		s.generateForCampus(ctx, campus)
	}
}

func (s *Service) generateForCampus(ctx context.Context, campus db.GetAllActiveCampusesRow) {
	if !campus.Timezone.Valid || campus.Timezone.String == "" {
		s.log.Debug("timezone not set for campus, skipping generation", "campus", campus.ShortName)
		return
	}

	loc, err := time.LoadLocation(campus.Timezone.String)
	if err != nil {
		s.log.Error("invalid timezone for campus", "campus", campus.ShortName, "tz", campus.Timezone.String, "error", err)
		return
	}

	rooms, err := s.queries.GetActiveRoomsByCampus(ctx, campus.ID)
	if err != nil {
		s.log.Error("failed to get active rooms", "campus", campus.ShortName, "error", err)
		return
	}

	if len(rooms) == 0 {
		return
	}

	now := time.Now().In(loc)
	today := pgtype.Date{Time: now, Valid: true}

	dbBookings := make(map[int16][]db.GetRoomBookingsByDateRow)
	for _, room := range rooms {
		bookings, err := s.queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
			CampusID:    campus.ID,
			RoomID:      room.ID,
			BookingDate: today,
		})
		if err != nil {
			s.log.Error("failed to get bookings", "room", room.Name, "error", err)
			continue
		}
		if len(bookings) > 0 {
			dbBookings[room.ID] = bookings
		}
	}

	scheduleRooms := convertRooms(rooms, dbBookings, loc)

	imgBytes, err := schedule.GenerateScheduleImageBytes(campus.ShortName, now, campus.Timezone.String, scheduleRooms)
	if err != nil {
		s.log.Error("failed to generate schedule image bytes", "campus", campus.ShortName, "error", err)
		return
	}

	key := fmt.Sprintf("imgcache:schedule:%s", campus.ShortName)
	s.imgCache.Set(key, imgBytes)

	dir := filepath.Join(s.cfg.ScheduleImages.TempDir, loc.String())
	if err := os.MkdirAll(dir, 0755); err == nil {
		path := filepath.Join(dir, campus.ShortName+".png")
		if err := os.WriteFile(path, imgBytes, 0644); err != nil {
			s.log.Debug("failed to write schedule image to disk", "campus", campus.ShortName, "path", path, "error", err)
		}
	}

	// Invalidate cached file_id so next send will upload new image
	if s.invalidator != nil {
		s.invalidator.InvalidateScheduleFileID(campus.ShortName)
	}

	s.log.Debug("generated and cached schedule image", "campus", campus.ShortName, "key", key)
}
