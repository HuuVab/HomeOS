package api

import (
	"archive/zip"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func registerCatalogDriveRoutes(mux *http.ServeMux, db *store.Store, files *drive.Store, ownerID int64, unlimited bool) {
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
	jsonBody := func(w http.ResponseWriter, r *http.Request, target any) bool {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(target) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request")
			return false
		}
		return true
	}
	source := func(w http.ResponseWriter, r *http.Request, name string) (drive.Entry, bool) {
		entry, err := db.FileEntry(ownerID, name)
		if err != nil {
			driveError(w, err)
			return drive.Entry{}, false
		}
		version, err := strconv.ParseInt(r.Header.Get("X-Source-Version"), 10, 64)
		if err != nil || version < 1 {
			writeError(w, http.StatusPreconditionRequired, "version_required", "Refresh the file list and retry with X-Source-Version")
			return drive.Entry{}, false
		}
		if entry.Version != version {
			writeError(w, http.StatusPreconditionFailed, "version_conflict", "The item changed; refresh the file list before retrying")
			return drive.Entry{}, false
		}
		return entry, true
	}
	usage := func() (drive.StorageUsage, error) {
		maxFile, quota := files.Limits()
		value, err := db.CatalogUsage(ownerID, maxFile, quota)
		value.Unlimited = unlimited
		return value, err
	}
	mux.HandleFunc("GET /api/v1/drive", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		entries, err := db.FileEntries(ownerID, r.URL.Query().Get("path"))
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
	mux.HandleFunc("GET /api/v1/drive/storage", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		value, err := usage()
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	})
	mux.HandleFunc("GET /api/v1/drive/search", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		entries, err := db.SearchFileEntries(ownerID, r.URL.Query().Get("q"))
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
	mux.HandleFunc("GET /api/v1/drive/changes", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		after := int64(0)
		if value := r.URL.Query().Get("after"); value != "" {
			var err error
			after, err = strconv.ParseInt(value, 10, 64)
			if err != nil || after < 0 {
				writeError(w, http.StatusBadRequest, "invalid_cursor", "Expected a non-negative change cursor")
				return
			}
		}
		events, err := db.ChangesAfter(ownerID, after, 100)
		if err != nil {
			driveError(w, err)
			return
		}
		if len(events) > 0 {
			after = events[len(events)-1].ID
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "nextCursor": after})
	})
	mux.HandleFunc("GET /api/v1/drive/versions", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		entry, err := db.FileEntry(ownerID, name)
		if err != nil || entry.Kind != "file" {
			driveError(w, os.ErrNotExist)
			return
		}
		versions, err := db.FileVersions(ownerID, name)
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
	})
	mux.HandleFunc("GET /api/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		entry, err := db.FileEntry(ownerID, name)
		if err != nil {
			driveError(w, err)
			return
		}
		if entry.Kind != "file" {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		file, err := files.OpenBlob(entry.Checksum, entry.Size)
		if err != nil {
			driveError(w, err)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
		http.ServeContent(w, r, path.Base(name), entry.ModifiedAt, file)
	})
	mux.HandleFunc("GET /api/v1/drive/preview", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		entry, err := db.FileEntry(ownerID, name)
		if err != nil {
			driveError(w, err)
			return
		}
		if entry.Kind != "file" {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		contentType := catalogPreviewType(name)
		if contentType == "" || strings.HasPrefix(contentType, "text/") && entry.Size > 1<<20 {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		file, err := files.OpenBlob(entry.Checksum, entry.Size)
		if err != nil {
			driveError(w, err)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(name)}))
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		http.ServeContent(w, r, path.Base(name), entry.ModifiedAt, file)
	})
	mux.HandleFunc("POST /api/v1/drive/folders", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path string `json:"path"`
		}
		if !jsonBody(w, r, &payload) {
			return
		}
		if err := db.CatalogCreateFolder(ownerID, payload.Path); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PUT /api/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		name := r.URL.Query().Get("path")
		if !drive.ValidCatalogPath(name) {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		expected := int64(0)
		if r.Header.Get("X-Source-Version") != "" {
			entry, ok := source(w, r, name)
			if !ok {
				return
			}
			if entry.Kind != "file" {
				driveError(w, drive.ErrInvalidPath)
				return
			}
			expected = entry.Version
		}
		value, err := usage()
		if err != nil {
			driveError(w, err)
			return
		}
		available := value.QuotaBytes - value.UsedBytes - value.ReservedBytes
		if available < 0 {
			driveError(w, drive.ErrQuotaExceeded)
			return
		}
		limit := value.MaxFileBytes
		if available < limit {
			limit = available
		}
		checksum, size, err := files.PutBlob(r.Body, limit)
		if err != nil {
			driveError(w, err)
			return
		}
		_, err = db.CatalogCommitFile(ownerID, name, checksum, size, expected)
		if err != nil {
			driveError(w, err)
			return
		}
		if expected == 0 {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("POST /api/v1/drive/versions/restore", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path    string `json:"path"`
			Version int64  `json:"version"`
		}
		if !jsonBody(w, r, &payload) || payload.Version < 1 {
			return
		}
		if _, ok := source(w, r, payload.Path); !ok {
			return
		}
		version, err := db.FileVersion(ownerID, payload.Path, payload.Version)
		if errors.Is(err, sql.ErrNoRows) {
			driveError(w, os.ErrNotExist)
			return
		}
		if err != nil {
			driveError(w, err)
			return
		}
		file, err := files.OpenBlob(version.Checksum, version.Size)
		if err != nil {
			driveError(w, err)
			return
		}
		file.Close()
		expected, _ := strconv.ParseInt(r.Header.Get("X-Source-Version"), 10, 64)
		if err := db.CatalogRestoreVersion(ownerID, payload.Path, payload.Version, expected); err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"restored": true})
	})
	mux.HandleFunc("POST /api/v1/drive/move", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if !jsonBody(w, r, &payload) {
			return
		}
		entry, ok := source(w, r, payload.From)
		if !ok {
			return
		}
		if err := db.CatalogMoveOrCopy(ownerID, payload.From, payload.To, false, entry.Version); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/drive/copy", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if !jsonBody(w, r, &payload) {
			return
		}
		entry, ok := source(w, r, payload.From)
		if !ok {
			return
		}
		if err := db.CatalogMoveOrCopy(ownerID, payload.From, payload.To, true, entry.Version); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("POST /api/v1/drive/trash", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path string `json:"path"`
		}
		if !jsonBody(w, r, &payload) {
			return
		}
		entry, ok := source(w, r, payload.Path)
		if !ok {
			return
		}
		if err := db.CatalogTrash(ownerID, payload.Path, entry.Version); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/drive/trash", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		entries, err := db.TrashEntries(ownerID)
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
	mux.HandleFunc("POST /api/v1/drive/trash/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		entries, err := db.CatalogTrashFiles(ownerID, r.PathValue("id"))
		if err != nil {
			driveError(w, err)
			return
		}
		for _, entry := range entries {
			if entry.Kind != "file" {
				continue
			}
			file, err := files.OpenBlob(entry.Checksum, entry.Size)
			if err != nil {
				driveError(w, err)
				return
			}
			file.Close()
		}
		if err := db.CatalogRestoreTrash(ownerID, r.PathValue("id")); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/drive/trash/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		if err := db.CatalogDeleteForever(ownerID, r.PathValue("id")); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/drive/folders/zip", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		folder := r.URL.Query().Get("path")
		entries, err := db.CatalogSubtree(ownerID, folder)
		if err != nil {
			driveError(w, err)
			return
		}
		if entries[0].Kind != "folder" {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		for _, entry := range entries {
			if entry.Kind != "file" {
				continue
			}
			file, err := files.OpenBlob(entry.Checksum, entry.Size)
			if err != nil {
				driveError(w, err)
				return
			}
			file.Close()
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(folder) + ".zip"}))
		archive := zip.NewWriter(w)
		for _, entry := range entries {
			name := path.Base(folder) + strings.TrimPrefix(entry.Path, folder)
			if entry.Kind == "folder" {
				_, err = archive.Create(name + "/")
			} else {
				var writer io.Writer
				writer, err = archive.Create(name)
				if err == nil {
					var file *os.File
					file, err = files.OpenBlob(entry.Checksum, entry.Size)
					if err == nil {
						_, err = io.Copy(writer, file)
						file.Close()
					}
				}
			}
			if err != nil {
				break
			}
		}
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return
		}
	})
	mux.HandleFunc("POST /api/v1/drive/uploads", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path        string `json:"path"`
			Size        int64  `json:"size"`
			Fingerprint string `json:"fingerprint"`
		}
		if !jsonBody(w, r, &payload) {
			return
		}
		if !drive.ValidCatalogPath(payload.Path) || payload.Size < 0 || len(payload.Fingerprint) != 64 {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		if _, err := hex.DecodeString(payload.Fingerprint); err != nil {
			driveError(w, drive.ErrInvalidPath)
			return
		}
		if _, err := db.FileEntry(ownerID, payload.Path); err == nil {
			driveError(w, os.ErrExist)
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			driveError(w, err)
			return
		}
		if parent := path.Dir(payload.Path); parent != "." {
			entry, err := db.FileEntry(ownerID, parent)
			if err != nil {
				driveError(w, err)
				return
			}
			if entry.Kind != "folder" {
				driveError(w, drive.ErrInvalidPath)
				return
			}
		}
		value, err := usage()
		if err != nil {
			driveError(w, err)
			return
		}
		if payload.Size > value.MaxFileBytes {
			driveError(w, drive.ErrTooLarge)
			return
		}
		if payload.Size > value.QuotaBytes-value.UsedBytes-value.ReservedBytes {
			driveError(w, drive.ErrQuotaExceeded)
			return
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			driveError(w, err)
			return
		}
		session := drive.UploadSession{ID: hex.EncodeToString(token[:]), Path: payload.Path, Size: payload.Size, Fingerprint: payload.Fingerprint}
		if err := files.CreateCatalogStage(session.ID); err != nil {
			driveError(w, err)
			return
		}
		if err := db.UpsertUploadSession(ownerID, session); err != nil {
			files.RemoveCatalogStage(session.ID)
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("PATCH /api/v1/drive/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		offset, err := strconv.ParseInt(r.Header.Get("X-Upload-Offset"), 10, 64)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "invalid_offset", "Expected a non-negative upload offset")
			return
		}
		session, err := db.PendingUploadSession(ownerID, r.PathValue("id"))
		if err != nil {
			driveError(w, err)
			return
		}
		if offset != session.Offset {
			driveError(w, drive.ErrOffset)
			return
		}
		next, err := files.AppendCatalogStage(session.ID, offset, session.Size, r.Body, r.Header.Get("X-Chunk-SHA256"))
		if err != nil {
			driveError(w, err)
			return
		}
		if err := db.SetUploadOffset(ownerID, session.ID, next); err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"offset": next})
	})
	mux.HandleFunc("POST /api/v1/drive/uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		session, err := db.PendingUploadSession(ownerID, r.PathValue("id"))
		if err != nil {
			driveError(w, err)
			return
		}
		if session.Offset != session.Size {
			driveError(w, drive.ErrOffset)
			return
		}
		checksum, err := files.FinishCatalogStage(session.ID, session.Size, session.Fingerprint)
		if err != nil {
			driveError(w, err)
			return
		}
		if _, err := db.CatalogCommitFile(ownerID, session.Path, checksum, session.Size, 0, session.ID); err != nil {
			driveError(w, err)
			return
		}
		if err := files.RemoveCatalogStage(session.ID); err != nil { /* Recovery will sweep the orphan stage. */
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func catalogPreviewType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".md", ".csv", ".json", ".log", ".go", ".js", ".ts", ".tsx", ".py", ".css":
		return "text/plain; charset=utf-8"
	default:
		return ""
	}
}
