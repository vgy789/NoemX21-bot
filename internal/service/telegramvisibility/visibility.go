package telegramvisibility

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const DisableGrace = 24 * time.Hour

func Effective(isSearchable pgtype.Bool, visibilityEndsAt pgtype.Timestamptz, now time.Time) bool {
	return (isSearchable.Valid && isSearchable.Bool) ||
		(visibilityEndsAt.Valid && visibilityEndsAt.Time.After(now))
}

func PendingHide(isSearchable pgtype.Bool, visibilityEndsAt pgtype.Timestamptz, now time.Time) bool {
	return isSearchable.Valid && !isSearchable.Bool && visibilityEndsAt.Valid && visibilityEndsAt.Time.After(now)
}
