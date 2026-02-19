package schedule_generator

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

func TestConvertRooms(t *testing.T) {
	loc, _ := time.LoadLocation("UTC")
	now := time.Date(2023, 10, 27, 0, 0, 0, 0, loc)

	dbRooms := []db.Room{
		{ID: 1, Name: "Room A", Capacity: 10},
		{ID: 2, Name: "Room B", Capacity: 5},
	}

	dbBookings := map[int16][]db.GetRoomBookingsByDateRow{
		1: {
			{
				ID:              101,
				BookingDate:     pgtype.Date{Time: now, Valid: true},
				StartTime:       pgtype.Time{Microseconds: 10 * 60 * 60 * 1000000, Valid: true}, // 10:00
				DurationMinutes: 60,
				Nickname:        "student1",
			},
		},
	}

	rooms := convertRooms(dbRooms, dbBookings, loc)

	assert.Len(t, rooms, 2)
	assert.Equal(t, "Room A", rooms[0].Name)
	assert.Equal(t, "10 МЕСТ", rooms[0].Capacity)
	assert.Len(t, rooms[0].Bookings, 1)

	b := rooms[0].Bookings[0]
	assert.Equal(t, "student1", b.Nickname)
	assert.Equal(t, now.Add(10*time.Hour), b.Start)
	assert.Equal(t, now.Add(11*time.Hour), b.End)

	assert.Equal(t, "Room B", rooms[1].Name)
	assert.Equal(t, "5 МЕСТ", rooms[1].Capacity)
	assert.Empty(t, rooms[1].Bookings)
}
