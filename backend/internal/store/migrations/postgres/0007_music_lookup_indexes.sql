CREATE INDEX IF NOT EXISTS music_tracks_artist_name_idx ON music.tracks (artist) WHERE available;
CREATE INDEX IF NOT EXISTS music_tracks_album_name_idx ON music.tracks (album) WHERE available;
