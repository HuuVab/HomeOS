package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

var musicTypes = map[string]string{
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac",
	".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav",
	".flac": "audio/flac", ".webm": "audio/webm",
}

func registerMusicRoutes(mux *http.ServeMux, db *store.Store) {
	check := func(w http.ResponseWriter, r *http.Request) bool {
		user, csrf, _, ok := authenticate(w, r, db)
		if !ok {
			return false
		}
		if user.Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "HomeMusic server folders require an admin account")
			return false
		}
		if r.Method != http.MethodGet && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
			writeError(w, http.StatusForbidden, "invalid_csrf", "Invalid CSRF token")
			return false
		}
		return true
	}
	validFolder := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		folder := strings.TrimSpace(r.URL.Query().Get("path"))
		if folder == "" || !filepath.IsAbs(folder) {
			writeError(w, http.StatusBadRequest, "invalid_path", "Enter an absolute server folder path")
			return "", false
		}
		folder = filepath.Clean(folder)
		if !musicRootAllowed(folder) {
			writeError(w, http.StatusForbidden, "folder_not_approved", "Music folder is outside approved roots")
			return "", false
		}
		if db.IsPostgres() {
			library, err := db.MusicLibrary(r.Context())
			if err != nil || library.Root == "" || !strings.EqualFold(folder, library.Root) {
				writeError(w, http.StatusForbidden, "folder_not_approved", "Use the configured HomeMusic library")
				return "", false
			}
		}
		info, err := os.Stat(folder)
		if err != nil || !info.IsDir() {
			writeError(w, http.StatusNotFound, "folder_not_found", "Music folder is unavailable")
			return "", false
		}
		return folder, true
	}
	mux.HandleFunc("GET /api/v1/music/library", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		library, err := db.MusicLibrary(r.Context())
		if err != nil {
			writeError(w, 500, "music_library", err.Error())
			return
		}
		writeJSON(w, 200, library)
	})
	mux.HandleFunc("GET /api/v1/music/tracks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if !db.IsPostgres() {
			writeError(w, 400, "postgres_required", "Persistent music requires PostgreSQL")
			return
		}
		track, err := approvedMusicTrack(r, db, r.PathValue("id"))
		if err != nil {
			writeError(w, 404, "track_not_found", "Track is unavailable")
			return
		}
		writeJSON(w, 200, track)
	})
	mux.HandleFunc("DELETE /api/v1/music/tracks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if !db.IsPostgres() {
			writeError(w, 400, "postgres_required", "Persistent music requires PostgreSQL")
			return
		}
		track, err := approvedMusicTrack(r, db, r.PathValue("id"))
		if err != nil {
			writeError(w, 404, "track_not_found", "Track is unavailable")
			return
		}
		library, err := db.MusicLibrary(r.Context())
		if err != nil {
			writeError(w, 500, "music_library", "Music library is unavailable")
			return
		}
		if err := removeMusicTrackFile(library.Root, track.Path); err != nil {
			writeError(w, 500, "music_delete", "Could not delete the audio file from disk: "+err.Error())
			return
		}
		if err := db.MusicMarkDeleted(r.Context(), track.ID); err != nil {
			wakeMusicScanner()
			writeError(w, 500, "music_delete", "File deleted, but the library could not be updated: "+err.Error())
			return
		}
		wakeMusicScanner()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/music/library", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if !db.IsPostgres() {
			writeError(w, 400, "postgres_required", "Persistent music requires PostgreSQL")
			return
		}
		var body struct {
			Root string `json:"root"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || !filepath.IsAbs(body.Root) {
			writeError(w, 400, "invalid_path", "Enter an absolute server folder path")
			return
		}
		root := filepath.Clean(body.Root)
		if !musicRootAllowed(root) {
			writeError(w, 403, "folder_not_approved", "Music folder is outside approved roots")
			return
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			writeError(w, 404, "folder_not_found", "Music folder is unavailable")
			return
		}
		if err := db.SetMusicLibrary(r.Context(), root); err != nil {
			writeError(w, 500, "music_library", err.Error())
			return
		}
		wakeMusicScanner()
		writeJSON(w, 200, map[string]string{"root": root})
	})
	mux.HandleFunc("GET /api/v1/music/folders", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		folder := strings.TrimSpace(r.URL.Query().Get("path"))
		if folder == "" {
			roots := make([]map[string]string, 0)
			if allowed := os.Getenv("HOMEOS_MUSIC_ALLOWED_ROOTS"); allowed != "" {
				for _, root := range filepath.SplitList(allowed) {
					if root != "" {
						root = filepath.Clean(root)
						if info, err := os.Stat(root); err == nil && info.IsDir() {
							roots = append(roots, map[string]string{"name": root, "path": root})
						}
					}
				}
			} else if runtime.GOOS == "windows" {
				for drive := 'A'; drive <= 'Z'; drive++ {
					root := string(drive) + `:\`
					if info, err := os.Stat(root); err == nil && info.IsDir() {
						roots = append(roots, map[string]string{"name": root, "path": root})
					}
				}
			} else {
				roots = append(roots, map[string]string{"name": "/", "path": "/"})
			}
			writeJSON(w, http.StatusOK, map[string]any{"path": "", "parent": "", "folders": roots})
			return
		}
		if !filepath.IsAbs(folder) {
			writeError(w, http.StatusBadRequest, "invalid_path", "Enter an absolute server folder path")
			return
		}
		folder = filepath.Clean(folder)
		if !musicRootAllowed(folder) {
			writeError(w, 403, "folder_not_approved", "Music folder is outside approved roots")
			return
		}
		items, err := os.ReadDir(folder)
		if err != nil {
			writeError(w, http.StatusNotFound, "folder_unavailable", "Cannot browse this folder")
			return
		}
		folders := make([]map[string]string, 0)
		for _, item := range items {
			if item.IsDir() && item.Type()&os.ModeSymlink == 0 {
				folders = append(folders, map[string]string{"name": item.Name(), "path": filepath.Join(folder, item.Name())})
			}
		}
		parent := filepath.Dir(folder)
		if parent == folder || !musicRootAllowed(parent) {
			parent = ""
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": folder, "parent": parent, "folders": folders})
	})
	mux.HandleFunc("GET /api/v1/music", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if db.IsPostgres() && r.URL.Query().Get("path") == "" {
			library, err := db.MusicLibrary(r.Context())
			if err != nil {
				writeError(w, 500, "music_library", err.Error())
				return
			}
			if library.Root != "" && !musicRootAllowed(library.Root) {
				writeError(w, 403, "folder_not_approved", "Music folder is outside approved roots")
				return
			}
			limit := 500
			offset := 0
			if v := r.URL.Query().Get("limit"); v != "" {
				if n, e := strconv.Atoi(v); e == nil {
					limit = n
				}
			}
			if v := r.URL.Query().Get("offset"); v != "" {
				if n, e := strconv.Atoi(v); e == nil {
					offset = n
				}
			}
			tracks, err := db.MusicTracks(r.Context(), library.Root, r.URL.Query().Get("q"), r.URL.Query().Get("artist"), r.URL.Query().Get("album"), r.URL.Query().Get("folder"), r.URL.Query().Get("sort"), r.URL.Query().Get("order") == "desc", limit, offset)
			if err != nil {
				writeError(w, 500, "music_tracks", err.Error())
				return
			}
			writeJSON(w, 200, map[string]any{"tracks": tracks, "folder": library.Root, "scanCompletedAt": library.ScanCompletedAt, "scanError": library.ScanError})
			return
		}
		folder, ok := validFolder(w, r)
		if !ok {
			return
		}
		items, err := os.ReadDir(folder)
		if err != nil {
			writeError(w, http.StatusForbidden, "folder_unavailable", "Cannot read music folder")
			return
		}
		tracks := make([]musicTrack, 0)
		modified := make([]int64, 0)
		for _, item := range items {
			if item.IsDir() || item.Type()&os.ModeSymlink != 0 || musicTypes[strings.ToLower(filepath.Ext(item.Name()))] == "" {
				continue
			}
			info, err := item.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			tracks = append(tracks, musicTrack{Name: item.Name(), Size: info.Size()})
			modified = append(modified, info.ModTime().UnixNano())
		}
		probe, _ := exec.LookPath("ffprobe")
		var workers sync.WaitGroup
		jobs := make(chan int)
		for range 4 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for index := range jobs {
					track := tracks[index]
					tracks[index] = readMusicMetadata(r.Context(), probe, filepath.Join(folder, track.Name), track.Name, track.Size, time.Unix(0, modified[index]))
				}
			}()
		}
		for index := range tracks {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks})
	})
	mux.HandleFunc("GET /api/v1/music/audio", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if id := r.URL.Query().Get("id"); db.IsPostgres() && id != "" {
			track, err := approvedMusicTrack(r, db, id)
			if err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			library, err := db.MusicLibrary(r.Context())
			if err != nil {
				writeError(w, 500, "music_library", "Music library is unavailable")
				return
			}
			root, err := os.OpenRoot(library.Root)
			if err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			defer root.Close()
			resolvedRoot, err := filepath.EvalSymlinks(library.Root)
			if err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			rel, err := filepath.Rel(resolvedRoot, track.Path)
			if err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			file, err := root.Open(rel)
			if err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			w.Header().Set("Content-Type", musicTypes[strings.ToLower(filepath.Ext(track.Path))])
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeContent(w, r, track.Name, info.ModTime(), file)
			return
		}
		folder, ok := validFolder(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		kind := musicTypes[strings.ToLower(filepath.Ext(name))]
		if name == "" || filepath.Base(name) != name || kind == "" {
			writeError(w, http.StatusBadRequest, "invalid_track", "Invalid track")
			return
		}
		root, err := os.OpenRoot(folder)
		if err != nil {
			writeError(w, http.StatusNotFound, "folder_not_found", "Music folder is unavailable")
			return
		}
		defer root.Close()
		file, err := root.Open(name)
		if err != nil {
			writeError(w, http.StatusNotFound, "track_not_found", "Track is unavailable")
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "track_not_found", "Track is unavailable")
			return
		}
		w.Header().Set("Content-Type", kind)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, info.ModTime(), file)
	})
	mux.HandleFunc("GET /api/v1/music/cover", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if id := r.URL.Query().Get("id"); db.IsPostgres() && id != "" {
			track, err := approvedMusicTrack(r, db, id)
			if err != nil {
				writeError(w, 404, "track_not_found", "Track is unavailable")
				return
			}
			serveMusicCover(w, r, track)
			return
		}
		folder, ok := validFolder(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" || filepath.Base(name) != name || musicTypes[strings.ToLower(filepath.Ext(name))] == "" {
			writeError(w, http.StatusBadRequest, "invalid_track", "Invalid track")
			return
		}
		path := filepath.Join(folder, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "track_not_found", "Track is unavailable")
			return
		}
		probe, probeErr := exec.LookPath("ffprobe")
		ffmpeg, ffmpegErr := exec.LookPath("ffmpeg")
		if probeErr != nil || ffmpegErr != nil {
			writeError(w, http.StatusNotFound, "cover_not_found", "Cover art is unavailable")
			return
		}
		track := readMusicMetadata(r.Context(), probe, path, name, info.Size(), info.ModTime())
		if !track.HasCover {
			writeError(w, http.StatusNotFound, "cover_not_found", "Track has no embedded cover")
			return
		}
		image, err := readMusicCover(r.Context(), ffmpeg, path, track.CoverStream, info.Size(), info.ModTime())
		if err != nil {
			writeError(w, http.StatusNotFound, "cover_not_found", "Cover art is unavailable")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(image)
	})
	registerMusicPersonalRoutes(mux, db, check)
}
