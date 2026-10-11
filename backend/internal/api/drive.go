package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func registerDriveRoutes(mux *http.ServeMux, db *store.Store, files *drive.Store) {
	check := func(w http.ResponseWriter, r *http.Request, mutate bool) bool {
		_, csrf, _, ok := authenticate(w, r, db)
		if !ok {
			return false
		}
		if mutate && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
			writeError(w, http.StatusForbidden, "invalid_csrf", "Invalid CSRF token")
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/drive", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		entries, err := files.List(r.URL.Query().Get("path"))
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
	mux.HandleFunc("POST /api/v1/drive/folders", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path string `json:"path"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected a folder path")
			return
		}
		if err := files.CreateFolder(payload.Path); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PUT /api/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		if err := files.Upload(r.URL.Query().Get("path"), r.Body); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("GET /api/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		file, err := files.Open(name)
		if err != nil {
			driveError(w, err)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			driveError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}

func driveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, drive.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "invalid_path", "Invalid drive path")
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "not_found", "File or folder not found")
	case errors.Is(err, os.ErrExist):
		writeError(w, http.StatusConflict, "already_exists", "File or folder already exists")
	case errors.Is(err, os.ErrPermission):
		writeError(w, http.StatusForbidden, "forbidden", "Storage access denied")
	default:
		writeError(w, http.StatusInternalServerError, "storage_error", "Storage operation failed")
	}
}
