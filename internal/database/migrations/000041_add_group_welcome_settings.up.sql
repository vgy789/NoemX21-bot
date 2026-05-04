ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS welcome_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS welcome_thread_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS welcome_thread_label TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS welcome_delete_service_messages BOOLEAN NOT NULL DEFAULT true;
