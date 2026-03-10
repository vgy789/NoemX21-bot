ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_defender_filter_coalition_fkey;

ALTER TABLE telegram_groups
    DROP CONSTRAINT IF EXISTS telegram_groups_defender_filter_coalition_requires_campus;

ALTER TABLE telegram_groups
    DROP COLUMN IF EXISTS defender_filter_coalition_id,
    DROP COLUMN IF EXISTS defender_filter_campus_id;
