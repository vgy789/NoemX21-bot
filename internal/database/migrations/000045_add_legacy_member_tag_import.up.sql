CREATE TABLE legacy_member_tag_mappings (
    telegram_user_id BIGINT PRIMARY KEY,
    s21_login VARCHAR(32) NOT NULL UNIQUE,
    active_snapshot BOOLEAN NOT NULL,
    snapshot_observed_at TIMESTAMPTZ NOT NULL,
    source_digest CHAR(64) NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE legacy_member_tag_suppressions (
    id BIGSERIAL PRIMARY KEY,
    telegram_user_id BIGINT,
    s21_login VARCHAR(32),
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (telegram_user_id IS NOT NULL OR s21_login IS NOT NULL)
);

CREATE UNIQUE INDEX legacy_member_tag_suppressions_telegram_uidx
    ON legacy_member_tag_suppressions (telegram_user_id)
    WHERE telegram_user_id IS NOT NULL;
CREATE UNIQUE INDEX legacy_member_tag_suppressions_login_uidx
    ON legacy_member_tag_suppressions (LOWER(s21_login))
    WHERE s21_login IS NOT NULL;

CREATE TABLE legacy_member_tag_queue (
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL,
    desired_action TEXT NOT NULL DEFAULT 'apply' CHECK (desired_action IN ('apply', 'clear')),
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'applied', 'suppressed', 'failed')),
    last_applied_tag TEXT NOT NULL DEFAULT '',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (chat_id, telegram_user_id)
);

CREATE INDEX legacy_member_tag_queue_due_idx
    ON legacy_member_tag_queue (next_attempt_at)
    WHERE state = 'pending';
