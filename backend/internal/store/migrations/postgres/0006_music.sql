CREATE SCHEMA IF NOT EXISTS music;

CREATE TABLE IF NOT EXISTS music.library (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    root_path TEXT NOT NULL,
    scan_started_at BIGINT,
    scan_completed_at BIGINT,
    scan_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS music.artists (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS music.albums (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id BIGINT REFERENCES music.artists(id),
    name TEXT NOT NULL,
    UNIQUE (artist_id, name)
);

CREATE TABLE IF NOT EXISTS music.tracks (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    title TEXT NOT NULL,
    artist TEXT NOT NULL DEFAULT '',
    album TEXT NOT NULL DEFAULT '',
    artist_id BIGINT REFERENCES music.artists(id),
    album_id BIGINT REFERENCES music.albums(id),
    duration DOUBLE PRECISION NOT NULL DEFAULT 0,
    size BIGINT NOT NULL,
    modified_at BIGINT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    has_cover BOOLEAN NOT NULL DEFAULT FALSE,
    cover_stream INTEGER NOT NULL DEFAULT 0,
    available BOOLEAN NOT NULL DEFAULT TRUE,
    seen_scan BIGINT NOT NULL DEFAULT 0,
    indexed_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS music_tracks_search_idx ON music.tracks (lower(title), lower(artist), lower(album));
CREATE INDEX IF NOT EXISTS music_tracks_artist_idx ON music.tracks (artist_id) WHERE available;
CREATE INDEX IF NOT EXISTS music_tracks_album_idx ON music.tracks (album_id) WHERE available;
CREATE INDEX IF NOT EXISTS music_tracks_hash_idx ON music.tracks (content_hash) WHERE content_hash <> '';
CREATE INDEX IF NOT EXISTS music_tracks_seen_idx ON music.tracks (seen_scan);

CREATE TABLE IF NOT EXISTS music.playlists (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS music.playlist_tracks (
    playlist_id TEXT NOT NULL REFERENCES music.playlists(id) ON DELETE CASCADE,
    track_id TEXT NOT NULL REFERENCES music.tracks(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, track_id),
    UNIQUE (playlist_id, position)
);
CREATE TABLE IF NOT EXISTS music.favorites (
    track_id TEXT PRIMARY KEY REFERENCES music.tracks(id) ON DELETE CASCADE,
    added_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS music.plays (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    track_id TEXT NOT NULL REFERENCES music.tracks(id) ON DELETE CASCADE,
    played_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS music_plays_recent_idx ON music.plays (played_at DESC);
CREATE TABLE IF NOT EXISTS music.player_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    track_id TEXT REFERENCES music.tracks(id) ON DELETE SET NULL,
    position_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    shuffle BOOLEAN NOT NULL DEFAULT FALSE,
    repeat_mode TEXT NOT NULL DEFAULT 'off' CHECK (repeat_mode IN ('off','all','one'))
);
CREATE TABLE IF NOT EXISTS music.queue (
    position INTEGER PRIMARY KEY,
    track_id TEXT NOT NULL REFERENCES music.tracks(id) ON DELETE CASCADE
);
