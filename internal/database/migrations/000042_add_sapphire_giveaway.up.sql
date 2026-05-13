CREATE TABLE sapphire_giveaway_sync_jobs (
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

CREATE TABLE sapphire_giveaway_state (
    contest_key VARCHAR(64) PRIMARY KEY,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'finalizing', 'finished')),
    final_sync_job_id BIGINT REFERENCES sapphire_giveaway_sync_jobs(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sapphire_giveaway_participants (
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

CREATE INDEX idx_sapphire_giveaway_participants_counted
    ON sapphire_giveaway_participants (counted_projects_count DESC, s21_login ASC);
CREATE INDEX idx_sapphire_giveaway_participants_telegram_user
    ON sapphire_giveaway_participants (telegram_user_id);

INSERT INTO sapphire_giveaway_state (contest_key, status)
VALUES ('sapphire_100_coins', 'active')
ON CONFLICT (contest_key) DO NOTHING;
