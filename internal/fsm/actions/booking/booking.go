package booking

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// ScheduleRegenerator defines the interface for triggering schedule regeneration.
type ScheduleRegenerator interface {
	ForceRegenerate()
}

// Register registers booking-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, cfg *config.Config, aliasRegistrar func(alias, target string), scheduleRegen ScheduleRegenerator) {
	if aliasRegistrar != nil {
		aliasRegistrar("BOOKING_MENU", "booking.yaml/BOOKING_DASHBOARD")
	}
	registerTUIActions(registry, queries, cfg, scheduleRegen)
}

func getUserTimezone(ctx context.Context, queries db.Querier, userID int64, campusUUID pgtype.UUID) *time.Location {
	defaultLoc := time.UTC
	if moscow, err := time.LoadLocation("Europe/Moscow"); err == nil {
		defaultLoc = moscow
	}

	var campusLoc *time.Location
	if campusUUID.Valid {
		c, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.GetCampusByIDRow, error) {
			return queries.GetCampusByID(qctx, campusUUID)
		})
		if err == nil && c.Timezone.Valid && c.Timezone.String != "" {
			if loc, err := time.LoadLocation(c.Timezone.String); err == nil {
				campusLoc = loc
			}
		}
	}

	acc, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.UserAccount, error) {
		return queries.GetUserAccountByExternalId(qctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
	})
	if err == nil {
		u, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.RegisteredUser, error) {
			return queries.GetRegisteredUserByS21Login(qctx, acc.S21Login)
		})
		if err == nil {
			if u.Timezone != "" {
				if loc, err := time.LoadLocation(u.Timezone); err == nil {
					// In booking UX campus-local time is expected.
					// If user timezone is UTC but campus timezone is known, prefer campus.
					if u.Timezone == "UTC" && campusLoc != nil {
						return campusLoc
					}
					return loc
				}
			}
		}
	}

	if campusLoc != nil {
		return campusLoc
	}

	return defaultLoc
}

func toInt16(v any) int16 {
	switch val := v.(type) {
	case string:
		var i int16
		_, _ = fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int16(val)
	case float32:
		return int16(val)
	case int:
		return int16(val)
	case int16:
		return val
	case int32:
		return int16(val)
	case int64:
		return int16(val)
	case uint:
		return int16(val)
	case uint16:
		return int16(val)
	case uint32:
		return int16(val)
	case uint64:
		return int16(val)
	}
	return 0
}

func toInt32(v any) int32 {
	switch val := v.(type) {
	case string:
		i, _ := strconv.ParseInt(val, 10, 32)
		return int32(i)
	case float64:
		return int32(val)
	case int:
		return int32(val)
	case int32:
		return val
	}
	return 0
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case string:
		var i int64
		_, _ = fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	}
	return 0
}

// checkBookingConflict checks if a booking would conflict with existing bookings.
// Handles bookings that cross midnight (e.g., 23:30-00:30).
func checkBookingConflict(
	ctx context.Context,
	queries db.Querier,
	campusUUID pgtype.UUID,
	roomID int16,
	date time.Time,
	startMin, endMin int64,
	loc *time.Location,
) bool {
	// Check conflicts on the booking date
	bsCurrent, _ := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetRoomBookingsByDateRow, error) {
		return queries.GetRoomBookingsByDate(qctx, db.GetRoomBookingsByDateParams{
			CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: date, Valid: true},
		})
	})
	for _, b := range bsCurrent {
		bStart := b.StartTime.Microseconds / 60000000
		bEnd := bStart + int64(b.DurationMinutes)
		// Check overlap: [startMin, endMin) overlaps [bStart, bEnd)
		if startMin < bEnd && endMin > bStart {
			return true
		}
	}

	// If booking crosses midnight, check next day for conflicts in 00:00-00:30 range
	if endMin > 24*60 {
		nextDate := date.AddDate(0, 0, 1)
		minsAfterMidnight := endMin - 24*60
		bsNext, _ := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetRoomBookingsByDateRow, error) {
			return queries.GetRoomBookingsByDate(qctx, db.GetRoomBookingsByDateParams{
				CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: nextDate, Valid: true},
			})
		})
		for _, b := range bsNext {
			bStart := b.StartTime.Microseconds / 60000000
			// Only check early morning slots (up to 01:30)
			if bStart < 90 { // Before 01:30
				bEnd := bStart + int64(b.DurationMinutes)
				// Our booking occupies [0, minsAfterMidnight) on next day
				// Check overlap
				if 0 < bEnd && minsAfterMidnight > bStart {
					return true
				}
			}
		}
	}

	// Also check if there's a booking from previous day that crosses midnight
	// and would conflict with our booking
	if startMin < 60 { // Our booking starts early morning (00:00-01:00)
		prevDate := date.AddDate(0, 0, -1)
		bsPrev, _ := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetRoomBookingsByDateRow, error) {
			return queries.GetRoomBookingsByDate(qctx, db.GetRoomBookingsByDateParams{
				CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: prevDate, Valid: true},
			})
		})
		for _, b := range bsPrev {
			bStart := b.StartTime.Microseconds / 60000000
			bEnd := bStart + int64(b.DurationMinutes)
			// Check if previous day's booking crosses midnight and overlaps with us
			if bEnd > 24*60 {
				prevEndAfterMidnight := bEnd - 24*60
				if startMin < prevEndAfterMidnight {
					return true
				}
			}
		}
	}

	return false
}

func checkUserBookingLimits(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	startAt time.Time,
	newDurationMin int,
	loc *time.Location,
) string {
	const (
		maxBookingsPerUser = 2
		maxDailyMinutes    = 180
	)

	if userID == 0 || newDurationMin <= 0 {
		return ""
	}

	bookings, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetUserRoomBookingsRow, error) {
		return queries.GetUserRoomBookings(qctx, userID)
	})
	if err != nil {
		return ""
	}

	now := time.Now().In(loc)
	target := startAt.In(loc)
	activeOrFuture := 0
	dayTotal := 0

	for _, b := range bookings {
		bStart, bEnd := bookingBoundsLocal(b.BookingDate, b.StartTime, b.DurationMinutes, loc)
		if bEnd.After(now) {
			activeOrFuture++
		}
		by, bm, bd := bStart.Date()
		ty, tm, td := target.Date()
		if by == ty && bm == tm && bd == td {
			dayTotal += int(b.DurationMinutes)
		}
	}

	if activeOrFuture >= maxBookingsPerUser {
		return "user_booking_count_limit"
	}
	if dayTotal+newDurationMin > maxDailyMinutes {
		return "user_daily_limit"
	}
	return ""
}

// checkUserBookingConflict prevents one user from holding overlapping bookings.
// It works across all rooms returned by GetUserRoomBookings.
func checkUserBookingConflict(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	date time.Time,
	startMin, endMin int64,
	loc *time.Location,
	ignoreBookingID int64,
) bool {
	if userID == 0 {
		return false
	}

	requestedStart := time.Date(
		date.In(loc).Year(),
		date.In(loc).Month(),
		date.In(loc).Day(),
		int(startMin/60),
		int(startMin%60),
		0,
		0,
		loc,
	)
	requestedEnd := requestedStart.Add(time.Duration(endMin-startMin) * time.Minute)

	userBookings, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetUserRoomBookingsRow, error) {
		return queries.GetUserRoomBookings(qctx, userID)
	})
	if err != nil {
		return false
	}

	for _, b := range userBookings {
		if ignoreBookingID > 0 && b.ID == ignoreBookingID {
			continue
		}
		existingStart, existingEnd := bookingBoundsLocal(b.BookingDate, b.StartTime, b.DurationMinutes, loc)
		if requestedStart.Before(existingEnd) && requestedEnd.After(existingStart) {
			return true
		}
	}

	return false
}

func getVisualizationPath(ctx context.Context, queries db.Querier, campusUUID pgtype.UUID, cfg *config.Config) string {
	if cfg == nil || !cfg.ScheduleImages.Enabled || !campusUUID.Valid {
		return ""
	}
	c, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.GetCampusByIDRow, error) {
		return queries.GetCampusByID(qctx, campusUUID)
	})
	if err != nil {
		return ""
	}
	if c.Timezone.Valid && c.Timezone.String != "" {
		if loc, err := time.LoadLocation(c.Timezone.String); err == nil {
			return filepath.Join(cfg.ScheduleImages.TempDir, loc.String(), c.ShortName+".png")
		}
	}
	return fmt.Sprintf("imgcache:schedule:%s", c.ShortName)
}
