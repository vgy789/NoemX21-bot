package telegramvisibility

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestEffectiveVisibilityBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	disabled := pgtype.Bool{Bool: false, Valid: true}
	assert.True(t, Effective(disabled, pgtype.Timestamptz{Time: now.Add(time.Second), Valid: true}, now))
	assert.False(t, Effective(disabled, pgtype.Timestamptz{Time: now, Valid: true}, now))
	assert.False(t, Effective(disabled, pgtype.Timestamptz{Time: now.Add(-time.Second), Valid: true}, now))
	assert.True(t, Effective(pgtype.Bool{Bool: true, Valid: true}, pgtype.Timestamptz{}, now))
}

func TestPendingHide(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	deadline := pgtype.Timestamptz{Time: now.Add(DisableGrace), Valid: true}
	assert.True(t, PendingHide(pgtype.Bool{Bool: false, Valid: true}, deadline, now))
	assert.False(t, PendingHide(pgtype.Bool{Bool: true, Valid: true}, deadline, now))
}
