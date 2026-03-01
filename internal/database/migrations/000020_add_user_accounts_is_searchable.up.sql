ALTER TABLE user_accounts
    ADD COLUMN IF NOT EXISTS is_searchable BOOLEAN DEFAULT true;

UPDATE user_accounts
SET is_searchable = true
WHERE is_searchable IS NULL;
