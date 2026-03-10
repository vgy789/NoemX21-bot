ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS member_tags_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS member_tag_format TEXT NOT NULL DEFAULT 'login';

CREATE TABLE IF NOT EXISTS telegram_group_members (
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    is_member BOOLEAN NOT NULL DEFAULT true,
    is_bot BOOLEAN NOT NULL DEFAULT false,
    last_status TEXT NOT NULL DEFAULT 'member',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (chat_id, telegram_user_id)
);

CREATE INDEX IF NOT EXISTS idx_telegram_group_members_user_member
    ON telegram_group_members (telegram_user_id, is_member);
