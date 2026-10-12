package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func registerDriveRoutes(mux *http.ServeMux, db *store.Store, files *drive.Store, ownerID int64, metadataClient metadatapb.MetadataServiceClient) {
	fileEntry := func(r *http.Request, name string) (drive.Entry, error) {
		if metadataClient == nil {
			return db.FileEntry(ownerID, name)
		}
		item, err := metadataClient.GetFile(r.Context(), &metadatapb.GetFileRequest{OwnerId: ownerID, Path: name})
		if err != nil {
			return drive.Entry{}, rpcFileError(err)
		}
		return drive.Entry{Name: item.Name, Path: item.Path, Kind: item.Kind, Size: item.Size,
			ModifiedAt: time.Unix(item.ModifiedAtUnix, 0).UTC(), Version: item.Version}, nil
	}
	checkSource := func(w http.ResponseWriter, r *http.Request, name string) bool {
		if !db.AuthoritativeMetadata() {
			return true
		}
		entry, err := fileEntry(r, name)
		if err != nil {
			driveError(w, err)
			return false
		}
		version, parseErr := strconv.ParseInt(r.Header.Get("X-Source-Version"), 10, 64)
		if parseErr != nil || version < 1 {
			writeError(w, http.StatusPreconditionRequired, "version_required", "Refresh the file list and retry with X-Source-Version")
			return false
		}
		if version != entry.Version {
			writeError(w, http.StatusPreconditionFailed, "version_conflict", "The item changed; refresh the file list before retrying")
			return false
		}
		return true
	}
	checkTarget := func(w http.ResponseWriter, r *http.Request, name string) bool {
		if !db.AuthoritativeMetadata() {
			return true
		}
		if _, err := fileEntry(r, name); err == nil {
			driveError(w, os.ErrExist)
			return false
		} else if !errors.Is(err, os.ErrNotExist) {
			driveError(w, err)
			return false
		}
		parent := path.Dir(name)
		if parent != "." {
			entry, err := fileEntry(r, parent)
			if err != nil {
				driveError(w, err)
				return false
			}
			if entry.Kind != "folder" {
				driveError(w, drive.ErrInvalidPath)
				return false
			}
		}
		return true
	}
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
		var entries []drive.Entry
		var err error
		if db.AuthoritativeMetadata() {
			if metadataClient != nil {
				var result *metadatapb.ListFilesResponse
				result, err = metadataClient.ListFiles(r.Context(), &metadatapb.ListFilesRequest{OwnerId: ownerID, Folder: r.URL.Query().Get("path")})
				if err == nil {
					entries = make([]drive.Entry, 0, len(result.Entries))
					for _, item := range result.Entries {
						entries = append(entries, drive.Entry{Name: item.Name, Path: item.Path, Kind: item.Kind, Size: item.Size,
							ModifiedAt: time.Unix(item.ModifiedAtUnix, 0).UTC(), Version: item.Version})
					}
				} else {
					err = rpcFileError(err)
				}
			} else {
				entries, err = db.FileEntries(ownerID, r.URL.Query().Get("path"))
			}
		} else {
			entries, err = files.List(r.URL.Query().Get("path"))
		}
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
		usage, err := files.Usage()
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, usage)
	})
	mux.HandleFunc("GET /api/v1/drive/versions", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		entry, err := fileEntry(r, name)
		if err != nil || entry.Kind != "file" {
			driveError(w, os.ErrNotExist)
			return
		}
		versions, err := db.FileVersions(ownerID, name)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "File versions are unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
	})
	mux.HandleFunc("POST /api/v1/drive/versions/restore", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path    string `json:"path"`
			Version int64  `json:"version"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF || payload.Version < 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected a file path and version")
			return
		}
		if !checkSource(w, r, payload.Path) {
			return
		}
		version, err := db.FileVersion(ownerID, payload.Path, payload.Version)
		if err != nil {
			if err == sql.ErrNoRows {
				driveError(w, os.ErrNotExist)
			} else {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "File version is unavailable")
			}
			return
		}
		if err := files.RestoreVersion(payload.Path, version.Checksum, version.Size); err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"restored": true})
	})
	mux.HandleFunc("GET /api/v1/drive/changes", func(w http.ResponseWriter, r *http.Request) {
		user, _, _, ok := authenticate(w, r, db)
		if !ok {
			return
		}
		after := int64(0)
		if raw := r.URL.Query().Get("after"); raw != "" {
			var err error
			after, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || after < 0 {
				writeError(w, http.StatusBadRequest, "invalid_cursor", "Expected a non-negative change cursor")
				return
			}
		}
		events, err := db.ChangesAfter(user.ID, after, 100)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive changes are unavailable")
			return
		}
		if len(events) > 0 {
			after = events[len(events)-1].ID
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "nextCursor": after})
	})
	mux.HandleFunc("GET /api/v1/drive/search", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		var entries []drive.Entry
		var err error
		if db.AuthoritativeMetadata() {
			entries, err = db.SearchFileEntries(ownerID, r.URL.Query().Get("q"))
		} else {
			entries, err = files.Search(r.URL.Query().Get("q"))
		}
		if err != nil {
			driveError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
	mux.HandleFunc("GET /api/v1/drive/preview", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		if db.AuthoritativeMetadata() {
			entry, err := fileEntry(r, name)
			if err != nil {
				driveError(w, err)
				return
			}
			if entry.Kind != "file" {
				driveError(w, drive.ErrInvalidPath)
				return
			}
		}
		file, contentType, err := files.OpenPreview(name)
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
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(name)}))
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
	mux.HandleFunc("POST /api/v1/drive/move", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected source and destination paths")
			return
		}
		if !checkSource(w, r, payload.From) || !checkTarget(w, r, payload.To) {
			return
		}
		if err := files.Move(payload.From, payload.To); err != nil {
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
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected source and destination paths")
			return
		}
		if !checkSource(w, r, payload.From) || !checkTarget(w, r, payload.To) {
			return
		}
		if err := files.Copy(payload.From, payload.To); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("GET /api/v1/drive/folders/zip", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		folder := r.URL.Query().Get("path")
		if db.AuthoritativeMetadata() {
			entry, err := fileEntry(r, folder)
			if err != nil {
				driveError(w, err)
				return
			}
			if entry.Kind != "folder" {
				driveError(w, drive.ErrInvalidPath)
				return
			}
		}
		if err := files.ValidateFolder(folder); err != nil {
			driveError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(folder) + ".zip"}))
		if err := files.WriteZip(folder, w); err != nil {
			driveError(w, err)
		}
	})
	mux.HandleFunc("POST /api/v1/drive/trash", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		var payload struct {
			Path string `json:"path"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected path")
			return
		}
		if !checkSource(w, r, payload.Path) {
			return
		}
		if err := files.Trash(payload.Path); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/drive/trash", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		var entries []drive.TrashEntry
		var err error
		if db.AuthoritativeMetadata() {
			entries, err = db.TrashEntries(ownerID)
		} else {
			entries, err = files.ListTrash()
		}
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
		if db.AuthoritativeMetadata() {
			if _, err := db.TrashEntry(ownerID, r.PathValue("id")); err != nil {
				driveError(w, err)
				return
			}
		}
		if err := files.Restore(r.PathValue("id")); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/drive/trash/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		if db.AuthoritativeMetadata() {
			if _, err := db.TrashEntry(ownerID, r.PathValue("id")); err != nil {
				driveError(w, err)
				return
			}
		}
		if err := files.DeleteForever(r.PathValue("id")); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		if metadataClient != nil && db.AuthoritativeMetadata() {
			if _, err := metadataClient.CreateFolder(r.Context(), &metadatapb.CreateFolderRequest{OwnerId: ownerID, Path: payload.Path}); err != nil {
				driveError(w, rpcFileError(err))
				return
			}
			w.WriteHeader(http.StatusCreated)
			return
		}
		if !checkTarget(w, r, payload.Path) {
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
		name := r.URL.Query().Get("path")
		replacing := r.Header.Get("X-Source-Version") != ""
		if replacing {
			if !checkSource(w, r, name) {
				return
			}
			entry, err := fileEntry(r, name)
			if err != nil || entry.Kind != "file" {
				driveError(w, drive.ErrInvalidPath)
				return
			}
		} else if !checkTarget(w, r, name) {
			return
		}
		var err error
		if replacing {
			err = files.Replace(name, r.Body)
		} else {
			err = files.Upload(name, r.Body)
		}
		if err != nil {
			driveError(w, err)
			return
		}
		if replacing {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
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
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected upload path, size, and fingerprint")
			return
		}
		if !checkTarget(w, r, payload.Path) {
			return
		}
		session, err := files.StartUpload(payload.Path, payload.Size, payload.Fingerprint)
		if err != nil {
			driveError(w, err)
			return
		}
		if db.AuthoritativeMetadata() {
			if err := db.UpsertUploadSession(ownerID, session); err != nil {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "Upload session metadata is unavailable; retry to resume")
				return
			}
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
		if db.AuthoritativeMetadata() {
			if _, err := db.PendingUploadSession(ownerID, r.PathValue("id")); err != nil {
				driveError(w, err)
				return
			}
		}
		newOffset, err := files.AppendChunkVerified(r.PathValue("id"), offset, r.Body, r.Header.Get("X-Chunk-SHA256"))
		if err != nil {
			driveError(w, err)
			return
		}
		if db.AuthoritativeMetadata() {
			if err := db.SetUploadOffset(ownerID, r.PathValue("id"), newOffset); err != nil {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "Upload offset metadata is unavailable; retry to resume")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]int64{"offset": newOffset})
	})
	mux.HandleFunc("POST /api/v1/drive/uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, true) {
			return
		}
		if db.AuthoritativeMetadata() {
			if _, err := db.PendingUploadSession(ownerID, r.PathValue("id")); err != nil {
				driveError(w, err)
				return
			}
		}
		if err := files.FinishUpload(r.PathValue("id")); err != nil {
			driveError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r, false) {
			return
		}
		name := r.URL.Query().Get("path")
		if db.AuthoritativeMetadata() {
			entry, err := fileEntry(r, name)
			if err != nil {
				driveError(w, err)
				return
			}
			if entry.Kind != "file" {
				driveError(w, drive.ErrInvalidPath)
				return
			}
		}
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

func rpcFileError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return os.ErrNotExist
	case codes.AlreadyExists:
		return os.ErrExist
	case codes.InvalidArgument:
		return drive.ErrInvalidPath
	default:
		return err
	}
}

func driveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, drive.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "invalid_path", "Invalid drive path")
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "not_found", "File or folder not found")
	case errors.Is(err, os.ErrExist):
		writeError(w, http.StatusConflict, "already_exists", "File or folder already exists")
	case errors.Is(err, store.ErrCatalogRevision):
		writeError(w, http.StatusPreconditionFailed, "version_conflict", "The item changed; refresh the file list before retrying")
	case errors.Is(err, drive.ErrOffset):
		writeError(w, http.StatusConflict, "offset_mismatch", "Upload offset does not match the stored file")
	case errors.Is(err, drive.ErrChecksum):
		writeError(w, http.StatusBadRequest, "checksum_mismatch", "Upload chunk SHA-256 does not match")
	case errors.Is(err, drive.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "chunk_too_large", "Upload chunk exceeds the allowed size")
	case errors.Is(err, drive.ErrQuotaExceeded):
		writeError(w, http.StatusInsufficientStorage, "quota_exceeded", "Storage quota exceeded")
	case status.Code(err) == codes.Aborted:
		writeError(w, http.StatusConflict, "metadata_drift", "HomeDrive files differ from committed metadata")
	case status.Code(err) == codes.Unavailable:
		writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata is unavailable")
	case errors.Is(err, os.ErrPermission):
		writeError(w, http.StatusForbidden, "forbidden", "Storage access denied")
	default:
		writeError(w, http.StatusInternalServerError, "storage_error", "Storage operation failed")
	}
}
