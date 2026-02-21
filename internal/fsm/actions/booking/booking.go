package booking

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

	registry.Register("get_dashboard_data", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return getBookingData(ctx, queries, userID, payload, cfg)
	})

	registry.Register("get_booking_data", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return getBookingData(ctx, queries, userID, payload, cfg)
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
			_, _ = fmt.Sscanf(roomIDStr, "%d", &rid)

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
		if err := campusUUID.Scan(campusIDStr); err != nil {
			return "", map[string]any{"success": false, "error": "invalid_campus"}, nil
		}

		// Resolve timezone
		loc := getUserTimezone(ctx, queries, userID, campusUUID)

		roomID := toInt16(roomIDVal)
		duration := toInt32(durationVal)
		if duration == 0 {
			duration = 30
		}

		date, err := time.ParseInLocation("02.01.2006", dateStr, loc)
		if err != nil {
			return "", map[string]any{"success": false, "error": "invalid_date"}, nil
		}
		tParsed, err := time.ParseInLocation("15:04", timeStr, loc)
		if err != nil {
			return "", map[string]any{"success": false, "error": "invalid_time"}, nil
		}
		startMin := int64(tParsed.Hour()*60 + tParsed.Minute())
		micros := startMin * 60000000
		endMin := startMin + int64(duration)

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		// Check for conflicts before creating booking
		hasConflict := checkBookingConflict(ctx, queries, campusUUID, roomID, date, startMin, endMin, loc)
		if hasConflict {
			return "", map[string]any{"success": false, "conflict": true}, nil
		}

		_, err = queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
			CampusID: campusUUID, RoomID: roomID, UserID: acc.ID,
			BookingDate:     pgtype.Date{Time: date, Valid: true},
			StartTime:       pgtype.Time{Microseconds: micros, Valid: true},
			DurationMinutes: duration,
		})
		if err != nil {
			return "", map[string]any{"success": false, "error": "create_failed"}, nil
		}

		// Trigger schedule regeneration
		if scheduleRegen != nil {
			scheduleRegen.ForceRegenerate()
		}

		roomName, _ := payload["room_name"].(string)
		endT := tParsed.Add(time.Duration(duration) * time.Minute)
		// Format end time, handling midnight crossing
		timeInterval := fmt.Sprintf("%s–%s", timeStr, endT.Format("15:04"))

		return "", map[string]any{
			"success": true, "room_name": roomName, "time_interval": timeInterval,
		}, nil
	})

	registry.Register("get_user_bookings", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", map[string]any{"active_bookings_list": "Список пуст.", "future_bookings_list": "Список пуст."}, nil
		}
		bookings, _ := queries.GetUserRoomBookings(ctx, acc.ID)
		loc := getUserTimezone(ctx, queries, userID, pgtype.UUID{})
		var activeSb, futureSb strings.Builder
		vars := make(map[string]any)
		now := time.Now().In(loc)
		nowMinutes := int64(now.Hour()*60 + now.Minute())
		activeCount, futureCount := 0, 0
		for _, b := range bookings {
			startMin := b.StartTime.Microseconds / 60000000
			endMin := startMin + int64(b.DurationMinutes)
			bookingDate := b.BookingDate.Time.In(loc)
			isToday := bookingDate.Year() == now.Year() && bookingDate.YearDay() == now.YearDay()
			isStarted := isToday && nowMinutes >= startMin && nowMinutes < endMin
			isFuture := !isToday || (isToday && startMin > nowMinutes)
			timeStr := fmt.Sprintf("%02d:%02d", startMin/60, startMin%60)
			if isStarted {
				activeCount++
				timeLeft := endMin - nowMinutes
				activeSb.WriteString(fmt.Sprintf("%s (%s)\n⏳ До %s (осталось %d мин)\n", fsm.EscapeMarkdown(b.RoomName), b.CampusShortName, calculateEndTime(timeStr, b.DurationMinutes), timeLeft))
				vars["finish_early_id"] = fmt.Sprintf("release_%d", b.ID)
				vars["room_name"] = b.RoomName
				vars["minutes_left"] = timeLeft
				vars["booking_id"] = b.ID
			} else if isFuture {
				futureCount++
				dayLabel := bookingDate.Format("02.01")
				if isToday {
					dayLabel = "Сегодня"
				} else if bookingDate.YearDay() == now.AddDate(0, 0, 1).YearDay() {
					dayLabel = "Завтра"
				}
				futureSb.WriteString(fmt.Sprintf("%s | %s, %s — %s\n", fsm.EscapeMarkdown(b.RoomName), dayLabel, timeStr, calculateEndTime(timeStr, b.DurationMinutes)))
				vars["cancel_future_id"] = fmt.Sprintf("cancel_%d", b.ID)
				vars["date"] = bookingDate.Format("02.01.2006")
				vars["time_start"] = timeStr
				vars["time_end"] = calculateEndTime(timeStr, b.DurationMinutes)
			}
		}
		if activeCount == 0 {
			activeSb.WriteString("Нет активных броней.")
		}
		if futureCount == 0 {
			futureSb.WriteString("Нет будущих броней.")
		}
		vars["active_bookings_list"] = activeSb.String()
		vars["future_bookings_list"] = futureSb.String()
		vars["bookings_count"] = activeCount + futureCount

		// Ensure we clear these if they were set previously but are not present now
		if activeCount == 0 {
			vars["finish_early_id"] = ""
			vars["room_name"] = ""
			vars["minutes_left"] = 0
			vars["booking_id"] = 0
		}
		if futureCount == 0 {
			vars["cancel_future_id"] = ""
			vars["date"] = ""
			vars["time_start"] = ""
			vars["time_end"] = ""
		}

		return "", vars, nil
	})

	registry.Register("find_nearest_room", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		var campusUUID pgtype.UUID
		if err := campusUUID.Scan(campusIDStr); err != nil {
			return "", nil, fmt.Errorf("invalid campus_id")
		}
		rooms, err := queries.GetActiveRoomsByCampus(ctx, campusUUID)
		if err != nil || len(rooms) == 0 {
			return "", nil, fmt.Errorf("no rooms available")
		}
		room := rooms[0]

		vars := map[string]any{"room_id": room.ID, "room_name": room.Name}
		vars["dashboard_visualization"] = getVisualizationPath(ctx, queries, campusUUID, cfg)

		return "", vars, nil
	})

	registry.Register("process_duration_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		msg, _ := payload["message"].(map[string]any)
		text, _ := msg["text"].(string)

		// Fallback for button clicks where text might be in id or last_input
		if text == "" {
			if id, ok := payload["id"].(string); ok && id != "" {
				text = id
			} else if li, ok := payload["last_input"].(string); ok && li != "" {
				text = li
			}
		}

		var duration int
		if strings.HasPrefix(text, "set_") && strings.HasSuffix(text, "_min") {
			_, _ = fmt.Sscanf(text, "set_%d_min", &duration)
		} else {
			duration = int(toInt32(strings.TrimSpace(text)))
		}

		if duration <= 0 {
			return "FAST_BOOK_ERROR_NONDIGIT", nil, nil
		}
		rounded := ((duration + 7) / 15) * 15
		if rounded == 0 {
			rounded = 15
		}
		return "", map[string]any{"duration": rounded}, nil
	})

	registry.Register("create_booking_with_duration", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		roomIDVal := payload["room_id"]
		durationVal := payload["duration"]
		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)
		loc := getUserTimezone(ctx, queries, userID, campusUUID)
		now := time.Now().In(loc)
		roomID := toInt16(roomIDVal)
		duration := toInt32(durationVal)
		if duration == 0 {
			duration = 30 // Fallback
		}
		if duration > 120 {
			return "", map[string]any{"success": false, "error": "limit_exceeded"}, nil
		}
		startMin := int64(now.Hour()*60 + now.Minute())
		micros := startMin * 60000000
		endMin := startMin + int64(duration)
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}
		if checkBookingConflict(ctx, queries, campusUUID, roomID, now, startMin, endMin, loc) {
			bookings, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
				CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: now, Valid: true},
			})
			nextBookingTime := "23:59"
			minStart := int64(1440)
			for _, b := range bookings {
				bStart := b.StartTime.Microseconds / 60000000
				if bStart > startMin && bStart < minStart {
					minStart = bStart
					nextBookingTime = fmt.Sprintf("%02d:%02d", bStart/60, bStart%60)
				}
			}
			availableMins := int32(minStart - startMin)
			if availableMins < 5 {
				return "", map[string]any{"success": false, "error": "overlap", "available_minutes": 0, "next_booking_time": nextBookingTime}, nil
			}
			return "", map[string]any{"success": false, "error": "overlap", "available_minutes": availableMins, "next_booking_time": nextBookingTime, "requested_minutes": duration}, nil
		}
		if _, err := queries.CreateRoomBooking(ctx, db.CreateRoomBookingParams{
			CampusID: campusUUID, RoomID: roomID, UserID: acc.ID,
			BookingDate:     pgtype.Date{Time: now, Valid: true},
			StartTime:       pgtype.Time{Microseconds: micros, Valid: true},
			DurationMinutes: duration,
		}); err != nil {
			return "", map[string]any{"success": false, "error": "create_failed"}, nil
		}

		// Trigger schedule regeneration
		if scheduleRegen != nil {
			scheduleRegen.ForceRegenerate()
		}

		roomName, _ := payload["room_name"].(string)
		return "", map[string]any{"success": true, "room_name": roomName, "duration": duration, "end_time": calculateEndTime(fmt.Sprintf("%02d:%02d", startMin/60, startMin%60), duration)}, nil
	})

	registry.Register("find_room_by_duration", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		requestedDuration := toInt32(payload["requested_minutes"])
		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)
		loc := getUserTimezone(ctx, queries, userID, campusUUID)
		now := time.Now().In(loc)
		startMin := int64(now.Hour()*60 + now.Minute())
		endMin := startMin + int64(requestedDuration)
		rooms, _ := queries.GetActiveRoomsByCampus(ctx, campusUUID)
		for _, room := range rooms {
			if !checkBookingConflict(ctx, queries, campusUUID, room.ID, now, startMin, endMin, loc) {
				return "", map[string]any{"room_id": room.ID, "room_name": room.Name}, nil
			}
		}
		return "BOOKING_DASHBOARD", nil, nil
	})

	registry.Register("release_booking_early", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		bookingID := toInt64(payload["booking_id"])
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}
		if bookingID == 0 {
			input, _ := payload["last_input"].(string)
			fmt.Sscanf(input, "release_%d", &bookingID)
		}
		if err := queries.CancelRoomBooking(ctx, db.CancelRoomBookingParams{ID: bookingID, UserID: acc.ID}); err != nil {
			return "", map[string]any{"success": false, "error": "cancel_failed"}, nil
		}
		// Trigger schedule regeneration
		if scheduleRegen != nil {
			scheduleRegen.ForceRegenerate()
		}
		return "", map[string]any{"success": true}, nil
	})

	registry.Register("cancel_booking", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		input, _ := payload["last_input"].(string)
		var id int64
		fmt.Sscanf(input, "cancel_%d", &id)
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "FETCH_MY_BOOKINGS", nil, nil
		}
		if err := queries.CancelRoomBooking(ctx, db.CancelRoomBookingParams{ID: id, UserID: acc.ID}); err != nil {
			return "FETCH_MY_BOOKINGS", map[string]any{"success": false, "error": "cancel_failed"}, nil
		}
		// Trigger schedule regeneration
		if scheduleRegen != nil {
			scheduleRegen.ForceRegenerate()
		}
		return "FETCH_MY_BOOKINGS", map[string]any{"success": true}, nil
	})
}

func getBookingData(ctx context.Context, queries db.Querier, userID int64, payload map[string]any, cfg *config.Config) (string, map[string]any, error) {
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
	reset, _ := payload["reset"].(bool)
	if !reset {
		if dStr, ok := payload["selected_date"].(string); ok && dStr != "" {
			if p, err := time.ParseInLocation("2006-01-02", dStr, loc); err == nil {
				date = p
			} else if p, err := time.ParseInLocation("02.01.2006", dStr, loc); err == nil {
				date = p
			}
		}
	}

	displayDate := date.Format("02.01.2006")

	campusName := "Campus"
	if c, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
		campusName = c.ShortName
	}

	// Get user account for fetching their bookings
	acc, accErr := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})

	vars := map[string]any{
		"current_date": displayDate, "selected_date": displayDate,
		"my_campus": campusName, "campus_id": campusIDStr,
	}

	vars["dashboard_visualization"] = getVisualizationPath(ctx, queries, campusUUID, cfg)

	// Fetch user's active bookings
	var activeBookingReminder string
	bookingsCount := 0
	if accErr == nil {
		bookings, _ := queries.GetUserRoomBookings(ctx, acc.ID)
		now := time.Now().In(loc)
		nowMinutes := int64(now.Hour()*60 + now.Minute())
		for _, b := range bookings {
			startMin := b.StartTime.Microseconds / 60000000
			endMin := startMin + int64(b.DurationMinutes)
			bookingDate := b.BookingDate.Time.In(loc)

			isToday := bookingDate.Year() == now.Year() && bookingDate.YearDay() == now.YearDay()
			isStarted := isToday && nowMinutes >= startMin && nowMinutes < endMin
			isFuture := !isToday || (isToday && startMin > nowMinutes)

			if isStarted || isFuture {
				bookingsCount++
			}

			if isStarted {
				if activeBookingReminder == "" {
					activeBookingReminder = fmt.Sprintf("🔥 *СЕЙЧАС ИДЕТ:*\n%s (%s)\n⏳ До %s (осталось %d мин)\n\n---\n",
						fsm.EscapeMarkdown(b.RoomName), b.CampusShortName, calculateEndTime(fmt.Sprintf("%02d:%02d", startMin/60, startMin%60), b.DurationMinutes), endMin-nowMinutes)
				}
			} else if isToday && startMin > nowMinutes && startMin < nowMinutes+60 {
				if activeBookingReminder == "" {
					activeBookingReminder = fmt.Sprintf("📅 *СКОРО НАЧНЕТСЯ:*\n%s в %02d:%02d\n\n---\n",
						fsm.EscapeMarkdown(b.RoomName), startMin/60, startMin%60)
				}
			}
		}
	}
	vars["active_booking_reminder_logic"] = activeBookingReminder
	vars["bookings_count"] = bookingsCount

	if activeBookingReminder != "" {
		vars["dashboard_text"] = activeBookingReminder
		vars["dashboard_text_en"] = activeBookingReminder // Simplified
	} else {
		vars["dashboard_text"] = "🤷‍♂️ У тебя пока нет активных броней.\n\nСамое время занять уютный уголок и поработать!"
		vars["dashboard_text_en"] = "🤷‍♂️ You don't have any active bookings.\n\nTime to grab a cozy corner and get to work!"
	}

	// Always show book_now as primary button
	vars["primary_button_id"] = "book_now"
	vars["primary_button_label"] = "⚡️ Комната СЕЙЧАС"
	vars["primary_button_next_state"] = "FAST_BOOK_DURATION"

	// These variables are no longer used for buttons, clear them
	vars["smart_release_id"] = ""
	vars["smart_release_label"] = ""

	type Slot struct {
		RoomID   int16
		RoomName string
		Time     string
		SortKey  string
	}
	var availableSlots []Slot
	bookingsMap := make(map[int16]map[string]bool)

	// Build bookings map for the selected date
	// Also consider bookings from previous day that cross midnight
	prevDate := date.AddDate(0, 0, -1)

	for _, room := range rooms {
		bm := make(map[string]bool)

		// Get bookings for the selected date
		bsCurrent, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
			CampusID: campusUUID, RoomID: room.ID, BookingDate: pgtype.Date{Time: date, Valid: true},
		})
		for _, b := range bsCurrent {
			markBookedSlotsFromRow(b, bm)
		}

		// Get bookings from previous day that might cross midnight (start >= 23:00)
		bsPrev, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
			CampusID: campusUUID, RoomID: room.ID, BookingDate: pgtype.Date{Time: prevDate, Valid: true},
		})
		for _, b := range bsPrev {
			// Check if this booking crosses midnight
			startMin := b.StartTime.Microseconds / 60000000
			endMin := startMin + int64(b.DurationMinutes)
			// Only relevant if it started late yesterday and ends today (effectively > 24*60 relative to yesterday start)
			if endMin > 24*60 {
				// Minutes falling into today
				minsIntoToday := endMin - 24*60
				// Mark slots from 00:00 up to minsIntoToday
				for t := int64(0); t < minsIntoToday; t += 30 {
					h := t / 60
					m := t % 60
					bm[fmt.Sprintf("%02d:%02d", h, m)] = true
				}
			}
		}

		bookingsMap[room.ID] = bm
	}

	now := time.Now().In(loc)
	// Generate slots from 10:00 to 23:30 (inclusive)
	for _, room := range rooms {
		for h := 10; h <= 23; h++ {
			for _, m := range []int{0, 30} {
				ts := fmt.Sprintf("%02d:%02d", h, m)
				// Skip past slots for today
				if date.Format("02.01.2006") == now.Format("02.01.2006") {
					if h < now.Hour() || (h == now.Hour() && m <= now.Minute()) {
						continue
					}
				}
				if !bookingsMap[room.ID][ts] {
					availableSlots = append(availableSlots, Slot{RoomID: room.ID, RoomName: room.Name, Time: ts, SortKey: ts + "_" + room.Name})
				}
			}
		}
	}

	// Generate slots 00:00 and 00:30 for the next day (for bookings crossing midnight)
	// Only show these slots if viewing a future date
	dateMidnight := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	nowMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if dateMidnight.After(nowMidnight) || (dateMidnight.Equal(nowMidnight) && now.Hour() < 1) {
		for _, room := range rooms {
			for _, m := range []int{0, 30} {
				ts := fmt.Sprintf("00:%02d", m)
				// Skip if this slot is booked by a previous day's booking
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
		_, _ = fmt.Sscanf(val, "%d", &i)
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

// markBookedSlotsFromRow marks all 30-minute slots as booked for a given booking row.
func markBookedSlotsFromRow(b db.GetRoomBookingsByDateRow, bm map[string]bool) {
	totalMin := b.StartTime.Microseconds / 60000000
	for i := 0; i < int(b.DurationMinutes); i += 30 {
		nextMin := int(totalMin) + i
		h := nextMin / 60
		m := nextMin % 60
		if h < 24 { // Only mark slots within the same day
			bm[fmt.Sprintf("%02d:%02d", h, m)] = true
		} else {
			// Crosses midnight
			hAfter := h % 24
			bm[fmt.Sprintf("%02d:%02d", hAfter, m)] = true
		}
	}
}

// calculateEndTime calculates the end time given a start time and duration.
func calculateEndTime(startTime string, durationMinutes int32) string {
	t, err := time.Parse("15:04", startTime)
	if err != nil {
		return startTime
	}
	endT := t.Add(time.Duration(durationMinutes) * time.Minute)
	return endT.Format("15:04")
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
	bsCurrent, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
		CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: date, Valid: true},
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
		bsNext, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
			CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: nextDate, Valid: true},
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
		bsPrev, _ := queries.GetRoomBookingsByDate(ctx, db.GetRoomBookingsByDateParams{
			CampusID: campusUUID, RoomID: roomID, BookingDate: pgtype.Date{Time: prevDate, Valid: true},
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

func getVisualizationPath(ctx context.Context, queries db.Querier, campusUUID pgtype.UUID, cfg *config.Config) string {
	if cfg == nil || !cfg.ScheduleImages.Enabled || !campusUUID.Valid {
		return ""
	}
	c, err := queries.GetCampusByID(ctx, campusUUID)
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
