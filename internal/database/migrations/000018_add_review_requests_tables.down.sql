DROP INDEX IF EXISTS idx_review_requests_requester_status_created;
DROP INDEX IF EXISTS idx_review_requests_status_project_created;
DROP INDEX IF EXISTS uq_review_requests_open_per_project;

DROP TABLE IF EXISTS review_requests;

DROP TYPE IF EXISTS ENUM_REVIEW_STATUS;
