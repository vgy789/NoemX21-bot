//go:build integration

package booking

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/testutil/testdb"
)

func seedTestData(t *testing.T, queries db.Querier) (pgtype.UUID, int64, int64) {
	ctx := context.Background()
	campusID := pgtype.UUID{}
	_ = campusID.Scan("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	_, err := queries.UpsertCampus(ctx, db.UpsertCampusParams{
		ID:        campusID,
		ShortName: "MOSCOW",
		FullName:  "Moscow Campus",
		Timezone:  pgtype.Text{String: "Europe/Moscow", Valid: true},
		IsActive:  true,
	})
	assert.NoError(t, err)

	_, err = queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
		S21Login:     "user_a",
		RocketchatID: "rc_a",
		Timezone:     "Europe/Moscow",
	})
	assert.NoError(t, err)

	_, err = queries.UpsertRegisteredUser(ctx, db.UpsertRegisteredUserParams{
		S21Login:     "user_b",
		RocketchatID: "rc_b",
		Timezone:     "Europe/Moscow",
	})
	assert.NoError(t, err)

	accA, err := queries.CreateUserAccount(ctx, db.CreateUserAccountParams{
		S21Login:   "user_a",
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "100",
	})
	assert.NoError(t, err)

	accB, err := queries.CreateUserAccount(ctx, db.CreateUserAccountParams{
		S21Login:   "user_b",
		Platform:   db.EnumPlatformTelegram,
		ExternalID: "200",
	})
	assert.NoError(t, err)

	_, err = queries.UpsertRoom(ctx, db.UpsertRoomParams{
		ID:          1,
		CampusID:    campusID,
		Name:        "Room 1",
		MinDuration: 15,
		MaxDuration: 120,
		IsActive:    pgtype.Bool{Bool: true, Valid: true},
		Capacity:    2,
	})
	assert.NoError(t, err)

	_, err = queries.UpsertRoom(ctx, db.UpsertRoomParams{
		ID:          2,
		CampusID:    campusID,
		Name:        "Room 2",
		MinDuration: 15,
		MaxDuration: 120,
		IsActive:    pgtype.Bool{Bool: true, Valid: true},
		Capacity:    4,
	})
	assert.NoError(t, err)

	return campusID, accA.ID, accB.ID
}

func TestBooking_Integration_Scenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tdb := testdb.NewPostgres(t)
	queries := tdb.DB.Queries
	campusID, userAID, userBID := seedTestData(t, queries)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Europe/Moscow")

	today := time.Now().In(loc)
	todayDate := pgtype.Date{Time: today, Valid: true}

	// 1. CreateBooking_Success
	startTime1 := pgtype.Time{Microseconds: 10 * 60 * 60 * 1000000, Valid: true} // 10:00
	b1, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
		CampusID:        campusID,
		RoomID:          1,
		UserID:          userAID,
		BookingDate:     todayDate,
		StartTime:       startTime1,
		DurationMinutes: 30,
	})
	assert.NoError(t, err)
	assert.NotZero(t, b1.ID)

	// 2. CreateBooking_DuplicateSlot (returns no rows/error depending on sqlc, but we have DO NOTHING)
	b2, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
		CampusID:        campusID,
		RoomID:          1,
		UserID:          userBID,
		BookingDate:     todayDate,
		StartTime:       startTime1,
		DurationMinutes: 30,
	})
	// sqlc QueryRow on INSERT...DO NOTHING returns ErrNoRows if no row was inserted/returned
	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.Zero(t, b2.ID)

	// 3. TwoUsers_DifferentSlots
	startTime2 := pgtype.Time{Microseconds: 11 * 60 * 60 * 1000000, Valid: true} // 11:00
	b3, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
		CampusID:        campusID,
		RoomID:          1,
		UserID:          userBID,
		BookingDate:     todayDate,
		StartTime:       startTime2,
		DurationMinutes: 30,
	})
	assert.NoError(t, err)
	assert.NotZero(t, b3.ID)

	// 5. CancelAndRebook
	err = queries.CancelRoomBooking(ctx, db.CancelRoomBookingParams{
		ID:     b1.ID,
		UserID: userAID,
	})
	assert.NoError(t, err)

	b1Retry, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
		CampusID:        campusID,
		RoomID:          1,
		UserID:          userAID,
		BookingDate:     todayDate,
		StartTime:       startTime1,
		DurationMinutes: 30,
	})
	assert.NoError(t, err)
	assert.NotZero(t, b1Retry.ID)

	// 7. ConflictDetection_Overlap
	// 10:00-10:30 exists (b1Retry)
	// Try checkBookingConflict for 10:15-10:45
	hasConflict := checkBookingConflict(ctx, queries, campusID, 1, today, 10*60+15, 10*60+45, loc)
	assert.True(t, hasConflict)

	// 8. AdjacentBookings_NoConflict
	// 10:00-10:30 exists. Try 10:30-11:00
	hasConflictAdjacent := checkBookingConflict(ctx, queries, campusID, 1, today, 10*60+30, 11*60, loc)
	assert.False(t, hasConflictAdjacent)

	// 10. GetBookingsByDate_ReturnsNickname
	rows, err := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
		CampusID:    campusID,
		RoomID:      1,
		BookingDate: todayDate,
	})
	assert.NoError(t, err)
	// Should have 10:00 and 11:00 bookings
	assert.Len(t, rows, 2)
	assert.Equal(t, "user_a", rows[0].Nickname)
	assert.Equal(t, "user_b", rows[1].Nickname)

	// 6. CancelOtherUsersBooking
	err = queries.CancelRoomBooking(ctx, db.CancelRoomBookingParams{
		ID:     b3.ID,   // b3 is booked by userBID
		UserID: userAID, // try to cancel with userAID
	})
	// In my project, it returns no error even if no rows were deleted (standard Exec)
	// But the booking should still exist
	assert.NoError(t, err)

	userBBookings, err := queries.GetUserRoomBookings(ctx, userBID)
	assert.NoError(t, err)
	found := false
	for _, ub := range userBBookings {
		if ub.ID == b3.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "Booking should still exist because it was canceled with wrong user_id")

	// 11. GetUserBookings_FiltersByDate
	// Create a booking in the past
	yesterday := today.AddDate(0, 0, -1)
	yesterdayDate := pgtype.Date{Time: yesterday, Valid: true}
	_, err = queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
		CampusID:        campusID,
		RoomID:          1,
		UserID:          userAID,
		BookingDate:     yesterdayDate,
		StartTime:       startTime1,
		DurationMinutes: 30,
	})
	assert.NoError(t, err)

	userABookings, err := queries.GetUserRoomBookings(ctx, userAID)
	assert.NoError(t, err)
	// Should only find b1Retry (today), not the yesterday booking
	// Wait, standard SQL query in queries.sql: WHERE rb.user_id = $1 AND rb.booking_date >= CURRENT_DATE
	for _, ub := range userABookings {
		assert.False(t, ub.BookingDate.Time.Before(today.Truncate(24*time.Hour)), "Should not find past bookings")
	}
}

func TestBooking_Integration_MidnightCrossing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tdb := testdb.NewPostgres(t)
	queries := tdb.DB.Queries
	campusID, userAID, _ := seedTestData(t, queries)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Europe/Moscow")

	today := time.Now().In(loc)
	todayDate := pgtype.Date{Time: today, Valid: true}

	// Booking at 23:30 for 60 minutes
	startTime := pgtype.Time{Microseconds: 23*60*60*1000000 + 30*60*1000000, Valid: true}
	_, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
		CampusID:        campusID,
		RoomID:          1,
		UserID:          userAID,
		BookingDate:     todayDate,
		StartTime:       startTime,
		DurationMinutes: 60,
	})
	assert.NoError(t, err)

	// Next day checkBookingConflict for 00:00-00:30
	tomorrow := today.AddDate(0, 0, 1)
	hasConflict := checkBookingConflict(ctx, queries, campusID, 1, tomorrow, 0, 30, loc)
	assert.True(t, hasConflict, "Booking from previous day crossing midnight should conflict with 00:00 slot")
}
