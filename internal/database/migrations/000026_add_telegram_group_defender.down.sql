DROP INDEX IF EXISTS idx_telegram_group_logs_chat_created;
DROP TABLE IF EXISTS telegram_group_logs;

DROP INDEX IF EXISTS idx_telegram_group_whitelists_chat_created;
DROP TABLE IF EXISTS telegram_group_whitelists;

ALTER TABLE telegram_groups
    DROP COLUMN IF EXISTS defender_remove_blocked,
    DROP COLUMN IF EXISTS defender_enabled;
