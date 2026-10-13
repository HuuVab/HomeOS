ALTER TABLE music.tracks ADD COLUMN loudness_lufs DOUBLE PRECISION;
ALTER TABLE music.tracks ADD COLUMN loudness_peak_dbfs DOUBLE PRECISION;
ALTER TABLE music.tracks ADD COLUMN loudness_checked_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX music_tracks_loudness_pending_idx ON music.tracks (indexed_at DESC) WHERE available AND loudness_checked_at = 0;
