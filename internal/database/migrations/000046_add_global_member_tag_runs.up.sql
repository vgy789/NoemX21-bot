CREATE TABLE global_member_tag_runs (
    id BIGSERIAL PRIMARY KEY,
    owner_telegram_user_id BIGINT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('running', 'cancelling', 'completed', 'cancelled')),
    eligible_groups INTEGER NOT NULL DEFAULT 0,
    candidate_profiles INTEGER NOT NULL DEFAULT 0,
    total_items BIGINT NOT NULL DEFAULT 0,
    processed_items BIGINT NOT NULL DEFAULT 0,
    discovered_members BIGINT NOT NULL DEFAULT 0,
    verified_members BIGINT NOT NULL DEFAULT 0,
    updated_tags BIGINT NOT NULL DEFAULT 0,
    preserved_tags BIGINT NOT NULL DEFAULT 0,
    not_members BIGINT NOT NULL DEFAULT 0,
    skipped_no_rights BIGINT NOT NULL DEFAULT 0,
    error_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX global_member_tag_runs_active_uidx
    ON global_member_tag_runs ((true)) WHERE state IN ('running', 'cancelling');

CREATE TABLE global_member_tag_run_items (
    run_id BIGINT NOT NULL REFERENCES global_member_tag_runs(id) ON DELETE CASCADE,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'retry')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, chat_id, telegram_user_id)
);

CREATE INDEX global_member_tag_run_items_due_idx
    ON global_member_tag_run_items (next_attempt_at) WHERE state IN ('pending', 'retry');
