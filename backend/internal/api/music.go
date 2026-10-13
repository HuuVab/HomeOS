package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		user, _, _, ok := authenticate(w, r, db)
		if !ok {
			return false
		}
		if user.Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "HomeMusic server folders require an admin account")
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
		info, err := os.Stat(folder)
		if err != nil || !info.IsDir() {
			writeError(w, http.StatusNotFound, "folder_not_found", "Music folder is unavailable")
			return "", false
		}
		return folder, true
	}
	mux.HandleFunc("GET /api/v1/music/folders", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		folder := strings.TrimSpace(r.URL.Query().Get("path"))
		if folder == "" {
			roots := make([]map[string]string, 0)
			if runtime.GOOS == "windows" {
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
		if parent == folder {
			parent = ""
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": folder, "parent": parent, "folders": folders})
	})
	mux.HandleFunc("GET /api/v1/music", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
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
		file, err := os.Open(filepath.Join(folder, name))
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
}
