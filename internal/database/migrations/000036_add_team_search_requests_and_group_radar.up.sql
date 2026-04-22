CREATE TABLE team_search_requests (
    id BIGSERIAL PRIMARY KEY,
    requester_user_id BIGINT NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    requester_s21_login VARCHAR(21) NOT NULL,
    requester_campus_id UUID REFERENCES campuses(id),
    project_id BIGINT NOT NULL,
    project_name VARCHAR(255) NOT NULL,
    project_type VARCHAR(64) NOT NULL,
    planned_start_text TEXT NOT NULL,
    request_note_text TEXT NOT NULL,
    requester_timezone VARCHAR(100) NOT NULL DEFAULT 'UTC',
    requester_timezone_offset VARCHAR(10) NOT NULL DEFAULT '+00:00',
    status enum_review_status NOT NULL DEFAULT 'SEARCHING',
    view_count INT NOT NULL DEFAULT 0,
    response_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at TIMESTAMP WITH TIME ZONE,
    negotiating_peer_user_id BIGINT REFERENCES user_accounts(id) ON DELETE SET NULL,
    negotiating_peer_s21_login VARCHAR(100),
    negotiating_peer_telegram_username VARCHAR(64),
    negotiating_peer_rocketchat_id VARCHAR(100),
    negotiating_peer_alternative_contact VARCHAR(255),
    negotiating_started_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_team_search_requests_open_per_project
ON team_search_requests (requester_user_id, project_id)
WHERE status NOT IN ('CLOSED', 'WITHDRAWN');

CREATE INDEX idx_team_search_requests_status_project_created
ON team_search_requests (status, project_id, created_at DESC);

CREATE INDEX idx_team_search_requests_requester_status_created
ON team_search_requests (requester_user_id, status, created_at DESC);

ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS team_notifications_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS team_notifications_thread_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS team_notifications_thread_label TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS team_withdrawn_behavior TEXT NOT NULL DEFAULT 'stub';

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_team_withdrawn_behavior_check;

ALTER TABLE telegram_groups
    ADD CONSTRAINT telegram_groups_team_withdrawn_behavior_check
    CHECK (team_withdrawn_behavior IN ('stub', 'delete'));

CREATE TABLE telegram_group_team_project_filters (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, project_id)
);

CREATE INDEX idx_tg_group_team_project_filters_chat
    ON telegram_group_team_project_filters (chat_id, created_at DESC);

CREATE INDEX idx_tg_group_team_project_filters_project
    ON telegram_group_team_project_filters (project_id);

CREATE TABLE telegram_group_team_campus_filters (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    campus_id UUID NOT NULL REFERENCES campuses(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, campus_id)
);

CREATE INDEX idx_tg_group_team_campus_filters_chat
    ON telegram_group_team_campus_filters (chat_id, created_at DESC);

CREATE INDEX idx_tg_group_team_campus_filters_campus
    ON telegram_group_team_campus_filters (campus_id);

CREATE TABLE telegram_group_team_messages (
    id BIGSERIAL PRIMARY KEY,
    team_search_request_id BIGINT NOT NULL REFERENCES team_search_requests(id) ON DELETE CASCADE,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    message_id BIGINT NOT NULL,
    message_thread_id BIGINT NOT NULL DEFAULT 0,
    last_rendered_status enum_review_status NOT NULL DEFAULT 'SEARCHING',
    last_rendered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (team_search_request_id, chat_id)
);

CREATE INDEX idx_tg_group_team_messages_request
    ON telegram_group_team_messages (team_search_request_id, updated_at DESC);

CREATE INDEX idx_tg_group_team_messages_chat
    ON telegram_group_team_messages (chat_id, updated_at DESC);
