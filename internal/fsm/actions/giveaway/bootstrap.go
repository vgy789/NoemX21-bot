package giveaway

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

type schemaBootstrapper interface {
	Exec(ctx context.Context, sql string, args ...any) error
}

const giveawaySchemaBootstrapSQL = `
CREATE TABLE IF NOT EXISTS sapphire_giveaway_sync_jobs (
    id BIGSERIAL PRIMARY KEY,
    status VARCHAR(16) NOT NULL CHECK (status IN ('running', 'finished', 'failed')),
    total_count INT NOT NULL DEFAULT 0,
    processed_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    export_text TEXT NOT NULL DEFAULT '',
    requested_by_telegram_user_id BIGINT NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sapphire_giveaway_state (
    contest_key VARCHAR(64) PRIMARY KEY,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'finalizing', 'finished')),
    final_sync_job_id BIGINT REFERENCES sapphire_giveaway_sync_jobs(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sapphire_giveaway_participants (
    s21_login VARCHAR(21) PRIMARY KEY REFERENCES registered_users(s21_login) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    baseline_project_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    counted_project_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    counted_projects_count INT NOT NULL DEFAULT 0,
    joined_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_synced_at TIMESTAMP WITH TIME ZONE,
    last_sync_error TEXT NOT NULL DEFAULT '',
    last_final_sync_job_id BIGINT REFERENCES sapphire_giveaway_sync_jobs(id)
);

CREATE INDEX IF NOT EXISTS idx_sapphire_giveaway_participants_counted
    ON sapphire_giveaway_participants (counted_projects_count DESC, s21_login ASC);
CREATE INDEX IF NOT EXISTS idx_sapphire_giveaway_participants_telegram_user
    ON sapphire_giveaway_participants (telegram_user_id);
`

func ensureContestState(ctx context.Context, queries db.Querier) (db.SapphireGiveawayState, error) {
	if err := queries.CreateSapphireGiveawayStateIfMissing(ctx, db.CreateSapphireGiveawayStateIfMissingParams{
		ContestKey: contestKey,
		Status:     stateActive,
	}); err != nil {
		if !isUndefinedTableError(err) {
			return db.SapphireGiveawayState{}, err
		}
		if bootstrapErr := bootstrapGiveawaySchema(ctx, queries); bootstrapErr != nil {
			return db.SapphireGiveawayState{}, errors.Join(err, bootstrapErr)
		}
		if retryErr := queries.CreateSapphireGiveawayStateIfMissing(ctx, db.CreateSapphireGiveawayStateIfMissingParams{
			ContestKey: contestKey,
			Status:     stateActive,
		}); retryErr != nil {
			return db.SapphireGiveawayState{}, retryErr
		}
	}

	state, err := queries.GetSapphireGiveawayState(ctx, contestKey)
	if err == nil || !isUndefinedTableError(err) {
		return state, err
	}
	if bootstrapErr := bootstrapGiveawaySchema(ctx, queries); bootstrapErr != nil {
		return db.SapphireGiveawayState{}, errors.Join(err, bootstrapErr)
	}
	return queries.GetSapphireGiveawayState(ctx, contestKey)
}

func bootstrapGiveawaySchema(ctx context.Context, queries db.Querier) error {
	execQueries, ok := queries.(schemaBootstrapper)
	if !ok {
		return errors.New("giveaway schema bootstrap is unavailable for this querier")
	}
	return execQueries.Exec(ctx, giveawaySchemaBootstrapSQL)
}

func isUndefinedTableError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01"
	}

	return strings.Contains(strings.ToLower(err.Error()), "does not exist")
}
