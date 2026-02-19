package schedule_generator

import (
	"fmt"
	"time"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/pkg/schedule"
)

func convertRooms(dbRooms []db.Room, dbBookings map[int16][]db.GetRoomBookingsByDateRow, loc *time.Location) []schedule.Room {
	rooms := make([]schedule.Room, 0, len(dbRooms))
	for _, dr := range dbRooms {
		r := schedule.Room{
			Name:     dr.Name,
			Capacity: fmt.Sprintf("%d МЕСТ", dr.Capacity),
		}

		dbBks := dbBookings[dr.ID]
		r.Bookings = make([]schedule.Booking, 0, len(dbBks))
		for _, dbk := range dbBks {
			start := time.Date(
				dbk.BookingDate.Time.Year(),
				dbk.BookingDate.Time.Month(),
				dbk.BookingDate.Time.Day(),
				0, 0, 0, 0, loc,
			).Add(time.Duration(dbk.StartTime.Microseconds) * time.Microsecond)

			end := start.Add(time.Duration(dbk.DurationMinutes) * time.Minute)

			r.Bookings = append(r.Bookings, schedule.Booking{
				Start:       start,
				End:         end,
				Nickname:    dbk.Nickname,
				Description: "", // Description is not in room_bookings table currently
			})
		}
		rooms = append(rooms, r)
	}
	return rooms
}
