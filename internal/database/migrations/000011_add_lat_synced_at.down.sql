-- Rollback adding lat_synced_at
ALTER TABLE participant_stats_cache
DROP COLUMN IF EXISTS lat_synced_at;

DROP INDEX IF EXISTS idx_participant_stats_cache_lat_synced_at;
