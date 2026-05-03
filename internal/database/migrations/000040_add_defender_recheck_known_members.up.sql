ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS defender_recheck_known_members BOOLEAN NOT NULL DEFAULT false;
