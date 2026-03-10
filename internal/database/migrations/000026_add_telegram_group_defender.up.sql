ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS defender_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS defender_remove_blocked BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS telegram_group_whitelists (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    added_by_account_id BIGINT NOT NULL REFERENCES user_accounts(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, telegram_user_id)
);

CREATE INDEX IF NOT EXISTS idx_telegram_group_whitelists_chat_created
    ON telegram_group_whitelists (chat_id, created_at DESC);

CREATE TABLE IF NOT EXISTS telegram_group_logs (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    telegram_user_id BIGINT NOT NULL DEFAULT 0,
    action TEXT NOT NULL,
    reason TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_telegram_group_logs_chat_created
    ON telegram_group_logs (chat_id, created_at DESC);
