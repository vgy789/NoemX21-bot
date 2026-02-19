package schedule_generator

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/pkg/schedule"
)

// Service handles periodic generation of room schedule images.
type Service struct {
	queries db.Querier
	cfg     *config.Config
	stopCh  chan struct{}
	log     *slog.Logger
}

// New creates a new schedule generation service.
func New(cfg *config.Config, log *slog.Logger, queries db.Querier) *Service {
	return &Service{
		queries: queries,
		cfg:     cfg,
		log:     log.With("service", "schedule_generator"),
		stopCh:  make(chan struct{}),
	}
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

func (s *Service) generate() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s.log.Debug("starting schedule image generation cycle")

	timezones, err := s.queries.GetDistinctUserTimezones(ctx)
	if err != nil {
		s.log.Error("failed to get distinct user timezones", "error", err)
		return
	}

	for _, tzName := range timezones {
		s.generateForTimezone(ctx, tzName)
	}
}

func (s *Service) generateForTimezone(ctx context.Context, tzName string) {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		s.log.Error("invalid timezone", "tz", tzName, "error", err)
		return
	}

	campuses, err := s.queries.GetCampusesWithBookingsForTimezone(ctx, pgtype.Text{String: tzName, Valid: true})
	if err != nil {
		s.log.Error("failed to get campuses for timezone", "tz", tzName, "error", err)
		return
	}

	for _, campus := range campuses {
		s.generateForCampus(ctx, campus, loc, tzName)
	}
}

func (s *Service) generateForCampus(ctx context.Context, campus db.GetCampusesWithBookingsForTimezoneRow, loc *time.Location, tzName string) {
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
	hasBookings := false
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
			hasBookings = true
		}
	}

	if !hasBookings {
		return
	}

	scheduleRooms := convertRooms(rooms, dbBookings, loc)

	// Output path: {tmp_dir}/{timezone}/{campus_short_name}.png
	outputPath := filepath.Join(s.cfg.ScheduleImages.TempDir, tzName, fmt.Sprintf("%s.png", campus.ShortName))

	err = schedule.GenerateScheduleImage(campus.ShortName, now, scheduleRooms, outputPath)
	if err != nil {
		s.log.Error("failed to generate schedule image", "campus", campus.ShortName, "error", err)
	} else {
		s.log.Debug("generated schedule image", "campus", campus.ShortName, "path", outputPath)
	}
}
