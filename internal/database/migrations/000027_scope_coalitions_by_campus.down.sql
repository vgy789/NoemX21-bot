-- Rollback to global coalitions(id).

ALTER TABLE participant_stats_cache
    DROP CONSTRAINT IF EXISTS participant_stats_cache_coalition_id_fkey;

ALTER TABLE coalitions RENAME TO coalitions_scoped;

CREATE TABLE coalitions (
    id SMALLINT PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Collapse scoped coalitions back to one row per ID.
WITH by_id AS (
    SELECT DISTINCT ON (id)
        id,
        name,
        created_at
    FROM coalitions_scoped
    ORDER BY id, created_at
),
dedup AS (
    SELECT
        id,
        name,
        created_at,
        ROW_NUMBER() OVER (PARTITION BY name ORDER BY id) AS name_rank
    FROM by_id
)
INSERT INTO coalitions (id, name, created_at)
SELECT
    id,
    CASE
        WHEN name_rank = 1 THEN name
        ELSE name || ' #' || id::text
    END,
    created_at
FROM dedup;

UPDATE participant_stats_cache p
SET coalition_id = NULL
WHERE p.coalition_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM coalitions c
      WHERE c.id = p.coalition_id
  );

ALTER TABLE participant_stats_cache
    ADD CONSTRAINT participant_stats_cache_coalition_id_fkey
    FOREIGN KEY (coalition_id)
    REFERENCES coalitions(id);

DROP TABLE coalitions_scoped;
