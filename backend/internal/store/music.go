package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MusicTrack struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Duration    float64 `json:"duration"`
	Size        int64   `json:"size"`
	ModifiedAt  int64   `json:"modifiedAt"`
	ContentHash string  `json:"-"`
	HasCover    bool    `json:"hasCover"`
	CoverStream int     `json:"-"`
	Available   bool    `json:"available"`
}

type MusicLibrary struct {
	Root            string `json:"root"`
	ScanStartedAt   int64  `json:"scanStartedAt"`
	ScanCompletedAt int64  `json:"scanCompletedAt"`
	ScanError       string `json:"scanError"`
}

func musicPathPrefix(root string) string {
	if root == "" {
		return ""
	}
	if strings.HasSuffix(root, string(filepath.Separator)) {
		return root
	}
	return root + string(filepath.Separator)
}

func NewMusicID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (s *Store) MusicLibrary(ctx context.Context) (MusicLibrary, error) {
	var v MusicLibrary
	if !s.IsPostgres() {
		return v, nil
	}
	err := s.db.QueryRowContext(ctx, `SELECT root_path,COALESCE(scan_started_at,0),COALESCE(scan_completed_at,0),scan_error FROM music.library WHERE id=1`).Scan(&v.Root, &v.ScanStartedAt, &v.ScanCompletedAt, &v.ScanError)
	if errors.Is(err, sql.ErrNoRows) {
		return MusicLibrary{}, nil
	}
	return v, err
}

func (s *Store) SetMusicLibrary(ctx context.Context, root string) error {
	root = filepath.Clean(root)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT root_path FROM music.library WHERE id=1 FOR UPDATE`).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous != "" && previous != root {
		if _, err = tx.ExecContext(ctx, `UPDATE music.tracks SET available=false WHERE available=true`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO music.library(id,root_path) VALUES(1,$1) ON CONFLICT(id) DO UPDATE SET root_path=EXCLUDED.root_path,scan_started_at=NULL,scan_completed_at=NULL,scan_error=''`, root)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MusicScanStart(ctx context.Context, root string, stamp int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music.library SET scan_started_at=$1,scan_error='' WHERE id=1 AND root_path=$2`, stamp, root)
	return err
}
func (s *Store) MusicScanFinish(ctx context.Context, root string, stamp int64, scanError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music.library SET scan_completed_at=$1,scan_error=$2 WHERE id=1 AND root_path=$3`, stamp, scanError, root)
	return err
}

func (s *Store) MusicDeactivateRoot(ctx context.Context, root string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music.tracks SET available=false WHERE left(path,length($1))=$1`, musicPathPrefix(root))
	return err
}

func scanMusicTrack(rows *sql.Rows) (MusicTrack, error) {
	var t MusicTrack
	err := rows.Scan(&t.ID, &t.Path, &t.Name, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.Size, &t.ModifiedAt, &t.HasCover, &t.CoverStream, &t.Available)
	return t, err
}

const musicTrackColumns = `id,path,name,title,artist,album,duration,size,modified_at,has_cover,cover_stream,available`

func (s *Store) MusicTracks(ctx context.Context, root, query, artist, album, folder, sortKey string, descending bool, limit, offset int) ([]MusicTrack, error) {
	if limit < 1 || limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	order := map[string]string{"title": "lower(title)", "artist": "lower(artist)", "album": "lower(album)", "duration": "duration"}[sortKey]
	if order == "" {
		order = "lower(title)"
	}
	if descending {
		order += " DESC"
	} else {
		order += " ASC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+musicTrackColumns+` FROM music.tracks WHERE available=true AND $1<>'' AND left(path,length($1))=$1 AND ($2='' OR lower(title) LIKE '%'||lower($2)||'%' OR lower(artist) LIKE '%'||lower($2)||'%' OR lower(album) LIKE '%'||lower($2)||'%') AND ($3='' OR lower(artist)=lower($3)) AND ($4='' OR lower(album)=lower($4)) AND ($5='' OR left(path,length($5))=$5) ORDER BY `+order+`,id LIMIT $6 OFFSET $7`, musicPathPrefix(root), query, artist, album, musicPathPrefix(folder), limit, offset)
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

func (s *Store) MusicTrackByID(ctx context.Context, id string) (MusicTrack, error) {
	var t MusicTrack
	err := s.db.QueryRowContext(ctx, `SELECT `+musicTrackColumns+` FROM music.tracks WHERE id=$1`, id).Scan(&t.ID, &t.Path, &t.Name, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.Size, &t.ModifiedAt, &t.HasCover, &t.CoverStream, &t.Available)
	return t, err
}

func (s *Store) MusicTrackAtPath(ctx context.Context, path string) (MusicTrack, string, error) {
	var t MusicTrack
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT `+musicTrackColumns+`,content_hash FROM music.tracks WHERE path=$1`, path).Scan(&t.ID, &t.Path, &t.Name, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.Size, &t.ModifiedAt, &t.HasCover, &t.CoverStream, &t.Available, &hash)
	return t, hash, err
}

func (s *Store) UpsertMusicTrack(ctx context.Context, t MusicTrack, stamp int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var artistID, albumID any
	artistName := strings.TrimSpace(t.Artist)
	if artistName == "" {
		artistName = "Unknown artist"
	}
	var artistRowID int64
	if err = tx.QueryRowContext(ctx, `INSERT INTO music.artists(name) VALUES($1) ON CONFLICT(name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, artistName).Scan(&artistRowID); err != nil {
		return err
	}
	artistID = artistRowID
	if strings.TrimSpace(t.Album) != "" {
		var id int64
		if err = tx.QueryRowContext(ctx, `INSERT INTO music.albums(artist_id,name) VALUES($1,$2) ON CONFLICT(artist_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, artistID, t.Album).Scan(&id); err != nil {
			return err
		}
		albumID = id
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO music.tracks(id,path,name,title,artist,album,artist_id,album_id,duration,size,modified_at,content_hash,has_cover,cover_stream,available,seen_scan,indexed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,true,$15,$16) ON CONFLICT(id) DO UPDATE SET path=EXCLUDED.path,name=EXCLUDED.name,title=EXCLUDED.title,artist=EXCLUDED.artist,album=EXCLUDED.album,artist_id=EXCLUDED.artist_id,album_id=EXCLUDED.album_id,duration=EXCLUDED.duration,size=EXCLUDED.size,modified_at=EXCLUDED.modified_at,content_hash=EXCLUDED.content_hash,has_cover=EXCLUDED.has_cover,cover_stream=EXCLUDED.cover_stream,available=true,seen_scan=EXCLUDED.seen_scan,indexed_at=EXCLUDED.indexed_at`, t.ID, t.Path, t.Name, t.Title, t.Artist, t.Album, artistID, albumID, t.Duration, t.Size, t.ModifiedAt, t.ContentHash, t.HasCover, t.CoverStream, stamp, time.Now().Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MusicMatchHash(ctx context.Context, hash, root string, stamp int64) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,path FROM music.tracks WHERE content_hash=$1 AND seen_scan<>$3 AND left(path,length($2))=$2`, hash, musicPathPrefix(root), stamp)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var match string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return "", err
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if match != "" {
				return "", sql.ErrNoRows
			}
			match = id
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if match == "" {
		return "", sql.ErrNoRows
	}
	return match, nil
}

func (s *Store) MusicMarkSeen(ctx context.Context, id string, stamp int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music.tracks SET seen_scan=$2,available=true WHERE id=$1`, id, stamp)
	return err
}

func (s *Store) MusicMarkMissing(ctx context.Context, root string, stamp int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE music.tracks SET available=false WHERE seen_scan<>$1 AND left(path,length($2))=$2`, stamp, musicPathPrefix(root))
	return err
}

func (s *Store) MusicPlay(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO music.plays(track_id,played_at) SELECT id,$2 FROM music.tracks WHERE id=$1 AND available=true`, id, time.Now().Unix())
	return err
}
