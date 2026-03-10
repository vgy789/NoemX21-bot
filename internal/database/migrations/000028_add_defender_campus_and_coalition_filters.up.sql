ALTER TABLE telegram_groups
    ADD COLUMN IF NOT EXISTS defender_filter_campus_id UUID NULL REFERENCES campuses(id),
    ADD COLUMN IF NOT EXISTS defender_filter_coalition_id SMALLINT NULL;

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_defender_filter_coalition_requires_campus;
ALTER TABLE telegram_groups
    ADD CONSTRAINT telegram_groups_defender_filter_coalition_requires_campus
    CHECK (defender_filter_coalition_id IS NULL OR defender_filter_campus_id IS NOT NULL);

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_defender_filter_coalition_fkey;
ALTER TABLE telegram_groups
    ADD CONSTRAINT telegram_groups_defender_filter_coalition_fkey
    FOREIGN KEY (defender_filter_campus_id, defender_filter_coalition_id)
    REFERENCES coalitions(campus_id, id);
