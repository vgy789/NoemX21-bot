CREATE TABLE IF NOT EXISTS telegram_group_moderators (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    can_ban BOOLEAN NOT NULL DEFAULT false,
    can_mute BOOLEAN NOT NULL DEFAULT false,
    full_access BOOLEAN NOT NULL DEFAULT false,
    added_by_account_id BIGINT NOT NULL REFERENCES user_accounts(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, telegram_user_id)
);

CREATE INDEX IF NOT EXISTS idx_telegram_group_moderators_chat_updated
    ON telegram_group_moderators (chat_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_telegram_group_moderators_user_full_access
    ON telegram_group_moderators (telegram_user_id, full_access);
