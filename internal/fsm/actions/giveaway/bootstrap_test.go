package giveaway

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	dbMock "github.com/vgy789/noemx21-bot/internal/database/db/mock"
	"go.uber.org/mock/gomock"
)

type schemaAwareQuerier struct {
	db.Querier
	execFn func(ctx context.Context, sql string, args ...any) error
}

func (q *schemaAwareQuerier) Exec(ctx context.Context, sql string, args ...any) error {
	return q.execFn(ctx, sql, args...)
}

func TestEnsureContestStateBootstrapsMissingSchema(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockQ := dbMock.NewMockQuerier(ctrl)

	bootstrapCalled := false
	queries := &schemaAwareQuerier{
		Querier: mockQ,
		execFn: func(_ context.Context, sql string, _ ...any) error {
			bootstrapCalled = true
			require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sapphire_giveaway_state")
			require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sapphire_giveaway_participants")
			return nil
		},
	}

	missingRelationErr := &pgconn.PgError{
		Code:    "42P01",
		Message: `relation "sapphire_giveaway_state" does not exist`,
	}
	expectedState := db.SapphireGiveawayState{
		ContestKey: contestKey,
		Status:     stateActive,
	}

	gomock.InOrder(
		mockQ.EXPECT().
			CreateSapphireGiveawayStateIfMissing(gomock.Any(), db.CreateSapphireGiveawayStateIfMissingParams{
				ContestKey: contestKey,
				Status:     stateActive,
			}).
			Return(missingRelationErr),
		mockQ.EXPECT().
			CreateSapphireGiveawayStateIfMissing(gomock.Any(), db.CreateSapphireGiveawayStateIfMissingParams{
				ContestKey: contestKey,
				Status:     stateActive,
			}).
			Return(nil),
		mockQ.EXPECT().
			GetSapphireGiveawayState(gomock.Any(), contestKey).
			Return(expectedState, nil),
	)

	state, err := ensureContestState(context.Background(), queries)
	require.NoError(t, err)
	require.True(t, bootstrapCalled)
	require.Equal(t, expectedState, state)
}

func TestIsUndefinedTableError(t *testing.T) {
	t.Parallel()

	require.True(t, isUndefinedTableError(&pgconn.PgError{Code: "42P01"}))
	require.True(t, isUndefinedTableError(assertErr("relation does not exist")))
	require.False(t, isUndefinedTableError(assertErr("permission denied")))
}

type assertErr string

func (e assertErr) Error() string {
	return strings.TrimSpace(string(e))
}
