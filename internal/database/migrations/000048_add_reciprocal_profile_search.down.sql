DROP INDEX IF EXISTS user_accounts_effective_telegram_visibility_idx;

ALTER TABLE user_accounts
    DROP COLUMN IF EXISTS telegram_visibility_ends_at;
