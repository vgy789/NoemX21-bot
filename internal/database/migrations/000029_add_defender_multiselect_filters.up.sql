CREATE TABLE IF NOT EXISTS telegram_group_defender_campus_filters (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    campus_id UUID NOT NULL REFERENCES campuses(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, campus_id)
);

CREATE INDEX IF NOT EXISTS idx_tg_group_defender_campus_filters_chat
    ON telegram_group_defender_campus_filters (chat_id, created_at DESC);

CREATE TABLE IF NOT EXISTS telegram_group_defender_tribe_filters (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES telegram_groups(chat_id) ON DELETE CASCADE,
    campus_id UUID NOT NULL,
    coalition_id SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (chat_id, campus_id, coalition_id),
    CONSTRAINT telegram_group_defender_tribe_filters_coalition_fkey
        FOREIGN KEY (campus_id, coalition_id)
        REFERENCES coalitions(campus_id, id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_tg_group_defender_tribe_filters_chat
    ON telegram_group_defender_tribe_filters (chat_id, created_at DESC);

INSERT INTO telegram_group_defender_campus_filters (chat_id, campus_id)
SELECT chat_id, defender_filter_campus_id
FROM telegram_groups
WHERE defender_filter_campus_id IS NOT NULL
ON CONFLICT (chat_id, campus_id) DO NOTHING;

INSERT INTO telegram_group_defender_tribe_filters (chat_id, campus_id, coalition_id)
SELECT chat_id, defender_filter_campus_id, defender_filter_coalition_id
FROM telegram_groups
WHERE defender_filter_campus_id IS NOT NULL
  AND defender_filter_coalition_id IS NOT NULL
ON CONFLICT (chat_id, campus_id, coalition_id) DO NOTHING;
