DROP INDEX IF EXISTS idx_telegram_group_members_user_member;
DROP TABLE IF EXISTS telegram_group_members;

ALTER TABLE telegram_groups
    DROP COLUMN IF EXISTS member_tag_format,
    DROP COLUMN IF EXISTS member_tags_enabled;
