CREATE TABLE IF NOT EXISTS telegram_group_legacy_access (
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('owner', 'moderator')),
    can_ban BOOLEAN NOT NULL DEFAULT false,
    can_mute BOOLEAN NOT NULL DEFAULT false,
    full_access BOOLEAN NOT NULL DEFAULT false,
    snapshot_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (chat_id, telegram_user_id)
);

CREATE INDEX IF NOT EXISTS idx_telegram_group_legacy_access_user
    ON telegram_group_legacy_access (telegram_user_id, chat_id);

INSERT INTO telegram_group_legacy_access (
    chat_id,
    telegram_user_id,
    source,
    can_ban,
    can_mute,
    full_access
)
SELECT
    chat_id,
    owner_telegram_user_id,
    'owner',
    true,
    true,
    true
FROM telegram_groups
WHERE owner_telegram_user_id > 0
  AND is_active = true
  AND is_initialized = true
ON CONFLICT (chat_id, telegram_user_id) DO UPDATE SET
    source = 'owner',
    can_ban = true,
    can_mute = true,
    full_access = true;

INSERT INTO telegram_group_legacy_access (
    chat_id,
    telegram_user_id,
    source,
    can_ban,
    can_mute,
    full_access
)
SELECT
    m.chat_id,
    m.telegram_user_id,
    'moderator',
    m.can_ban,
    m.can_mute,
    m.full_access
FROM telegram_group_moderators m
JOIN telegram_groups g ON g.chat_id = m.chat_id
WHERE g.is_active = true
  AND g.is_initialized = true
ON CONFLICT (chat_id, telegram_user_id) DO UPDATE SET
    can_ban = telegram_group_legacy_access.can_ban OR EXCLUDED.can_ban,
    can_mute = telegram_group_legacy_access.can_mute OR EXCLUDED.can_mute,
    full_access = telegram_group_legacy_access.full_access OR EXCLUDED.full_access;

CREATE TABLE IF NOT EXISTS telegram_group_legacy_moderation_actions (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    admin_telegram_user_id BIGINT NOT NULL,
    target_telegram_user_id BIGINT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('ban', 'kick')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_telegram_group_legacy_moderation_actions_recent
    ON telegram_group_legacy_moderation_actions (chat_id, admin_telegram_user_id, action, created_at DESC);

UPDATE telegram_groups
SET defender_enabled = false,
    defender_remove_blocked = false,
    defender_recheck_known_members = false,
    updated_at = CURRENT_TIMESTAMP
WHERE defender_enabled = true
   OR defender_remove_blocked = true
   OR defender_recheck_known_members = true;
