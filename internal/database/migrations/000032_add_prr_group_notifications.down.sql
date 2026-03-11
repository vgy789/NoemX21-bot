DROP TABLE IF EXISTS telegram_group_prr_messages;
DROP TABLE IF EXISTS telegram_group_prr_campus_filters;
DROP TABLE IF EXISTS telegram_group_prr_project_filters;

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_prr_withdrawn_behavior_check;

ALTER TABLE telegram_groups
    DROP COLUMN IF EXISTS prr_withdrawn_behavior,
    DROP COLUMN IF EXISTS prr_notifications_thread_label,
    DROP COLUMN IF EXISTS prr_notifications_thread_id,
    DROP COLUMN IF EXISTS prr_notifications_enabled,
    DROP COLUMN IF EXISTS is_forum;

DROP INDEX IF EXISTS uq_review_requests_open_per_project;
CREATE UNIQUE INDEX uq_review_requests_open_per_project
ON review_requests (requester_user_id, project_id)
WHERE status <> 'CLOSED';

-- PostgreSQL does not support dropping enum values safely in down migration.
