package booking

import (
	"context"
	"fmt"
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

	// Alias for spec compliance
	registry.Register("get_dashboard_data", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return getBookingData(ctx, queries, userID, payload)
	})

	registry.Register("get_booking_data", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return getBookingData(ctx, queries, userID, payload)
	})

	registry.Register("resolve_slot_from_last_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		lastInput, _ := payload["last_input"].(string) // e.g., "slot_5_10:00"
		if !strings.HasPrefix(lastInput, "slot_") {
			return "", nil, nil
		}

		parts := strings.Split(lastInput, "_")
		if len(parts) < 3 {
			return "", nil, nil
		}

		roomIDStr := parts[1]
		timeStr := parts[2]

		// Try to find room name from context if available
		roomName := ""
		// Loop through possible slots in context to find matching room ID and time
		for i := 1; i <= 12; i++ {
			ctxSlotID, _ := payload[fmt.Sprintf("slot_id_%d", i)].(string) // "slot_5_10:00"

			if ctxSlotID == lastInput {
				ctxRoomName, _ := payload[fmt.Sprintf("slot_room_name_%d", i)].(string)
				ctxTime, _ := payload[fmt.Sprintf("slot_time_%d", i)].(string)
				roomName = ctxRoomName
				if ctxTime != "" {
					timeStr = ctxTime
				}
				break
			}
		}

		if roomName == "" {
			var rid int16
			fmt.Sscanf(roomIDStr, "%d", &rid)

			campusIDStr, _ := payload["campus_id"].(string)
			if campusIDStr != "" {
				var campusUUID pgtype.UUID
				if err := campusUUID.Scan(campusIDStr); err == nil {
					r, err := queries.GetRoomByID(ctx, db.GetRoomByIDParams{CampusID: campusUUID, ID: rid})
					if err == nil {
						roomName = r.Name
					}
				}
			}
			if roomName == "" {
				roomName = fmt.Sprintf("Room #%s", roomIDStr)
			}
		}

		return "", map[string]any{
			"room_id":    roomIDStr,
			"room_name":  roomName,
			"start_time": timeStr,
			"time":       timeStr,
		}, nil
	})

	registry.Register("create_booking", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		roomIDVal := payload["room_id"]
		dateStr, _ := payload["date"].(string)
		timeStr, _ := payload["time"].(string)
		durationVal := payload["duration"]

		if dateStr == "" {
			if s, ok := payload["selected_date"].(string); ok {
				dateStr = s
			} else if s, ok := payload["current_date"].(string); ok {
				dateStr = s
			} else {
				dateStr = time.Now().Format("02.01.2006")
			}
		}

		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		// Resolve timezone
		loc := getUserTimezone(ctx, queries, userID, campusUUID)

		roomID := toInt16(roomIDVal)
		duration := toInt32(durationVal)
		if duration == 0 {
			duration = 30
		}

		date, _ := time.ParseInLocation("02.01.2006", dateStr, loc)
		tParsed, _ := time.ParseInLocation("15:04", timeStr, loc)
		micros := int64(tParsed.Hour()*3600+tParsed.Minute()*60) * 1000000

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		_, err = queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
			CampusID: campusUUID, RoomID: roomID, UserID: acc.ID,
			BookingDate:     pgtype.Date{Time: date, Valid: true},
			StartTime:       pgtype.Time{Microseconds: micros, Valid: true},
			DurationMinutes: duration,
		})
		if err != nil {
			return "", map[string]any{"success": false}, nil
		}

		roomName, _ := payload["room_name"].(string)
		endT := tParsed.Add(time.Duration(duration) * time.Minute)
		timeInterval := fmt.Sprintf("%s–%s", timeStr, endT.Format("15:04"))

		return "", map[string]any{
			"success": true, "room_name": roomName, "time_interval": timeInterval,
		}, nil
	})

	registry.Register("get_user_bookings", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, _ := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		bookings, _ := queries.GetUserRoomBookings(ctx, acc.ID)

		// We need a campus ID to resolve timezone, but bookings might be across campuses.
		// For simplicity, we use the user's "primary"/current campus timezone or just the first booking's campus.
		var loc *time.Location = time.UTC
		if len(bookings) > 0 {
			loc = getUserTimezone(ctx, queries, userID, bookings[0].CampusID)
		}

		var sb strings.Builder
		if len(bookings) == 0 {
			sb.WriteString("Список пуст.")
		}
		vars := make(map[string]any)
		for i, b := range bookings {
			t := b.StartTime.Microseconds / 60000000
			date := b.BookingDate.Time.In(loc).Format("02.01")
			timeStr := fmt.Sprintf("%02d:%02d", t/60, t%60)
			sb.WriteString(fmt.Sprintf("*%d.* *%s* %s %s (%d min)\n", i+1, date, timeStr, fsm.EscapeMarkdown(b.RoomName), b.DurationMinutes))
			if i < 5 {
				vars[fmt.Sprintf("cancel_id_%d", i+1)] = fmt.Sprintf("cancel_%d", b.ID)
				vars[fmt.Sprintf("cancel_label_%d", i+1)] = fmt.Sprintf("❌ Отменить %s %s", date, timeStr)
			}
		}
		vars["my_bookings_list"] = sb.String()
		vars["my_bookings_formatted"] = sb.String()
		return "", vars, nil
	})

	registry.Register("cancel_booking", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		input, _ := payload["last_input"].(string)
		var id int64
		fmt.Sscanf(input, "cancel_%d", &id)
		acc, _ := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		_ = queries.CancelRoomBooking(ctx, db.CancelRoomBookingParams{ID: id, UserID: acc.ID})
		return "FETCH_MY_BOOKINGS", nil, nil
	})
}

func getBookingData(ctx context.Context, queries db.Querier, userID int64, payload map[string]any) (string, map[string]any, error) {
	campusIDStr, _ := payload["campus_id"].(string)
	if campusIDStr == "" || campusIDStr == "$context.campus_id" {
		return "", nil, fmt.Errorf("campus_id missing")
	}

	var campusUUID pgtype.UUID
	_ = campusUUID.Scan(campusIDStr)

	// Resolve timezone
	loc := getUserTimezone(ctx, queries, userID, campusUUID)

	rooms, _ := queries.GetActiveRoomsByCampus(ctx, campusUUID)

	if len(rooms) == 0 {
		return "", map[string]any{
			"free_slots_list":  "😔 Нет доступных переговорок.",
			"hot_slots_list":   "😔 Нет доступных переговорок.",
			"free_rooms_count": 0, "busy_rooms_count": 0,
		}, nil
	}

	date := time.Now().In(loc)
	if dStr, ok := payload["selected_date"].(string); ok && dStr != "" {
		if p, err := time.ParseInLocation("2006-01-02", dStr, loc); err == nil {
			date = p
		} else if p, err := time.ParseInLocation("02.01.2006", dStr, loc); err == nil {
			date = p
		}
	}
	displayDate := date.Format("02.01.2006")

	campusName := "Campus"
	if c, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
		campusName = c.ShortName
	}

	vars := map[string]any{
		"current_date": displayDate, "selected_date": displayDate,
		"my_campus": campusName, "campus_id": campusIDStr,
	}

	type Slot struct {
		RoomID   int16
		RoomName string
		Time     string
		SortKey  string
	}
	var availableSlots []Slot
	bookingsMap := make(map[int16]map[string]bool)

	for _, room := range rooms {
		bs, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
			CampusID: campusUUID, RoomID: room.ID, BookingDate: pgtype.Date{Time: date, Valid: true},
		})
		bm := make(map[string]bool)
		for _, b := range bs {
			totalMin := b.StartTime.Microseconds / 60000000
			for i := 0; i < int(b.DurationMinutes/30); i++ {
				nextMin := int(totalMin) + i*30
				bm[fmt.Sprintf("%02d:%02d", nextMin/60, nextMin%60)] = true
			}
		}
		bookingsMap[room.ID] = bm
	}

	now := time.Now().In(loc)
	for _, room := range rooms {
		for h := 10; h < 22; h++ {
			for _, m := range []int{0, 30} {
				if date.Format("02.01.2006") == now.Format("02.01.2006") {
					if h < now.Hour() || (h == now.Hour() && m < now.Minute()) {
						continue
					}
				}
				ts := fmt.Sprintf("%02d:%02d", h, m)
				if !bookingsMap[room.ID][ts] {
					availableSlots = append(availableSlots, Slot{RoomID: room.ID, RoomName: room.Name, Time: ts, SortKey: ts + "_" + room.Name})
				}
			}
		}
	}

	sort.Slice(availableSlots, func(i, j int) bool { return availableSlots[i].SortKey < availableSlots[j].SortKey })
	vars["free_rooms_count"] = len(availableSlots)
	vars["busy_rooms_count"] = 0

	var sb strings.Builder
	for i, s := range availableSlots {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("\n... и ещё %d", len(availableSlots)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("• `%s` %s\n", s.Time, fsm.EscapeMarkdown(s.RoomName)))
	}
	vars["free_slots_list"] = sb.String()
	vars["hot_slots_list"] = sb.String()

	for i := range 12 {
		ik := fmt.Sprintf("slot_id_%d", i+1)
		lk := fmt.Sprintf("slot_label_%d", i+1)
		rnk := fmt.Sprintf("slot_room_name_%d", i+1)
		tk := fmt.Sprintf("slot_time_%d", i+1)
		if i < len(availableSlots) {
			s := availableSlots[i]
			vars[ik] = fmt.Sprintf("slot_%d_%s", s.RoomID, s.Time)
			vars[lk] = fmt.Sprintf("⚡ %s | %s", s.Time, s.RoomName)
			vars[rnk] = s.RoomName
			vars[tk] = s.Time
		} else {
			vars[ik] = ""
			vars[lk] = ""
			vars[rnk] = ""
			vars[tk] = ""
		}
	}
	return "", vars, nil
}

func getUserTimezone(ctx context.Context, queries db.Querier, userID int64, campusUUID pgtype.UUID) *time.Location {
	defaultLoc := time.UTC
	if moscow, err := time.LoadLocation("Europe/Moscow"); err == nil {
		defaultLoc = moscow
	}

	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err == nil {
		if u, err := queries.GetRegisteredUserByS21Login(ctx, acc.S21Login); err == nil {
			if u.Timezone != "" {
				if loc, err := time.LoadLocation(u.Timezone); err == nil {
					return loc
				}
			}
		}
	}

	if campusUUID.Valid {
		if c, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
			if c.Timezone.Valid && c.Timezone.String != "" {
				if loc, err := time.LoadLocation(c.Timezone.String); err == nil {
					return loc
				}
			}
		}
	}

	return defaultLoc
}

func toInt16(v any) int16 {
	switch val := v.(type) {
	case string:
		var i int16
		fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int16(val)
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
