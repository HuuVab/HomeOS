package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type MusicPlaylist struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	TrackIDs []string `json:"trackIds"`
}
type MusicPlayerState struct {
	TrackID  string  `json:"trackId"`
	Position float64 `json:"position"`
	Shuffle  bool    `json:"shuffle"`
	Repeat   string  `json:"repeat"`
}
type MusicPersonal struct {
	Favorites  []string         `json:"favorites"`
	Recent     []MusicTrack     `json:"recent"`
	PlayCounts map[string]int   `json:"playCounts"`
	Playlists  []MusicPlaylist  `json:"playlists"`
	Queue      []string         `json:"queue"`
	Player     MusicPlayerState `json:"player"`
}

const qualifiedMusicTrackColumns = `t.id,t.path,t.name,t.title,t.artist,t.album,t.duration,t.size,t.modified_at,t.has_cover,t.cover_stream,t.available`

func (s *Store) MusicCollectionTracks(ctx context.Context, collection, id string) ([]MusicTrack, error) {
	statement := `SELECT ` + qualifiedMusicTrackColumns + ` FROM music.favorites f JOIN music.tracks t ON t.id=f.track_id WHERE t.available ORDER BY f.added_at DESC`
	args := []any{}
	if collection == "playlist" {
		statement = `SELECT ` + qualifiedMusicTrackColumns + ` FROM music.playlist_tracks p JOIN music.tracks t ON t.id=p.track_id WHERE p.playlist_id=$1 AND t.available ORDER BY p.position`
		args = []any{id}
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tracks := make([]MusicTrack, 0)
	for rows.Next() {
		t, err := scanMusicTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (s *Store) MusicPersonal(ctx context.Context) (MusicPersonal, error) {
	v := MusicPersonal{Favorites: []string{}, Recent: []MusicTrack{}, PlayCounts: map[string]int{}, Playlists: []MusicPlaylist{}, Queue: []string{}, Player: MusicPlayerState{Repeat: "off"}}
	rows, err := s.db.QueryContext(ctx, `SELECT track_id FROM music.favorites ORDER BY added_at DESC`)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return v, err
		}
		v.Favorites = append(v.Favorites, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT `+qualifiedMusicTrackColumns+` FROM music.plays p JOIN music.tracks t ON t.id=p.track_id ORDER BY p.played_at DESC,p.id DESC LIMIT 30`)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		t, err := scanMusicTrack(rows)
		if err != nil {
			rows.Close()
			return v, err
		}
		v.Recent = append(v.Recent, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT track_id,count(*) FROM music.plays GROUP BY track_id`)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			rows.Close()
			return v, err
		}
		v.PlayCounts[id] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,name FROM music.playlists ORDER BY created_at,id`)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var p MusicPlaylist
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			rows.Close()
			return v, err
		}
		p.TrackIDs = []string{}
		v.Playlists = append(v.Playlists, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	for i := range v.Playlists {
		rows, err = s.db.QueryContext(ctx, `SELECT track_id FROM music.playlist_tracks WHERE playlist_id=$1 ORDER BY position`, v.Playlists[i].ID)
		if err != nil {
			return v, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return v, err
			}
			v.Playlists[i].TrackIDs = append(v.Playlists[i].TrackIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return v, err
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT track_id FROM music.queue ORDER BY position`)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return v, err
		}
		v.Queue = append(v.Queue, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	var id sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT track_id,position_seconds,shuffle,repeat_mode FROM music.player_state WHERE id=1`).Scan(&id, &v.Player.Position, &v.Player.Shuffle, &v.Player.Repeat)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	v.Player.TrackID = id.String
	return v, nil
}

func (s *Store) SetMusicFavorite(ctx context.Context, id string, liked bool) error {
	if liked {
		_, err := s.db.ExecContext(ctx, `INSERT INTO music.favorites(track_id,added_at) VALUES($1,$2) ON CONFLICT(track_id) DO NOTHING`, id, time.Now().Unix())
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM music.favorites WHERE track_id=$1`, id)
	return err
}

func (s *Store) CreateMusicPlaylist(ctx context.Context, name string) (string, error) {
	id := NewMusicID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO music.playlists(id,name,created_at) VALUES($1,$2,$3)`, id, name, time.Now().Unix())
	return id, err
}
func (s *Store) DeleteMusicPlaylist(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM music.playlists WHERE id=$1`, id)
	return err
}
func (s *Store) SetMusicPlaylistTracks(ctx context.Context, id string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM music.playlist_tracks WHERE playlist_id=$1`, id); err != nil {
		return err
	}
	for i, trackID := range ids {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music.playlist_tracks(playlist_id,track_id,position) VALUES($1,$2,$3)`, id, trackID, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) SetMusicQueue(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM music.queue`); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music.queue(position,track_id) VALUES($1,$2)`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) SetMusicPlayer(ctx context.Context, p MusicPlayerState) error {
	if p.Repeat != "off" && p.Repeat != "all" && p.Repeat != "one" {
		p.Repeat = "off"
	}
	if p.Position < 0 {
		p.Position = 0
	}
	var id any
	if p.TrackID != "" {
		id = p.TrackID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO music.player_state(id,track_id,position_seconds,shuffle,repeat_mode) VALUES(1,$1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET track_id=EXCLUDED.track_id,position_seconds=EXCLUDED.position_seconds,shuffle=EXCLUDED.shuffle,repeat_mode=EXCLUDED.repeat_mode`, id, p.Position, p.Shuffle, p.Repeat)
	return err
}
