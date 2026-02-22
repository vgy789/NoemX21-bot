package schedule

import "time"

// Booking represents a single room booking.
type Booking struct {
	Start       time.Time
	End         time.Time
	Nickname    string
	Description string
}

// Room represents a campus room with its bookings.
type Room struct {
	Name     string
	Capacity string
	Bookings []Booking
}
