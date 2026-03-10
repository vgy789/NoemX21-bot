-- Scope coalitions by campus: coalition ID can repeat across different campuses.

-- Drop old single-column FK from participant stats to coalitions(id).
ALTER TABLE participant_stats_cache
    DROP CONSTRAINT IF EXISTS participant_stats_cache_coalition_id_fkey;

-- Preserve old data while rebuilding table structure.
ALTER TABLE coalitions RENAME TO coalitions_legacy;

CREATE TABLE coalitions (
    campus_id UUID NOT NULL REFERENCES campuses(id),
    id SMALLINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campus_id, id),
    UNIQUE (campus_id, name)
);

-- Recreate coalition rows per campus using participant stats cache links.
INSERT INTO coalitions (campus_id, id, name, created_at)
SELECT DISTINCT
    p.campus_id,
    c.id,
    c.name,
    c.created_at
FROM coalitions_legacy c
JOIN participant_stats_cache p ON p.coalition_id = c.id
WHERE p.campus_id IS NOT NULL;

-- Keep cache rows valid for the new composite FK.
UPDATE participant_stats_cache p
SET coalition_id = NULL
WHERE p.coalition_id IS NOT NULL
  AND (
      p.campus_id IS NULL
      OR NOT EXISTS (
          SELECT 1
          FROM coalitions c
          WHERE c.campus_id = p.campus_id
            AND c.id = p.coalition_id
      )
  );

ALTER TABLE participant_stats_cache
    ADD CONSTRAINT participant_stats_cache_coalition_id_fkey
    FOREIGN KEY (campus_id, coalition_id)
    REFERENCES coalitions(campus_id, id);

DROP TABLE coalitions_legacy;
