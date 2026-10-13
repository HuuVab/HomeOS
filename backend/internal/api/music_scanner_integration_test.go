package api

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func TestMusicScannerIntegration(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	db, err := store.OpenPostgres(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	library, err := db.MusicLibrary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if library.Root != "" {
		t.Skip("music library is already configured; scanner test requires an empty test library")
	}
	root := t.TempDir()
	nested := filepath.Join(root, "album")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(nested, "song.mp3")
	if err := os.WriteFile(first, []byte("test audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMusicLibrary(ctx, root); err != nil {
		t.Fatal(err)
	}
	cleanup, err := sql.Open("pgx", connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Close()
	defer func() {
		_, _ = cleanup.Exec(`DELETE FROM music.player_state WHERE id=1 AND track_id IN (SELECT id FROM music.tracks WHERE left(path,length($1))=$1)`, root)
		_, _ = cleanup.Exec(`DELETE FROM music.library WHERE root_path=$1`, root)
		_, _ = cleanup.Exec(`DELETE FROM music.tracks WHERE left(path,length($1))=$1`, root)
	}()
	scanner := &MusicScanner{db: db}
	if err := scanner.scan(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	tracks, err := db.MusicTracks(ctx, root, "", "", "", "", "title", false, 500, 0)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("initial scan: %v tracks=%d", err, len(tracks))
	}
	id := tracks[0].ID
	playlistID, err := db.CreateMusicPlaylist(ctx, "Scanner test")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DeleteMusicPlaylist(ctx, playlistID)
	if err := db.SetMusicPlaylistTracks(ctx, playlistID, []string{id}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMusicFavorite(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := db.MusicPlay(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMusicQueue(ctx, []string{id}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMusicPlayer(ctx, store.MusicPlayerState{TrackID: id, Position: 12, Repeat: "all"}); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "renamed.mp3")
	if err := os.Rename(first, second); err != nil {
		t.Fatal(err)
	}
	if err := scanner.scan(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	tracks, err = db.MusicTracks(ctx, root, "", "", "", "", "title", false, 500, 0)
	if err != nil || len(tracks) != 1 || tracks[0].ID != id || tracks[0].Path != second {
		t.Fatalf("rename did not preserve ID: %+v %v", tracks, err)
	}
	if opened, err := approvedMusicTrack(httptest.NewRequest("GET", "/", nil), db, id); err != nil || opened.ID != id {
		t.Fatalf("ID-based playback lookup: %+v %v", opened, err)
	}
	personal, err := db.MusicPersonal(ctx)
	if err != nil || len(personal.Playlists) != 1 || personal.Playlists[0].TrackIDs[0] != id || len(personal.Favorites) != 1 || personal.Favorites[0] != id || len(personal.Recent) != 1 || personal.Recent[0].ID != id || personal.PlayCounts[id] != 1 || len(personal.Queue) != 1 || personal.Player.TrackID != id || personal.Player.Position != 12 {
		t.Fatalf("personal state did not survive rename: %+v %v", personal, err)
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	if err := scanner.scan(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	track, err := db.MusicTrackByID(ctx, id)
	if err != nil || track.Available {
		t.Fatalf("deleted track should be unavailable: %+v %v", track, err)
	}
	if _, err := approvedMusicTrack(httptest.NewRequest("GET", "/", nil), db, id); err == nil {
		t.Fatal("deleted track remained streamable")
	}
}
