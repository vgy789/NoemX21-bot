package booking

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers booking-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("BOOKING_MENU", "booking.yaml/AUTO_SYNC_BOOKING")
	}

	registry.Register("get_booking_data", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, ok := payload["campus_id"].(string)
		if !ok || campusIDStr == "" || campusIDStr == "$context.campus_id" {
			// Try to recover from DB like clubs.go, or fail
			return "", nil, fmt.Errorf("campus_id missing")
		}

		var campusUUID pgtype.UUID
		if err := campusUUID.Scan(campusIDStr); err != nil {
			return "", nil, fmt.Errorf("invalid campus_id: %w", err)
		}

		// Get active rooms
		rooms, err := queries.GetActiveRoomsByCampus(ctx, campusUUID)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get rooms: %w", err)
		}

		if len(rooms) == 0 {
			return "", map[string]any{
				"free_slots_list": "😔 Нет доступных переговорок.",
			}, nil
		}

		// Calculate available slots for TODAY
		date := time.Now()
		// If payload has explicit date (e.g. from calendar), use it
		if dStr, ok := payload["selected_date"].(string); ok && dStr != "" {
			if parsed, err := time.Parse("2006-01-02", dStr); err == nil {
				date = parsed
			}
		}

		displayDate := date.Format("02.01.2006")

		vars := map[string]any{
			"current_date":  displayDate,
			"selected_date": displayDate,
		}

		type Slot struct {
			RoomID   int16
			RoomName string
			Time     string
			SortKey  string // "Time_RoomName"
		}
		var availableSlots []Slot

		// Define working hours (e.g. 10:00 - 22:00)
		startHour := 10
		endHour := 22

		// Fetch existing bookings for this day for ALL rooms in this campus
		// Optimization: query by campus+date, not per room
		// But currently we have GetRoomBookingsByDate which is per room.
		// I should ideally add GetCampusBookingsByDate, but let's iterate for now (assuming few rooms)
		bookingsMap := make(map[int16]map[string]bool) // roomID -> "HH:MM" -> true

		for _, room := range rooms {
			bookings, err := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
				CampusID:    campusUUID,
				RoomID:      room.ID,
				BookingDate: pgtype.Date{Time: date, Valid: true},
			})
			if err != nil {
				slog.Error("failed to get bookings", "room", room.Name, "error", err)
				continue
			}
			bm := make(map[string]bool)
			for _, b := range bookings {
				tStr := b.StartTime.Microseconds
				// pgtype.Time is microseconds since midnight.
				// Convert to HH:MM
				totalMin := tStr / 60000000
				hh := totalMin / 60
				mm := totalMin % 60
				timeStr := fmt.Sprintf("%02d:%02d", hh, mm)
				bm[timeStr] = true
				// Also mark subsequent slots if duration > 30?
				// Simple logic: slots are fixed 30 min?
				// If user booked 60 min at 10:00, then 10:00 and 10:30 are taken.
				durationMin := b.DurationMinutes
				slotsCovered := durationMin / 30
				for i := 1; i < int(slotsCovered); i++ {
					nextMin := int(totalMin) + i*30
					h := nextMin / 60
					m := nextMin % 60
					nextTimeStr := fmt.Sprintf("%02d:%02d", h, m)
					bm[nextTimeStr] = true
				}
			}
			bookingsMap[room.ID] = bm
		}

		currentHour := time.Now().Hour()
		currentMin := time.Now().Minute()
		isToday := date.Format("2006-01-02") == time.Now().Format("2006-01-02")

		for _, room := range rooms {
			for h := startHour; h < endHour; h++ {
				for _, m := range []int{0, 30} {
					// Check if past
					if isToday {
						if h < currentHour || (h == currentHour && m < currentMin) {
							continue
						}
					}

					timeStr := fmt.Sprintf("%02d:%02d", h, m)
					if bookingsMap[room.ID][timeStr] {
						continue
					}

					availableSlots = append(availableSlots, Slot{
						RoomID:   room.ID,
						RoomName: room.Name,
						Time:     timeStr,
						SortKey:  fmt.Sprintf("%s_%s", timeStr, room.Name),
					})
				}
			}
		}

		sort.Slice(availableSlots, func(i, j int) bool {
			return availableSlots[i].SortKey < availableSlots[j].SortKey
		})

		// Text list
		var sb strings.Builder
		if len(availableSlots) == 0 {
			sb.WriteString("😔 На эту дату всё занято.")
		} else {
			count := 0
			for _, s := range availableSlots {
				if count >= 10 { // limit text list
					sb.WriteString(fmt.Sprintf("\n... и ещё %d", len(availableSlots)-10))
					break
				}
				// Escape RoomName since it's outside the code block
				sb.WriteString(fmt.Sprintf("• `%s` %s\n", s.Time, fsm.EscapeMarkdown(s.RoomName)))
				count++
			}
		}
		vars["free_slots_list"] = sb.String()

		// Buttons (up to 12)
		maxButtons := 12
		for i := range maxButtons {
			idKey := fmt.Sprintf("slot_id_%d", i+1)
			lblKey := fmt.Sprintf("slot_label_%d", i+1)

			if i < len(availableSlots) {
				s := availableSlots[i]
				// ID format: slot_{room_id}_{time} e.g. slot_5_10:00
				vars[idKey] = fmt.Sprintf("slot_%d_%s", s.RoomID, s.Time)
				vars[lblKey] = fmt.Sprintf("⚡ %s | %s", s.Time, s.RoomName)
			} else {
				vars[idKey] = ""
				vars[lblKey] = ""
			}
		}

		return "", vars, nil
	})

	registry.Register("create_booking", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		roomIDVal := payload["room_id"] // could be string or number
		// dateStr from payload
		dateStr, _ := payload["date"].(string)
		timeStr, _ := payload["time"].(string) // "HH:MM"
		durationVal := payload["duration"]

		if campusIDStr == "" || roomIDVal == nil || dateStr == "" || timeStr == "" {
			return "", map[string]any{"success": false}, nil
		}

		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		roomID := toInt16(roomIDVal)
		duration := toInt32(durationVal)
		if duration == 0 {
			duration = 30
		} // default

		date, _ := time.Parse("02.01.2006", dateStr) // Display format was DD.MM.YYYY
		if date.IsZero() {
			// Try YYYY-MM-DD
			date, _ = time.Parse("2006-01-02", dateStr)
		}

		// timeStr is "HH:MM", need pgtype.Time (microseconds from midnight)
		tParsed, _ := time.Parse("15:04", timeStr)
		micros := int64(tParsed.Hour()*3600+tParsed.Minute()*60) * 1000000

		// Check if user account exists (implicit from userID)
		// Assuming userID is telegram ID, we need internal UserAccount ID
		// The queries.CreateRoomBooking uses user_id (bigint) which maps to user_accounts.id
		// payload should ideally contain internal user_id, or we fetch it.
		// Wait, FSM engine passes `userID` which `InitState` gets.
		// `get_user_stats` gets `acc` via `GetUserAccountByExternalId`.
		// FSM `userID` is typically external ID.
		// We need internal ID for FK.
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			slog.Error("failed to get user account", "error", err)
			return "", map[string]any{"success": false}, nil
		}

		_, err = queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
			CampusID:        campusUUID,
			RoomID:          roomID,
			UserID:          acc.ID,
			BookingDate:     pgtype.Date{Time: date, Valid: true},
			StartTime:       pgtype.Time{Microseconds: micros, Valid: true},
			DurationMinutes: duration,
		})

		if err != nil {
			// Could be unique constraint violation (conflict) -> err won't be nil if sqlc signatures?
			// But using `ON CONFLICT DO NOTHING`, sqlc logic depends.
			// If `RETURNING *` is used, and no row returned -> Scan error?
			// `CreateRoomBooking` returns `RoomBooking, error`.
			// If no rows, pgx returns `ErrNoRows`.
			slog.Error("create booking failed", "error", err)
			if strings.Contains(err.Error(), "no rows") { // ErrNoRows usually
				return "", map[string]any{"success": false}, nil
			}
			return "", map[string]any{"success": false}, nil
		}
		// Assuming if we got booking, it's successful.
		// Wait, if DO NOTHING triggered, booking might be empty structure?
		// pgx typically returns ErrNoRows if no rows returned.

		return "", map[string]any{"success": true}, nil
	})

	registry.Register("get_user_bookings", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		bookings, err := queries.GetUserRoomBookings(ctx, acc.ID)
		if err != nil {
			return "", nil, err
		}

		var sb strings.Builder
		if len(bookings) == 0 {
			sb.WriteString("Список пуст.")
		}

		vars := make(map[string]any)
		maxCancel := 5

		for i, b := range bookings {
			// Format: 02.01 15:00 RoomName (30m)
			t := b.StartTime.Microseconds / 60000000
			date := b.BookingDate.Time.Format("02.01")
			timeStr := fmt.Sprintf("%02d:%02d", t/60, t%60)

			// Escape RoomName
			sb.WriteString(fmt.Sprintf("%d. *%s* %s %s (%d min)\n", i+1, date, timeStr, fsm.EscapeMarkdown(b.RoomName), b.DurationMinutes))

			if i < maxCancel {
				kID := fmt.Sprintf("cancel_id_%d", i+1)
				kLbl := fmt.Sprintf("cancel_label_%d", i+1)
				vars[kID] = fmt.Sprintf("cancel_%d", b.ID)
				vars[kLbl] = fmt.Sprintf("❌ Отменить %s %s", date, timeStr)
			}
		}

		for i := len(bookings); i < maxCancel; i++ {
			vars[fmt.Sprintf("cancel_id_%d", i+1)] = ""
			vars[fmt.Sprintf("cancel_label_%d", i+1)] = ""
		}

		vars["my_bookings_list"] = sb.String()
		return "", vars, nil
	})

	// Add cancel action
	registry.Register("cancel_booking", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		// Parse booking ID from last_input "cancel_123"
		input, _ := payload["last_input"].(string)
		var id int64
		_, err := fmt.Sscanf(input, "cancel_%d", &id)
		if err != nil {
			// try direct payload if available?
			return "", nil, nil
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		err = queries.CancelRoomBooking(ctx, db.CancelRoomBookingParams{
			ID:     id,
			UserID: acc.ID,
		})
		if err != nil {
			slog.Error("failed to cancel", "id", id, "error", err)
		}
		return "FETCH_MY_BOOKINGS", nil, nil // Refresh list
	})
}

func toInt16(v any) int16 {
	switch val := v.(type) {
	case string:
		var i int16
		fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int16(val) // yaml parsing often gives float64
	case int:
		return int16(val)
	}
	return 0
}

func toInt32(v any) int32 {
	switch val := v.(type) {
	case string:
		var i int32
		fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int32(val)
	case int:
		return int32(val)
	}
	return 0
}
