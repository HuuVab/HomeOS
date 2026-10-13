package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func registerMusicPersonalRoutes(mux *http.ServeMux, db *store.Store, check func(http.ResponseWriter, *http.Request) bool) {
	read := func(w http.ResponseWriter, r *http.Request, out any) bool {
		if !check(w, r) {
			return false
		}
		if !db.IsPostgres() {
			writeError(w, 400, "postgres_required", "Persistent music requires PostgreSQL")
			return false
		}
		if r.Method == http.MethodGet {
			return true
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(out); err != nil {
			writeError(w, 400, "invalid_json", "Invalid request body")
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/music/personal", func(w http.ResponseWriter, r *http.Request) {
		if !read(w, r, nil) {
			return
		}
		v, err := db.MusicPersonal(r.Context())
		if err != nil {
			writeError(w, 500, "music_personal", err.Error())
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/music/favorites", func(w http.ResponseWriter, r *http.Request) {
		if !read(w, r, nil) {
			return
		}
		tracks, err := db.MusicCollectionTracks(r.Context(), "favorites", "")
		if err != nil {
			writeError(w, 500, "music_favorites", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"tracks": tracks})
	})
	mux.HandleFunc("GET /api/v1/music/playlists/{id}/tracks", func(w http.ResponseWriter, r *http.Request) {
		if !read(w, r, nil) {
			return
		}
		tracks, err := db.MusicCollectionTracks(r.Context(), "playlist", r.PathValue("id"))
		if err != nil {
			writeError(w, 500, "music_playlist", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"tracks": tracks})
	})
	mux.HandleFunc("POST /api/v1/music/play", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			ID string `json:"id"`
		}
		if !read(w, r, &v) {
			return
		}
		if _, err := approvedMusicTrack(r, db, v.ID); err != nil {
			writeError(w, 404, "track_not_found", "Track is unavailable")
			return
		}
		if err := db.MusicPlay(r.Context(), v.ID); err != nil {
			writeError(w, 500, "music_play", err.Error())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/music/favorite", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			ID    string `json:"id"`
			Liked bool   `json:"liked"`
		}
		if !read(w, r, &v) {
			return
		}
		if _, err := db.MusicTrackByID(r.Context(), v.ID); err != nil {
			writeError(w, 404, "track_not_found", "Track is unavailable")
			return
		}
		if err := db.SetMusicFavorite(r.Context(), v.ID, v.Liked); err != nil {
			writeError(w, 500, "music_favorite", err.Error())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/music/player", func(w http.ResponseWriter, r *http.Request) {
		var v store.MusicPlayerState
		if !read(w, r, &v) {
			return
		}
		if v.TrackID != "" {
			if _, err := db.MusicTrackByID(r.Context(), v.TrackID); err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
		}
		if err := db.SetMusicPlayer(r.Context(), v); err != nil {
			writeError(w, 500, "music_player", err.Error())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/music/queue", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			TrackIDs []string `json:"trackIds"`
		}
		if !read(w, r, &v) {
			return
		}
		if len(v.TrackIDs) > 1000 {
			writeError(w, 400, "queue_too_large", "Queue is too large")
			return
		}
		if err := db.SetMusicQueue(r.Context(), v.TrackIDs); err != nil {
			writeError(w, 400, "music_queue", err.Error())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/v1/music/playlists", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Name string `json:"name"`
		}
		if !read(w, r, &v) {
			return
		}
		v.Name = strings.TrimSpace(v.Name)
		if v.Name == "" || len(v.Name) > 100 {
			writeError(w, 400, "invalid_name", "Enter a playlist name")
			return
		}
		id, err := db.CreateMusicPlaylist(r.Context(), v.Name)
		if err != nil {
			writeError(w, 500, "music_playlist", err.Error())
			return
		}
		writeJSON(w, 201, map[string]string{"id": id})
	})
	mux.HandleFunc("PUT /api/v1/music/playlists/{id}/tracks", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			TrackIDs []string `json:"trackIds"`
		}
		if !read(w, r, &v) {
			return
		}
		if len(v.TrackIDs) > 5000 {
			writeError(w, 400, "playlist_too_large", "Playlist is too large")
			return
		}
		if err := db.SetMusicPlaylistTracks(r.Context(), r.PathValue("id"), v.TrackIDs); err != nil {
			writeError(w, 400, "music_playlist", err.Error())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /api/v1/music/playlists/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if !db.IsPostgres() {
			writeError(w, 400, "postgres_required", "Persistent music requires PostgreSQL")
			return
		}
		if err := db.DeleteMusicPlaylist(r.Context(), r.PathValue("id")); err != nil {
			writeError(w, 500, "music_playlist", err.Error())
			return
		}
		w.WriteHeader(204)
	})
}
