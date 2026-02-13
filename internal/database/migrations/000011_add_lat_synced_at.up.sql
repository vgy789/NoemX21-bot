-- Add separate timestamp for API sync vs DB update
ALTER TABLE participant_stats_cache
ADD COLUMN lat_synced_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_participant_stats_cache_lat_synced_at ON participant_stats_cache(lat_synced_at);
