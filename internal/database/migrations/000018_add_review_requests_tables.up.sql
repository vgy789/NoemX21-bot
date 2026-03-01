CREATE TYPE ENUM_REVIEW_STATUS AS ENUM ('SEARCHING', 'NEGOTIATING', 'PAUSED', 'CLOSED');

CREATE TABLE review_requests (
    id BIGSERIAL PRIMARY KEY,
    requester_user_id BIGINT NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    requester_s21_login VARCHAR(21) NOT NULL,
    requester_campus_id UUID REFERENCES campuses(id),
    project_id BIGINT NOT NULL,
    project_name VARCHAR(255) NOT NULL,
    project_type VARCHAR(64) NOT NULL,
    availability_text TEXT NOT NULL,
    requester_timezone VARCHAR(100) NOT NULL DEFAULT 'UTC',
    requester_timezone_offset VARCHAR(10) NOT NULL DEFAULT '+00:00',
    reviews_progress_text VARCHAR(128) NOT NULL DEFAULT 'n/a (school API does not provide this)',
    status ENUM_REVIEW_STATUS NOT NULL DEFAULT 'SEARCHING',
    view_count INT NOT NULL DEFAULT 0,
    response_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX uq_review_requests_open_per_project
ON review_requests (requester_user_id, project_id)
WHERE status <> 'CLOSED';

CREATE INDEX idx_review_requests_status_project_created
ON review_requests (status, project_id, created_at DESC);

CREATE INDEX idx_review_requests_requester_status_created
ON review_requests (requester_user_id, status, created_at DESC);
