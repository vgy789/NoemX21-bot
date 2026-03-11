ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS defender_ban_duration_sec INT NOT NULL DEFAULT 86400;
