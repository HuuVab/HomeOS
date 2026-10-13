CREATE INDEX IF NOT EXISTS music_tracks_artist_fold_idx ON music.tracks (lower(artist)) WHERE available;
CREATE INDEX IF NOT EXISTS music_tracks_album_fold_idx ON music.tracks (lower(album)) WHERE available;
CREATE INDEX IF NOT EXISTS music_tracks_duration_idx ON music.tracks (duration) WHERE available;
