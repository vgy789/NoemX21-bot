CREATE TABLE telegram_groups (
    chat_id BIGINT PRIMARY KEY,
    chat_title TEXT NOT NULL,
    owner_telegram_user_id BIGINT NOT NULL,
    owner_telegram_username VARCHAR(255) NOT NULL DEFAULT '',
    is_initialized BOOLEAN NOT NULL DEFAULT false,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_telegram_groups_owner_active
    ON telegram_groups (owner_telegram_user_id, is_active, is_initialized);
