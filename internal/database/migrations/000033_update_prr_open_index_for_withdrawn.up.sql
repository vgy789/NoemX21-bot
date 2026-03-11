DROP INDEX IF EXISTS uq_review_requests_open_per_project;
CREATE UNIQUE INDEX uq_review_requests_open_per_project
ON review_requests (requester_user_id, project_id)
WHERE status NOT IN ('CLOSED', 'WITHDRAWN');
