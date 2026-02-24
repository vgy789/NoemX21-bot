package library

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/common"
)

const (
	searchPageLimit    = 7
	maxResultButtons   = 7
	maxCategoryButtons = 8
	maxLoanButtons     = 8
	loanPeriodDays     = 14
)

type catalogBook struct {
	ID             int16
	Title          string
	Author         string
	Category       string
	AvailableStock int32
}

func resolveCampusID(ctx context.Context, queries db.Querier, userID int64, payload map[string]any) pgtype.UUID {
	campusUUID := robustScanUUID(payload["campus_id"])
	if campusUUID.Valid {
		return campusUUID
	}

	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err != nil {
		return campusUUID
	}

	profile, err := queries.GetMyProfile(ctx, acc.S21Login)
	if err != nil {
		return campusUUID
	}
	return profile.CampusID
}

func resolveBookID(payload map[string]any) int16 {
	bookID := common.ToInt16(payload["book_id"])
	if bookID != 0 {
		return bookID
	}

	bookID = common.ToInt16(payload["selected_book_id"])
	if bookID != 0 {
		return bookID
	}

	lastInput := strings.TrimSpace(common.ToString(payload["last_input"]))
	if after, ok := strings.CutPrefix(lastInput, "book_"); ok {
		return common.ToInt16(after)
	}
	return 0
}

func extractBookID(raw string) int16 {
	if after, ok := strings.CutPrefix(strings.TrimSpace(raw), "book_"); ok {
		return common.ToInt16(after)
	}
	return 0
}

func getUserTimezoneForLibrary(ctx context.Context, queries db.Querier, userID int64, campusUUID pgtype.UUID) *time.Location {
	defaultLoc := time.UTC
	if moscow, err := time.LoadLocation("Europe/Moscow"); err == nil {
		defaultLoc = moscow
	}

	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err == nil {
		if user, err := queries.GetRegisteredUserByS21Login(ctx, acc.S21Login); err == nil {
			if user.Timezone != "" {
				if loc, err := time.LoadLocation(user.Timezone); err == nil {
					return loc
				}
			}
		}
	}

	if campusUUID.Valid {
		if campus, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
			if campus.Timezone.Valid && campus.Timezone.String != "" {
				if loc, err := time.LoadLocation(campus.Timezone.String); err == nil {
					return loc
				}
			}
		}
	}

	return defaultLoc
}

func robustScanUUID(v any) pgtype.UUID {
	var uuid pgtype.UUID
	if v == nil {
		return uuid
	}
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, "$context.") {
			return uuid
		}
		_ = uuid.Scan(val)
	case [16]byte:
		uuid.Bytes = val
		uuid.Valid = true
	case []byte:
		if len(val) == 16 {
			copy(uuid.Bytes[:], val)
			uuid.Valid = true
		} else {
			_ = uuid.Scan(string(val))
		}
	case pgtype.UUID:
		return val
	}
	return uuid
}
