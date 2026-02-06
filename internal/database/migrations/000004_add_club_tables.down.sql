DROP TABLE IF EXISTS clubs;
DROP TABLE IF EXISTS club_categories;

-- We don't necessarily need to drop NOT NULL on campuses as it's a hardening change,
-- but for symmetry if we want to revert completely:
ALTER TABLE campuses ALTER COLUMN is_active DROP NOT NULL;
