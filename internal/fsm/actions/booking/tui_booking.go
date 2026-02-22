package booking

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

const (
	dbQueryTimeout       = 3 * time.Second
	tuiLineLimit         = 26
	minBookingMinutes    = 5
	roomFreeGapMin       = 15
	roomSoonWindowMin    = 10
	maxRoomButtons       = 8
	maxManageBookButtons = 8
	maxPlanDayButtons    = 3
	maxPlanTimeButtons   = 3
)

type roomState string

const (
	roomStateFree roomState = "free"
	roomStateSoon roomState = "soon"
	roomStateBusy roomState = "busy"
)

type bookingInterval struct {
	Start time.Time
	End   time.Time
}

type roomAvailability struct {
	State         roomState
	AvailableFrom time.Time
	GapMinutes    int
	NextStart     time.Time
}

func registerTUIActions(
	registry *fsm.LogicRegistry,
	queries db.Querier,
	cfg *config.Config,
	scheduleRegen ScheduleRegenerator,
) {
	registry.Register("get_dashboard_data", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return getDashboardTUI(ctx, queries, cfg, userID, payload)
	})

	registry.Register("prepare_room_selection", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return prepareRoomSelectionTUI(ctx, queries, userID, payload)
	})
	registry.Register("prepare_plan_day", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return preparePlanDayTUI(ctx, queries, userID, payload)
	})
	registry.Register("plan_select_day", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return planSelectDayTUI(ctx, queries, userID, payload)
	})
	registry.Register("prepare_plan_time", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return preparePlanTimeTUI(ctx, queries, userID, payload)
	})
	registry.Register("plan_process_time", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return planProcessTimeTUI(ctx, queries, userID, payload)
	})
	registry.Register("prepare_plan_duration", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return preparePlanDurationTUI(ctx, queries, userID, payload)
	})
	registry.Register("plan_process_duration", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return planProcessDurationTUI(ctx, payload)
	})
	registry.Register("prepare_plan_rooms", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return preparePlanRoomsTUI(ctx, queries, userID, payload)
	})
	registry.Register("plan_select_room", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return planSelectRoomTUI(ctx, queries, userID, payload)
	})

	registry.Register("prepare_duration_choice", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return prepareDurationChoiceTUI(ctx, queries, userID, payload)
	})

	registry.Register("process_duration_input", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return processDurationInputTUI(ctx, queries, userID, payload)
	})

	registry.Register("create_booking_with_duration", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return createBookingWithDurationTUI(ctx, queries, userID, payload, scheduleRegen)
	})

	registry.Register("get_user_bookings", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return getUserBookingsTUI(ctx, queries, cfg, userID, payload)
	})

	registry.Register("cancel_booking", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return manageBookingTUI(ctx, queries, userID, payload, scheduleRegen)
	})

	registry.Register("release_booking_early", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		bookingID := toInt64(payload["booking_id"])
		if bookingID == 0 {
			input, _ := payload["last_input"].(string)
			if after, ok := strings.CutPrefix(input, "finish_"); ok {
				bookingID = toInt64(after)
			}
		}
		if bookingID == 0 {
			return "", map[string]any{"success": false}, nil
		}
		return manageBookingTUI(ctx, queries, userID, map[string]any{
			"last_input": fmt.Sprintf("finish_%d", bookingID),
		}, scheduleRegen)
	})
}

// RoundTime rounds to the nearest 5 minutes and enforces a 5-minute minimum.
func RoundTime(minutes int) int {
	if minutes < minBookingMinutes {
		return minBookingMinutes
	}

	remainder := minutes % 5
	if remainder >= 3 {
		minutes += 5 - remainder
	} else {
		minutes -= remainder
	}
	if minutes < minBookingMinutes {
		return minBookingMinutes
	}
	return minutes
}

// GetAvailableWindow applies sliding-window logic.
func GetAvailableWindow(requestedMinutes, gapMinutes int) int {
	if gapMinutes <= 0 {
		return 0
	}
	if requestedMinutes <= 0 {
		return gapMinutes
	}
	if requestedMinutes > gapMinutes {
		return gapMinutes
	}
	return requestedMinutes
}

func getDashboardTUI(
	ctx context.Context,
	queries db.Querier,
	cfg *config.Config,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, campusIDStr, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)

	lines := make([]string, 0, 8)
	bookingsCount := 0

	acc, err := getAccountByExternalID(ctx, queries, userID)
	if err == nil {
		bookings, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetUserRoomBookingsRow, error) {
			return queries.GetUserRoomBookings(qctx, acc.ID)
		})
		if err == nil {
			type bookingView struct {
				line      string
				startTime time.Time
			}
			var current, future []bookingView
			for _, b := range bookings {
				startAt, endAt := bookingBoundsLocal(b.BookingDate, b.StartTime, b.DurationMinutes, loc)
				if endAt.Before(now) || endAt.Equal(now) {
					continue
				}
				bookingsCount++
				if startAt.Before(now) || startAt.Equal(now) {
					minLeft := minutesUntil(now, endAt)
					line := fitRunes(
						fmt.Sprintf(
							dashboardBookingLineCurrent(lang),
							b.RoomName,
							startAt.Format("15:04"),
							endAt.Format("15:04"),
							minLeft,
						),
						tuiLineLimit,
					)
					item := bookingView{line: line, startTime: startAt}
					current = append(current, item)
				} else {
					minToStart := minutesUntil(now, startAt)
					line := fitRunes(
						fmt.Sprintf(
							dashboardBookingLineFuture(lang),
							b.RoomName,
							startAt.Format("15:04"),
							endAt.Format("15:04"),
							minToStart,
						),
						tuiLineLimit,
					)
					item := bookingView{line: line, startTime: startAt}
					future = append(future, item)
				}
			}

			sort.Slice(current, func(i, j int) bool { return current[i].startTime.Before(current[j].startTime) })
			sort.Slice(future, func(i, j int) bool { return future[i].startTime.Before(future[j].startTime) })
			for _, item := range append(current, future...) {
				lines = append(lines, item.line)
			}
		}
	}

	if len(lines) == 0 {
		lines = append(lines, dashboardNoBookingsLine(lang))
	}

	dashboardText := strings.Join([]string{
		strings.Join(lines, "\n"),
	}, "\n")

	return "", map[string]any{
		"campus_id":               campusIDStr,
		"dashboard_text":          dashboardText,
		"bookings_count":          bookingsCount,
		"dashboard_visualization": getVisualizationPath(ctx, queries, campusUUID, cfg),
	}, nil
}

func prepareRoomSelectionTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}

	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)
	rooms, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.Room, error) {
		return queries.GetActiveRoomsByCampus(qctx, campusUUID)
	})
	if err != nil {
		return "", nil, err
	}

	type roomView struct {
		ID    int16
		Name  string
		Label string
		Line  string
	}

	views := make([]roomView, 0, len(rooms))
	for _, room := range rooms {
		availability, err := getRoomAvailability(ctx, queries, campusUUID, room, now, loc)
		if err != nil {
			continue
		}
		if availability.State == roomStateBusy {
			continue
		}

		name := fitRunes(room.Name, 10)
		description := strings.TrimSpace(room.Description.String)
		var line string
		if description != "" {
			if availability.State == roomStateSoon {
				line = fmt.Sprintf("🟡 %s (%s)", name, description)
			} else {
				line = fmt.Sprintf("🟢 %s (%s)", name, description)
			}
		} else if availability.State == roomStateFree {
			if !availability.NextStart.IsZero() {
				line = fmt.Sprintf(roomFreeUntilFmt(lang), name, availability.NextStart.In(loc).Format("15:04"))
			} else {
				line = fmt.Sprintf("🟢 %s", name)
			}
		} else {
			line = fmt.Sprintf(roomSoonAvailableFmt(lang), name, availability.AvailableFrom.In(loc).Format("15:04"))
		}

		views = append(views, roomView{
			ID:    room.ID,
			Name:  room.Name,
			Label: fitRunes(line, tuiLineLimit),
			Line:  fitRunes(line, tuiLineLimit),
		})
	}

	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	vars := map[string]any{
		"rooms_tui_list":   roomsNoAvailableText(lang),
		"booking_mode":     "now",
		"planned_date":     "",
		"planned_start_at": "",
	}
	if len(views) > 0 {
		lines := make([]string, 0, len(views))
		for i, v := range views {
			lines = append(lines, v.Line)
			if i >= maxRoomButtons-1 {
				break
			}
		}
		vars["rooms_tui_list"] = strings.Join(lines, "\n")
	}

	for i := range maxRoomButtons {
		idKey := fmt.Sprintf("room_btn_id_%d", i+1)
		labelKey := fmt.Sprintf("room_btn_label_%d", i+1)
		if i < len(views) {
			vars[idKey] = fmt.Sprintf("room_%d", views[i].ID)
			vars[labelKey] = views[i].Label
			continue
		}
		vars[idKey] = ""
		vars[labelKey] = ""
	}

	return "", vars, nil
}

func preparePlanDayTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)
	limit := now.Add(28 * time.Hour)

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	options := []time.Time{today, today.AddDate(0, 0, 1)}
	dayAfterTomorrow := today.AddDate(0, 0, 2)
	if !dayAfterTomorrow.After(limit) {
		options = append(options, dayAfterTomorrow)
	}

	vars := map[string]any{
		"booking_mode":     "planned",
		"plan_time_error":  "",
		"planned_date":     "",
		"planned_start_at": "",
	}
	for i := range maxPlanDayButtons {
		vars[fmt.Sprintf("plan_day_btn_id_%d", i+1)] = ""
		vars[fmt.Sprintf("plan_day_btn_label_%d", i+1)] = ""
	}

	tomorrow := today.AddDate(0, 0, 1)
	for i, day := range options {
		if i >= maxPlanDayButtons {
			break
		}
		label := ""
		switch {
		case sameLocalDate(day, today):
			label = dayLabelToday(lang)
		case sameLocalDate(day, tomorrow):
			label = fmt.Sprintf("%s, %s", dayLabelTomorrow(lang), formatDayMonth(lang, day))
		default:
			label = fmt.Sprintf("%s, %s", weekdayShort(lang, day.Weekday()), formatDayMonth(lang, day))
		}
		vars[fmt.Sprintf("plan_day_btn_id_%d", i+1)] = fmt.Sprintf("pday_%s", day.Format("2006-01-02"))
		vars[fmt.Sprintf("plan_day_btn_label_%d", i+1)] = fitRunes(label, tuiLineLimit)
	}

	return "", vars, nil
}

func planSelectDayTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	input := resolveDurationRawInput(payload)
	dayStr, ok := strings.CutPrefix(strings.TrimSpace(input), "pday_")
	if !ok {
		return "BOOKING_PLAN_DAY", nil, nil
	}
	day, err := time.ParseInLocation("2006-01-02", dayStr, loc)
	if err != nil {
		return "BOOKING_PLAN_DAY", nil, nil
	}
	return "", map[string]any{
		"booking_mode":     "planned",
		"planned_date":     day.Format("2006-01-02"),
		"planned_start_at": "",
	}, nil
}

func preparePlanTimeTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	day, ok := resolvePlannedDay(payload, loc)
	if !ok {
		return "BOOKING_PLAN_DAY", nil, nil
	}

	now := time.Now().In(loc)
	limit := now.Add(28 * time.Hour)

	suggestions := make([]time.Time, 0, maxPlanTimeButtons)
	baseHour := now.Truncate(time.Hour).Add(time.Hour)
	for i := 0; i < 24 && len(suggestions) < maxPlanTimeButtons; i++ {
		h := baseHour.Add(time.Duration(i) * time.Hour).Hour()
		candidate := time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, loc)
		if !candidate.After(now) || candidate.After(limit) {
			continue
		}
		suggestions = append(suggestions, candidate)
	}

	vars := map[string]any{
		"booking_mode":   "planned",
		"plan_time_text": fmt.Sprintf("%s (%s)", planDayShort(lang, day), relativeDayLabel(lang, now, day)),
	}
	for i := range maxPlanTimeButtons {
		vars[fmt.Sprintf("plan_time_btn_id_%d", i+1)] = ""
		vars[fmt.Sprintf("plan_time_btn_label_%d", i+1)] = ""
	}
	for i, t := range suggestions {
		if i >= maxPlanTimeButtons {
			break
		}
		hhmm := t.Format("15:04")
		vars[fmt.Sprintf("plan_time_btn_id_%d", i+1)] = "ptime_" + hhmm
		vars[fmt.Sprintf("plan_time_btn_label_%d", i+1)] = hhmm
	}
	return "", vars, nil
}

func planProcessTimeTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	day, ok := resolvePlannedDay(payload, loc)
	if !ok {
		return "BOOKING_PLAN_DAY", nil, nil
	}

	raw := resolveDurationRawInput(payload)
	if after, ok := strings.CutPrefix(strings.TrimSpace(raw), "ptime_"); ok {
		raw = after
	}
	parsed, err := time.ParseInLocation("15:04", strings.TrimSpace(raw), loc)
	if err != nil {
		return "BOOKING_PLAN_TIME", map[string]any{
			"plan_time_error": planTimeFormatError(lang),
		}, nil
	}

	roundedMin := roundClockToFive(parsed.Hour()*60 + parsed.Minute())
	startAt := time.Date(day.Year(), day.Month(), day.Day(), roundedMin/60, roundedMin%60, 0, 0, loc)
	now := time.Now().In(loc)
	limit := now.Add(28 * time.Hour)
	if !startAt.After(now) || startAt.After(limit) {
		return "BOOKING_PLAN_TIME", map[string]any{
			"plan_time_error": fmt.Sprintf(planTimeLimitFmt(lang), planLimitTimeFmt(lang, limit)),
		}, nil
	}

	return "", map[string]any{
		"booking_mode":         "planned",
		"plan_time_error":      "",
		"planned_start_at":     startAt.UTC().Format(time.RFC3339),
		"booking_starts_local": startAt.Format("15:04"),
	}, nil
}

func preparePlanDurationTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	startAt, ok := resolvePlannedStartAt(payload, loc)
	if !ok {
		return "BOOKING_PLAN_TIME", nil, nil
	}
	return "", map[string]any{
		"booking_mode": "planned",
		"plan_duration_text": fmt.Sprintf(
			planDurationTextFmt(lang),
			planLimitTimeFmt(lang, startAt),
			relativeDayLabel(lang, time.Now().In(loc), startAt),
		),
		"plan_dur_15_id": "pldur15",
		"plan_dur_30_id": "pldur30",
		"plan_dur_45_id": "pldur45",
		"plan_dur_60_id": "pldur60",
	}, nil
}

func planProcessDurationTUI(_ context.Context, payload map[string]any) (string, map[string]any, error) {
	raw := resolveDurationRawInput(payload)
	raw = strings.TrimSpace(raw)
	duration := 0
	switch raw {
	case "pldur15":
		duration = 15
	case "pldur30":
		duration = 30
	case "pldur45":
		duration = 45
	case "pldur60":
		duration = 60
	}
	if duration == 0 {
		return "BOOKING_PLAN_DURATION", nil, nil
	}
	return "", map[string]any{
		"booking_mode":      "planned",
		"duration":          duration,
		"requested_minutes": duration,
		"rounded_minutes":   duration,
	}, nil
}

func preparePlanRoomsTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	startAt, ok := resolvePlannedStartAt(payload, loc)
	if !ok {
		return "BOOKING_PLAN_TIME", nil, nil
	}
	duration := int(toInt32(payload["duration"]))
	if duration <= 0 {
		return "BOOKING_PLAN_DURATION", nil, nil
	}

	rooms, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.Room, error) {
		return queries.GetActiveRoomsByCampus(qctx, campusUUID)
	})
	if err != nil {
		return "", nil, err
	}
	startMin := int64(startAt.Hour()*60 + startAt.Minute())
	endMin := startMin + int64(duration)

	vars := map[string]any{
		"booking_mode":         "planned",
		"plan_rooms_text":      planRoomsNoneText(lang),
		"booking_starts_local": startAt.Format("15:04"),
	}
	for i := range maxRoomButtons {
		vars[fmt.Sprintf("room_btn_id_%d", i+1)] = ""
		vars[fmt.Sprintf("room_btn_label_%d", i+1)] = ""
	}

	type roomView struct {
		ID    int16
		Name  string
		Label string
	}
	views := make([]roomView, 0, len(rooms))
	for _, room := range rooms {
		if room.MaxDuration > 0 && duration > int(room.MaxDuration) {
			continue
		}
		if checkBookingConflict(ctx, queries, campusUUID, room.ID, startAt, startMin, endMin, loc) {
			continue
		}
		line := fitRunes(fmt.Sprintf("🟢 %s", room.Name), tuiLineLimit)
		views = append(views, roomView{ID: room.ID, Name: room.Name, Label: line})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	if len(views) > 0 {
		lines := make([]string, 0, len(views))
		for i, v := range views {
			if i >= maxRoomButtons {
				break
			}
			lines = append(lines, v.Label)
			vars[fmt.Sprintf("room_btn_id_%d", i+1)] = fmt.Sprintf("room_%d", v.ID)
			vars[fmt.Sprintf("room_btn_label_%d", i+1)] = v.Label
		}
		vars["plan_rooms_text"] = strings.Join(lines, "\n")
	}
	return "", vars, nil
}

func planSelectRoomTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	roomID := parseRoomIDFromInput(payload)
	if roomID == 0 {
		return "BOOKING_PLAN_ROOMS", nil, nil
	}
	room, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.Room, error) {
		return queries.GetRoomByID(qctx, db.GetRoomByIDParams{CampusID: campusUUID, ID: roomID})
	})
	if err != nil {
		return "BOOKING_PLAN_ROOMS", nil, nil
	}
	return "", map[string]any{
		"booking_mode":       "planned",
		"selected_room_id":   room.ID,
		"selected_room_name": room.Name,
	}, nil
}

func prepareDurationChoiceTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}

	selectedRoomID := parseRoomIDFromInput(payload)
	if selectedRoomID == 0 {
		selectedRoomID = toInt16(payload["selected_room_id"])
	}
	if selectedRoomID == 0 {
		return "BOOKING_ROOM_SELECTION", nil, nil
	}

	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)
	room, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.Room, error) {
		return queries.GetRoomByID(qctx, db.GetRoomByIDParams{CampusID: campusUUID, ID: selectedRoomID})
	})
	if err != nil {
		return "BOOKING_ROOM_SELECTION", nil, nil
	}

	availability, err := getRoomAvailability(ctx, queries, campusUUID, room, now, loc)
	if err != nil || availability.State == roomStateBusy {
		return "BOOKING_CONFLICT", nil, nil
	}

	maxGap := availability.GapMinutes
	if room.MaxDuration > 0 && maxGap > int(room.MaxDuration) {
		maxGap = int(room.MaxDuration)
	}
	if maxGap <= 0 {
		return "BOOKING_CONFLICT", nil, nil
	}

	vars := map[string]any{
		"selected_room_id":   room.ID,
		"selected_room_name": room.Name,
		"max_gap":            maxGap,
		"duration_15_id":     "",
		"duration_30_id":     "",
		"duration_45_id":     "",
		"duration_60_id":     "",
		"duration_max_id":    "",
	}
	startLocal := availability.AvailableFrom.In(loc).Format("15:04")
	timingLine := fmt.Sprintf(durationStartFmt(lang), startLocal)
	if availability.State == roomStateSoon {
		timingLine = fmt.Sprintf(durationFreeAtFmt(lang), startLocal)
	}
	vars["duration_text"] = strings.Join([]string{
		fitRunes(timingLine, tuiLineLimit),
	}, "\n")

	if maxGap >= 15 {
		vars["duration_15_id"] = fmt.Sprintf("dur15_%d", room.ID)
	}
	if maxGap >= 30 {
		vars["duration_30_id"] = fmt.Sprintf("dur30_%d", room.ID)
	}
	if maxGap >= 45 {
		vars["duration_45_id"] = fmt.Sprintf("dur45_%d", room.ID)
	}
	if maxGap >= 60 {
		vars["duration_60_id"] = fmt.Sprintf("dur60_%d", room.ID)
	}
	// Avoid duplicate "До упора" when max gap already matches a preset button.
	if maxGap != 15 && maxGap != 30 && maxGap != 60 {
		vars["duration_max_id"] = fmt.Sprintf("durmax_%d", room.ID)
	}

	return "", vars, nil
}

func processDurationInputTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}

	selectedRoomID := parseRoomIDFromInput(payload)
	if selectedRoomID == 0 {
		selectedRoomID = toInt16(payload["selected_room_id"])
	}
	if selectedRoomID == 0 {
		return "BOOKING_ROOM_SELECTION", nil, nil
	}

	raw := resolveDurationRawInput(payload)
	requestedRaw, ok := parseRequestedMinutes(raw, toInt32(payload["max_gap"]))
	if !ok {
		return "BOOKING_DURATION_CHOICE", nil, nil
	}
	requestedRounded := RoundTime(requestedRaw)

	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)
	room, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.Room, error) {
		return queries.GetRoomByID(qctx, db.GetRoomByIDParams{CampusID: campusUUID, ID: selectedRoomID})
	})
	if err != nil {
		return "BOOKING_ROOM_SELECTION", nil, nil
	}

	availability, err := getRoomAvailability(ctx, queries, campusUUID, room, now, loc)
	if err != nil || availability.State == roomStateBusy {
		return "BOOKING_CONFLICT", nil, nil
	}

	maxGap := availability.GapMinutes
	if room.MaxDuration > 0 && maxGap > int(room.MaxDuration) {
		maxGap = int(room.MaxDuration)
	}
	actual := GetAvailableWindow(requestedRounded, maxGap)
	if actual <= 0 {
		return "BOOKING_CONFLICT", nil, nil
	}

	return "", map[string]any{
		"selected_room_id":     selectedRoomID,
		"selected_room_name":   room.Name,
		"requested_minutes":    requestedRaw,
		"rounded_minutes":      requestedRounded,
		"duration":             actual,
		"available_minutes":    maxGap,
		"booking_starts_local": availability.AvailableFrom.In(loc).Format("15:04"),
	}, nil
}

func createBookingWithDurationTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
	scheduleRegen ScheduleRegenerator,
) (string, map[string]any, error) {
	campusUUID, _, err := campusFromPayload(payload)
	if err != nil {
		return "", nil, err
	}

	roomID := toInt16(payload["selected_room_id"])
	if roomID == 0 {
		roomID = toInt16(payload["room_id"])
	}
	if roomID == 0 {
		return "", map[string]any{"success": false, "error": "conflict"}, nil
	}

	duration := toInt32(payload["duration"])
	if duration <= 0 {
		return "", map[string]any{"success": false, "error": "invalid_duration"}, nil
	}

	loc := getUserTimezone(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)
	room, err := withTimeoutQuery(ctx, func(qctx context.Context) (db.Room, error) {
		return queries.GetRoomByID(qctx, db.GetRoomByIDParams{CampusID: campusUUID, ID: roomID})
	})
	if err != nil {
		return "", map[string]any{"success": false, "error": "conflict"}, nil
	}

	mode, _ := payload["booking_mode"].(string)
	plannedStart, hasPlannedStart := resolvePlannedStartAt(payload, loc)
	if mode != "planned" {
		hasPlannedStart = false
	}
	actual := int(duration)
	startAt := now
	if hasPlannedStart {
		startAt = plannedStart.In(loc).Truncate(time.Minute)
		if room.MaxDuration > 0 && actual > int(room.MaxDuration) {
			return "", map[string]any{"success": false, "error": "invalid_duration"}, nil
		}
		if actual < minBookingMinutes || !startAt.After(now) {
			return "", map[string]any{"success": false, "error": "conflict"}, nil
		}
		startMin := int64(startAt.Hour()*60 + startAt.Minute())
		endMin := startMin + int64(actual)
		// Final recheck for race conditions in "plan ahead" mode.
		if checkBookingConflict(ctx, queries, campusUUID, roomID, startAt, startMin, endMin, loc) {
			return "", map[string]any{"success": false, "error": "conflict"}, nil
		}
	} else {
		availability, err := getRoomAvailability(ctx, queries, campusUUID, room, now, loc)
		if err != nil || availability.State == roomStateBusy {
			return "", map[string]any{"success": false, "error": "conflict"}, nil
		}
		maxGap := availability.GapMinutes
		if room.MaxDuration > 0 && maxGap > int(room.MaxDuration) {
			maxGap = int(room.MaxDuration)
		}
		actual = GetAvailableWindow(int(duration), maxGap)
		if actual < minBookingMinutes {
			return "", map[string]any{"success": false, "error": "conflict"}, nil
		}
		startAt = availability.AvailableFrom.In(loc).Truncate(time.Minute)
	}

	startMin := int64(startAt.Hour()*60 + startAt.Minute())
	endMin := startMin + int64(actual)
	acc, err := getAccountByExternalID(ctx, queries, userID)
	if err != nil {
		return "", nil, err
	}
	if checkUserBookingConflict(ctx, queries, acc.ID, startAt, startMin, endMin, loc, 0) {
		return "", map[string]any{"success": false, "error": "user_overlap"}, nil
	}
	if limitErr := checkUserBookingLimits(ctx, queries, acc.ID, startAt, actual, loc); limitErr != "" {
		return "", map[string]any{"success": false, "error": limitErr}, nil
	}

	_, err = withTimeoutQuery(ctx, func(qctx context.Context) (db.RoomBooking, error) {
		return queries.CreateRoomBooking(qctx, db.CreateRoomBookingParams{
			CampusID: campusUUID,
			RoomID:   roomID,
			UserID:   acc.ID,
			BookingDate: pgtype.Date{
				Time:  startAt,
				Valid: true,
			},
			StartTime: pgtype.Time{
				Microseconds: startMin * 60 * 1_000_000,
				Valid:        true,
			},
			DurationMinutes: int32(actual),
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", map[string]any{"success": false, "error": "conflict"}, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23P01" {
			if pgErr.ConstraintName == "room_bookings_user_no_overlap" {
				return "", map[string]any{"success": false, "error": "user_overlap"}, nil
			}
			return "", map[string]any{"success": false, "error": "conflict"}, nil
		}
		return "", map[string]any{"success": false, "error": "create_failed"}, nil
	}

	if scheduleRegen != nil {
		scheduleRegen.ForceRegenerate()
	}

	endAt := startAt.Add(time.Duration(actual) * time.Minute)
	return "", map[string]any{
		"success":              true,
		"selected_room_id":     roomID,
		"selected_room_name":   room.Name,
		"duration":             actual,
		"booking_end_time":     endAt.In(loc).Format("15:04"),
		"booking_starts_local": startAt.In(loc).Format("15:04"),
	}, nil
}

func getUserBookingsTUI(
	ctx context.Context,
	queries db.Querier,
	cfg *config.Config,
	userID int64,
	payload map[string]any,
) (string, map[string]any, error) {
	lang := payloadLang(payload)
	var dashboardVisualization string
	if campusUUID, _, err := campusFromPayload(payload); err == nil {
		dashboardVisualization = getVisualizationPath(ctx, queries, campusUUID, cfg)
	}

	acc, err := getAccountByExternalID(ctx, queries, userID)
	if err != nil {
		return "", map[string]any{
			"my_bookings_text":        bookingsNoneActiveText(lang),
			"dashboard_visualization": dashboardVisualization,
		}, nil
	}

	bookings, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetUserRoomBookingsRow, error) {
		return queries.GetUserRoomBookings(qctx, acc.ID)
	})
	if err != nil {
		return "", map[string]any{
			"my_bookings_text":        bookingsNoneActiveText(lang),
			"dashboard_visualization": dashboardVisualization,
		}, nil
	}

	type manageItem struct {
		ID       int64
		Label    string
		Data     string
		Line     string
		StartAt  time.Time
		IsActive bool
	}

	items := make([]manageItem, 0, len(bookings))
	nowUTC := time.Now().UTC()
	for _, b := range bookings {
		loc := getUserTimezone(ctx, queries, userID, b.CampusID)
		startAt, endAt := bookingBoundsLocal(b.BookingDate, b.StartTime, b.DurationMinutes, loc)
		if !endAt.After(nowUTC.In(loc)) {
			continue
		}
		active := startAt.Before(nowUTC.In(loc)) || startAt.Equal(nowUTC.In(loc))
		if active {
			minLeft := minutesUntil(nowUTC.In(loc), endAt)
			items = append(items, manageItem{
				ID:    b.ID,
				Label: fitRunes(fmt.Sprintf(bookingFinishLabelFmt(lang), b.RoomName), tuiLineLimit),
				Data:  fmt.Sprintf("finish_%d", b.ID),
				Line: fitRunes(
					fmt.Sprintf(
						dashboardBookingLineCurrent(lang),
						b.RoomName,
						startAt.Format("15:04"),
						endAt.Format("15:04"),
						minLeft,
					),
					tuiLineLimit,
				),
				StartAt:  startAt,
				IsActive: true,
			})
			continue
		}
		minToStart := minutesUntil(nowUTC.In(loc), startAt)
		items = append(items, manageItem{
			ID:    b.ID,
			Label: fitRunes(fmt.Sprintf(bookingCancelLabelFmt(lang), b.RoomName), tuiLineLimit),
			Data:  fmt.Sprintf("cancel_%d", b.ID),
			Line: fitRunes(
				fmt.Sprintf(
					dashboardBookingLineFuture(lang),
					b.RoomName,
					startAt.Format("15:04"),
					endAt.Format("15:04"),
					minToStart,
				),
				tuiLineLimit,
			),
			StartAt:  startAt,
			IsActive: false,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsActive != items[j].IsActive {
			return items[i].IsActive
		}
		return items[i].StartAt.Before(items[j].StartAt)
	})

	lines := make([]string, 0, len(items))
	for i := range items {
		lines = append(lines, items[i].Line)
	}
	if len(lines) == 0 {
		lines = append(lines, bookingsNoBookingsLine(lang))
	}

	vars := map[string]any{
		"my_bookings_text":        strings.Join(lines, "\n"),
		"bookings_count":          len(items),
		"my_bookings_hint_ru":     "",
		"my_bookings_hint_en":     "",
		"dashboard_visualization": dashboardVisualization,
	}
	if len(items) > 0 {
		vars["my_bookings_hint_ru"] = "👇 Нажми на кнопку, чтобы отменить:"
		vars["my_bookings_hint_en"] = "👇 Click the button to cancel:"
	}

	for i := range maxManageBookButtons {
		idKey := fmt.Sprintf("booking_btn_id_%d", i+1)
		labelKey := fmt.Sprintf("booking_btn_label_%d", i+1)
		if i < len(items) {
			vars[idKey] = items[i].Data
			vars[labelKey] = items[i].Label
			continue
		}
		vars[idKey] = ""
		vars[labelKey] = ""
	}

	return "", vars, nil
}

func manageBookingTUI(
	ctx context.Context,
	queries db.Querier,
	userID int64,
	payload map[string]any,
	scheduleRegen ScheduleRegenerator,
) (string, map[string]any, error) {
	lastInput, _ := payload["last_input"].(string)
	if lastInput == "" {
		lastInput, _ = payload["id"].(string)
	}
	if lastInput == "" {
		return "FETCH_MY_BOOKINGS", nil, nil
	}

	action := "cancel"
	bookingID := int64(0)
	switch {
	case strings.HasPrefix(lastInput, "finish_"):
		action = "finish"
		bookingID = toInt64(strings.TrimPrefix(lastInput, "finish_"))
	case strings.HasPrefix(lastInput, "cancel_"):
		action = "cancel"
		bookingID = toInt64(strings.TrimPrefix(lastInput, "cancel_"))
	default:
		return "FETCH_MY_BOOKINGS", nil, nil
	}
	if bookingID == 0 {
		return "FETCH_MY_BOOKINGS", nil, nil
	}

	acc, err := getAccountByExternalID(ctx, queries, userID)
	if err != nil {
		return "FETCH_MY_BOOKINGS", nil, nil
	}

	bookings, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetUserRoomBookingsRow, error) {
		return queries.GetUserRoomBookings(qctx, acc.ID)
	})
	if err != nil {
		return "FETCH_MY_BOOKINGS", nil, nil
	}

	var target *db.GetUserRoomBookingsRow
	for i := range bookings {
		if bookings[i].ID == bookingID {
			target = &bookings[i]
			break
		}
	}
	if target == nil {
		return "FETCH_MY_BOOKINGS", nil, nil
	}

	loc := getUserTimezone(ctx, queries, userID, target.CampusID)
	now := time.Now().In(loc)
	startAt, endAt := bookingBoundsLocal(target.BookingDate, target.StartTime, target.DurationMinutes, loc)
	isStarted := !now.Before(startAt) && now.Before(endAt)

	// Security rule: cancel for already-started bookings turns into finish.
	shouldFinish := action == "finish" || isStarted
	if shouldFinish {
		elapsed := min(max(int32(now.Sub(startAt).Minutes()), 0), target.DurationMinutes)
		_ = withTimeoutExec(ctx, func(qctx context.Context) error {
			return queries.UpdateRoomBookingDuration(qctx, db.UpdateRoomBookingDurationParams{
				ID:              bookingID,
				UserID:          acc.ID,
				DurationMinutes: elapsed,
			})
		})
	} else {
		_ = withTimeoutExec(ctx, func(qctx context.Context) error {
			return queries.CancelRoomBooking(qctx, db.CancelRoomBookingParams{
				ID:     bookingID,
				UserID: acc.ID,
			})
		})
	}

	if scheduleRegen != nil {
		scheduleRegen.ForceRegenerate()
	}

	return "FETCH_MY_BOOKINGS", map[string]any{"success": true}, nil
}

func getRoomAvailability(
	ctx context.Context,
	queries db.Querier,
	campusID pgtype.UUID,
	room db.Room,
	now time.Time,
	loc *time.Location,
) (roomAvailability, error) {
	intervals, err := loadRoomIntervalsAround(ctx, queries, campusID, room.ID, now, loc)
	if err != nil {
		return roomAvailability{}, err
	}
	availability := RoomStatus(now, intervals)
	if room.MaxDuration > 0 && availability.GapMinutes > int(room.MaxDuration) {
		availability.GapMinutes = int(room.MaxDuration)
	}
	return availability, nil
}

// RoomStatus evaluates room availability using the requested free/soon/busy rules.
func RoomStatus(now time.Time, intervals []bookingInterval) roomAvailability {
	currentIdx := -1
	nextStart := time.Time{}

	for i := range intervals {
		iv := intervals[i]
		if !now.Before(iv.Start) && now.Before(iv.End) {
			currentIdx = i
			break
		}
	}

	if currentIdx >= 0 {
		currentEnd := intervals[currentIdx].End
		for i := currentIdx + 1; i < len(intervals); i++ {
			if intervals[i].Start.After(currentEnd) || intervals[i].Start.Equal(currentEnd) {
				nextStart = intervals[i].Start
				break
			}
		}
		gapAfterCurrent := maxGapFrom(currentEnd, nextStart)
		endsSoon := int(currentEnd.Sub(now).Minutes()) <= roomSoonWindowMin
		if endsSoon && gapAfterCurrent >= roomFreeGapMin {
			return roomAvailability{
				State:         roomStateSoon,
				AvailableFrom: currentEnd,
				GapMinutes:    gapAfterCurrent,
				NextStart:     nextStart,
			}
		}
		return roomAvailability{
			State:         roomStateBusy,
			AvailableFrom: currentEnd,
			GapMinutes:    0,
			NextStart:     nextStart,
		}
	}

	for _, iv := range intervals {
		if iv.Start.After(now) {
			nextStart = iv.Start
			break
		}
	}
	gapNow := maxGapFrom(now, nextStart)
	if gapNow >= roomFreeGapMin {
		return roomAvailability{
			State:         roomStateFree,
			AvailableFrom: now,
			GapMinutes:    gapNow,
			NextStart:     nextStart,
		}
	}

	return roomAvailability{
		State:         roomStateBusy,
		AvailableFrom: now,
		GapMinutes:    0,
		NextStart:     nextStart,
	}
}

func loadRoomIntervalsAround(
	ctx context.Context,
	queries db.Querier,
	campusID pgtype.UUID,
	roomID int16,
	anchor time.Time,
	loc *time.Location,
) ([]bookingInterval, error) {
	dates := []time.Time{
		anchor.AddDate(0, 0, -1),
		anchor,
		anchor.AddDate(0, 0, 1),
	}

	var intervals []bookingInterval
	seen := make(map[string]bool)
	for _, date := range dates {
		rows, err := withTimeoutQuery(ctx, func(qctx context.Context) ([]db.GetRoomBookingsByDateRow, error) {
			return queries.GetRoomBookingsByDate(qctx, db.GetRoomBookingsByDateParams{
				CampusID: campusID,
				RoomID:   roomID,
				BookingDate: pgtype.Date{
					Time:  date,
					Valid: true,
				},
			})
		})
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			startAt, endAt := bookingBoundsLocal(row.BookingDate, row.StartTime, row.DurationMinutes, loc)
			key := fmt.Sprintf("%d_%d", startAt.Unix(), endAt.Unix())
			if seen[key] {
				continue
			}
			seen[key] = true
			intervals = append(intervals, bookingInterval{
				Start: startAt,
				End:   endAt,
			})
		}
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start.Before(intervals[j].Start)
	})
	return intervals, nil
}

func bookingBoundsLocal(
	bookingDate pgtype.Date,
	startTime pgtype.Time,
	durationMinutes int32,
	loc *time.Location,
) (time.Time, time.Time) {
	// booking_date is DATE without timezone; preserve its calendar day.
	year, month, day := bookingDate.Time.Date()
	startMin := startTime.Microseconds / 60_000_000
	startAt := time.Date(year, month, day, int(startMin/60), int(startMin%60), 0, 0, loc)
	endAt := startAt.Add(time.Duration(durationMinutes) * time.Minute)
	return startAt, endAt
}

func getAccountByExternalID(ctx context.Context, queries db.Querier, userID int64) (db.UserAccount, error) {
	return withTimeoutQuery(ctx, func(qctx context.Context) (db.UserAccount, error) {
		return queries.GetUserAccountByExternalId(qctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
	})
}

func campusFromPayload(payload map[string]any) (pgtype.UUID, string, error) {
	campusIDStr, _ := payload["campus_id"].(string)
	if campusIDStr == "" || campusIDStr == "$context.campus_id" {
		return pgtype.UUID{}, "", fmt.Errorf("campus_id missing")
	}
	var campusUUID pgtype.UUID
	if err := campusUUID.Scan(campusIDStr); err != nil {
		return pgtype.UUID{}, "", fmt.Errorf("invalid campus_id")
	}
	return campusUUID, campusIDStr, nil
}

func parseRoomIDFromInput(payload map[string]any) int16 {
	lastInput, _ := payload["last_input"].(string)
	if lastInput == "" {
		lastInput, _ = payload["id"].(string)
	}
	if after, ok := strings.CutPrefix(lastInput, "room_"); ok {
		return toInt16(after)
	}
	parts := strings.Split(lastInput, "_")
	if len(parts) > 1 {
		return toInt16(parts[len(parts)-1])
	}
	return 0
}

func resolveDurationRawInput(payload map[string]any) string {
	msg, _ := payload["message"].(map[string]any)
	text, _ := msg["text"].(string)
	if strings.TrimSpace(text) != "" {
		return text
	}
	if id, ok := payload["id"].(string); ok && strings.TrimSpace(id) != "" {
		return id
	}
	if li, ok := payload["last_input"].(string); ok {
		return li
	}
	return ""
}

func parseRequestedMinutes(raw string, maxGap int32) (int, bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "dur15_"):
		return 15, true
	case strings.HasPrefix(raw, "dur30_"):
		return 30, true
	case strings.HasPrefix(raw, "dur45_"):
		return 45, true
	case strings.HasPrefix(raw, "dur60_"):
		return 60, true
	case strings.HasPrefix(raw, "durmax_"):
		return int(maxGap), true
	default:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}

func resolvePlannedDay(payload map[string]any, loc *time.Location) (time.Time, bool) {
	if dayStr, ok := payload["planned_date"].(string); ok && strings.TrimSpace(dayStr) != "" {
		if day, err := time.ParseInLocation("2006-01-02", dayStr, loc); err == nil {
			return day, true
		}
	}
	raw := resolveDurationRawInput(payload)
	if after, ok := strings.CutPrefix(strings.TrimSpace(raw), "pday_"); ok {
		if day, err := time.ParseInLocation("2006-01-02", after, loc); err == nil {
			return day, true
		}
	}
	return time.Time{}, false
}

func resolvePlannedStartAt(payload map[string]any, loc *time.Location) (time.Time, bool) {
	startStr, _ := payload["planned_start_at"].(string)
	if strings.TrimSpace(startStr) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return time.Time{}, false
	}
	return t.In(loc), true
}

func sameLocalDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func formatRuDayMonth(t time.Time) string {
	months := []string{
		"янв", "фев", "мар", "апр", "май", "июн",
		"июл", "авг", "сен", "окт", "ноя", "дек",
	}
	return fmt.Sprintf("%02d %s", t.Day(), months[int(t.Month())-1])
}

func weekdayShortRu(wd time.Weekday) string {
	switch wd {
	case time.Monday:
		return "Пн"
	case time.Tuesday:
		return "Вт"
	case time.Wednesday:
		return "Ср"
	case time.Thursday:
		return "Чт"
	case time.Friday:
		return "Пт"
	case time.Saturday:
		return "Сб"
	default:
		return "Вс"
	}
}

func payloadLang(payload map[string]any) string {
	if v, ok := payload["language"].(string); ok && strings.TrimSpace(v) != "" {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == fsm.LangEn || v == fsm.LangRu {
			return v
		}
	}
	return fsm.LangRu
}

func isEn(lang string) bool { return lang == fsm.LangEn }

func formatDayMonth(lang string, t time.Time) string {
	if isEn(lang) {
		// Go's time.Format month names are English.
		return t.Format("Jan 02")
	}
	return formatRuDayMonth(t)
}

func weekdayShort(lang string, wd time.Weekday) string {
	if !isEn(lang) {
		return weekdayShortRu(wd)
	}
	switch wd {
	case time.Monday:
		return "Mon"
	case time.Tuesday:
		return "Tue"
	case time.Wednesday:
		return "Wed"
	case time.Thursday:
		return "Thu"
	case time.Friday:
		return "Fri"
	case time.Saturday:
		return "Sat"
	default:
		return "Sun"
	}
}

func roundClockToFive(minutes int) int {
	if minutes < 0 {
		return 0
	}
	if minutes > 24*60-1 {
		minutes = 24*60 - 1
	}
	rounded := ((minutes + 2) / 5) * 5
	maxMinutes := 23*60 + 55
	if rounded > maxMinutes {
		return maxMinutes
	}
	return rounded
}

func relativeDayLabel(lang string, now, target time.Time) string {
	if sameLocalDate(now, target) {
		if isEn(lang) {
			return "today"
		}
		return "сегодня"
	}
	if sameLocalDate(now.AddDate(0, 0, 1), target) {
		if isEn(lang) {
			return "tomorrow"
		}
		return "завтра"
	}
	if sameLocalDate(now.AddDate(0, 0, 2), target) {
		if isEn(lang) {
			return "day after tomorrow"
		}
		return "послезавтра"
	}
	return weekdayShort(lang, target.Weekday())
}

func planDayShort(lang string, day time.Time) string {
	if isEn(lang) {
		return day.Format("Jan 02")
	}
	return day.Format("02.01")
}

func planTimeFormatError(lang string) string {
	if isEn(lang) {
		return "Format: HH:MM"
	}
	return "Формат: HH:MM"
}

func planTimeLimitFmt(lang string) string {
	if isEn(lang) {
		return "Available until %s"
	}
	return "Доступно до %s"
}

func planLimitTimeFmt(lang string, t time.Time) string {
	if isEn(lang) {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("02.01 15:04")
}

func dayLabelToday(lang string) string {
	if isEn(lang) {
		return "Today"
	}
	return "Сегодня"
}

func dayLabelTomorrow(lang string) string {
	if isEn(lang) {
		return "Tomorrow"
	}
	return "Завтра"
}

func planDurationTextFmt(lang string) string {
	if isEn(lang) {
		return "Start: %s (%s)"
	}
	return "Старт: %s (%s)"
}

func planRoomsNoneText(lang string) string {
	if isEn(lang) {
		return "No free rooms"
	}
	return "Нет свободных комнат"
}

func durationStartFmt(lang string) string {
	if isEn(lang) {
		return "Start: %s"
	}
	return "Старт: %s"
}

func durationFreeAtFmt(lang string) string {
	if isEn(lang) {
		return "Free at: %s"
	}
	return "Освободится: %s"
}

func roomsNoAvailableText(lang string) string {
	if isEn(lang) {
		return "No available rooms"
	}
	return "Нет доступных комнат"
}

func roomFreeUntilFmt(lang string) string {
	if isEn(lang) {
		return "🟢 %s (until %s)"
	}
	return "🟢 %s (до %s)"
}

func roomSoonAvailableFmt(lang string) string {
	if isEn(lang) {
		return "🟡 %s (free at %s)"
	}
	return "🟡 %s (осв. %s)"
}

func bookingsNoneActiveText(lang string) string {
	if isEn(lang) {
		return "No active bookings"
	}
	return "Нет активных броней"
}

func dashboardNoBookingsLine(lang string) string {
	if isEn(lang) {
		return "📍 No bookings yet"
	}
	return "📍 Броней пока нет"
}

func bookingsNoBookingsLine(lang string) string {
	if isEn(lang) {
		return "📅 No bookings yet"
	}
	return "📅 Броней пока нет"
}

func dashboardBookingLineCurrent(lang string) string {
	if isEn(lang) {
		return "⚡️ %s %s–%s (ends in %d min)"
	}
	return "⚡️ %s %s–%s (закончится через %d мин)"
}

func dashboardBookingLineFuture(lang string) string {
	if isEn(lang) {
		return "📅 %s %s–%s (starts in %d min)"
	}
	return "📅 %s %s–%s (начнётся через %d мин)"
}

func bookingFinishLabelFmt(lang string) string {
	if isEn(lang) {
		return "🏁 Finish: %s"
	}
	return "🏁 Завершить: %s"
}

func bookingCancelLabelFmt(lang string) string {
	if isEn(lang) {
		return "❌ Cancel: %s"
	}
	return "❌ Отменить: %s"
}

func fitRunes(s string, max int) string {
	_ = max
	return s
}

func minutesUntil(from, to time.Time) int {
	if !to.After(from) {
		return 0
	}
	diff := to.Sub(from)
	min := int(diff / time.Minute)
	if diff%time.Minute != 0 {
		min++
	}
	return min
}

func maxGapFrom(start, next time.Time) int {
	if next.IsZero() {
		return 120
	}
	diff := int(next.Sub(start).Minutes())
	if diff < 0 {
		return 0
	}
	return diff
}

func withTimeoutQuery[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	qctx, cancel := context.WithTimeout(ctx, dbQueryTimeout)
	defer cancel()
	return fn(qctx)
}

func withTimeoutExec(ctx context.Context, fn func(context.Context) error) error {
	qctx, cancel := context.WithTimeout(ctx, dbQueryTimeout)
	defer cancel()
	return fn(qctx)
}
