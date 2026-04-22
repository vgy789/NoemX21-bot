DROP TABLE IF EXISTS telegram_group_team_messages;
DROP TABLE IF EXISTS telegram_group_team_campus_filters;
DROP TABLE IF EXISTS telegram_group_team_project_filters;

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_team_withdrawn_behavior_check,
    DROP COLUMN IF EXISTS team_withdrawn_behavior,
    DROP COLUMN IF EXISTS team_notifications_thread_label,
    DROP COLUMN IF EXISTS team_notifications_thread_id,
    DROP COLUMN IF EXISTS team_notifications_enabled;

DROP INDEX IF EXISTS idx_team_search_requests_requester_status_created;
DROP INDEX IF EXISTS idx_team_search_requests_status_project_created;
DROP INDEX IF EXISTS uq_team_search_requests_open_per_project;
DROP TABLE IF EXISTS team_search_requests;
