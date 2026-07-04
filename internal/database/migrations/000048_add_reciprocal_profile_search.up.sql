ALTER TABLE user_accounts
    ADD COLUMN telegram_visibility_ends_at TIMESTAMPTZ;

CREATE INDEX user_accounts_effective_telegram_visibility_idx
    ON user_accounts (platform, external_id, telegram_visibility_ends_at)
    WHERE is_searchable = TRUE OR telegram_visibility_ends_at IS NOT NULL;
