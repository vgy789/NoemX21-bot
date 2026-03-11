ALTER TYPE enum_review_status ADD VALUE IF NOT EXISTS 'WITHDRAWN';

DROP INDEX IF EXISTS uq_review_requests_open_per_project;
CREATE UNIQUE INDEX uq_review_requests_open_per_project
ON review_requests (requester_user_id, project_id)
WHERE status <> 'CLOSED';

ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS is_forum BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS prr_notifications_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS prr_notifications_thread_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS prr_notifications_thread_label TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prr_withdrawn_behavior TEXT NOT NULL DEFAULT 'stub';

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_prr_withdrawn_behavior_check;

ALTER TABLE telegram_groups
    ADD CONSTRAINT telegram_groups_prr_withdrawn_behavior_check
    CHECK (prr_withdrawn_behavior IN ('stub', 'delete'));

CREATE TABLE IF NOT EXISTS telegram_group_prr_project_filters (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, project_id)
);

CREATE INDEX IF NOT EXISTS idx_tg_group_prr_project_filters_chat
    ON telegram_group_prr_project_filters (chat_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tg_group_prr_project_filters_project
    ON telegram_group_prr_project_filters (project_id);

CREATE TABLE IF NOT EXISTS telegram_group_prr_campus_filters (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    campus_id UUID NOT NULL REFERENCES campuses(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, campus_id)
);

CREATE INDEX IF NOT EXISTS idx_tg_group_prr_campus_filters_chat
    ON telegram_group_prr_campus_filters (chat_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tg_group_prr_campus_filters_campus
    ON telegram_group_prr_campus_filters (campus_id);

CREATE TABLE IF NOT EXISTS telegram_group_prr_messages (
    id BIGSERIAL PRIMARY KEY,
    review_request_id BIGINT NOT NULL REFERENCES review_requests(id) ON DELETE CASCADE,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    message_id BIGINT NOT NULL,
    message_thread_id BIGINT NOT NULL DEFAULT 0,
    last_rendered_status enum_review_status NOT NULL DEFAULT 'SEARCHING',
    last_rendered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (review_request_id, chat_id)
);

CREATE INDEX IF NOT EXISTS idx_tg_group_prr_messages_request
    ON telegram_group_prr_messages (review_request_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_tg_group_prr_messages_chat
    ON telegram_group_prr_messages (chat_id, updated_at DESC);
