ALTER TABLE telegram_groups
    DROP COLUMN IF EXISTS welcome_delete_service_messages,
    DROP COLUMN IF EXISTS welcome_thread_label,
    DROP COLUMN IF EXISTS welcome_thread_id,
    DROP COLUMN IF EXISTS welcome_enabled;
