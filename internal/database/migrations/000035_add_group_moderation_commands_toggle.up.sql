ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS moderation_commands_enabled BOOLEAN NOT NULL DEFAULT true;
