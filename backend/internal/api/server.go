package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/monitor"
	"github.com/HuuVab/HomeOS/backend/internal/redisaccel"
	"github.com/HuuVab/HomeOS/backend/internal/store"
)

var startedAt = time.Now()

type metric = monitor.Metric

type statusResponse struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	SampledAt time.Time `json:"sampledAt"`
	Metrics   []metric  `json:"metrics"`
}

func NewHandler(webDir string, db *store.Store, collector monitor.Collector, files *drive.Store, metadataClients ...metadatapb.MetadataServiceClient) http.Handler {
	return newHandler(webDir, db, collector, files, nil, metadataClients...)
}

func NewHandlerWithRedis(webDir string, db *store.Store, collector monitor.Collector, files *drive.Store, accelerator *redisaccel.Layer, metadataClient metadatapb.MetadataServiceClient) http.Handler {
	return newHandler(webDir, db, collector, files, accelerator, metadataClient)
}

func newHandler(webDir string, db *store.Store, collector monitor.Collector, files *drive.Store, accelerator *redisaccel.Layer, metadataClients ...metadatapb.MetadataServiceClient) http.Handler {
	var metadataClient metadatapb.MetadataServiceClient
	if len(metadataClients) > 0 {
		metadataClient = metadataClients[0]
	}
	mux := http.NewServeMux()
	registerAuthRoutes(mux, db, accelerator)
	driveHandler := func(w http.ResponseWriter, r *http.Request) {
		user, _, _, ok := authenticate(w, r, db)
		if !ok {
			return
		}
		userFiles, err := files.ForUser(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", "Storage unavailable")
			return
		}
		if db.AuthoritativeMetadata() {
			format, err := db.StorageFormat(user.ID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive storage format is unavailable")
				return
			}
			if format == "blobs" {
				catalogMux := http.NewServeMux()
				registerCatalogDriveRoutes(catalogMux, db, userFiles, user.ID)
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					db.LockDriveMutations()
					defer db.UnlockDriveMutations()
				} else {
					db.LockDriveReads()
					defer db.UnlockDriveReads()
				}
				catalogMux.ServeHTTP(w, r)
				return
			}
			if format != "legacy" {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive storage format is invalid")
				return
			}
		}
		reconcile := func() error {
			entries, err := userFiles.AllEntries()
			if err != nil {
				return err
			}
			if db.AuthoritativeMetadata() {
				trash, err := userFiles.ListTrash()
				if err != nil {
					return err
				}
				if err := db.ReconcileDriveMutation(user.ID, entries, trash, userFiles.PendingUploads()); err != nil {
					return err
				}
				if accelerator != nil {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					if err := accelerator.InvalidateOwner(ctx, user.ID); err != nil {
						log.Printf("HomeDrive Redis listing invalidation failed: %v", err)
					}
				}
				return nil
			}
			return db.ReconcileFileMutation(user.ID, entries)
		}
		if db.AuthoritativeMetadata() {
			pending, err := db.HasPendingFileMutation(user.ID)
			if err == nil && pending {
				db.LockDriveMutations()
				pending, err = db.HasPendingFileMutation(user.ID)
				if err == nil && pending {
					err = reconcile()
				}
				db.UnlockDriveMutations()
			}
			if err != nil {
				log.Printf("HomeDrive pending metadata recovery for user %d: %v", user.ID, err)
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata recovery is pending")
				return
			}
		}
		driveMux := http.NewServeMux()
		registerDriveRoutes(driveMux, db, userFiles, user.ID, metadataClient)
		metadataChanged := r.Method == http.MethodPut || r.Method == http.MethodDelete ||
			(r.Method == http.MethodPost && r.URL.Path != "/api/v1/drive/uploads")
		if metadataClient != nil && db.AuthoritativeMetadata() && r.Method == http.MethodPost && r.URL.Path == "/api/v1/drive/folders" {
			metadataChanged = false // The internal RPC owns the mutation marker and reconciliation.
		}
		if !metadataChanged {
			driveMux.ServeHTTP(w, r)
			return
		}
		db.LockDriveMutations()
		defer db.UnlockDriveMutations()
		if db.AuthoritativeMetadata() {
			current, err := userFiles.AllEntries()
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "HomeDrive storage is unavailable")
				return
			}
			matches, err := db.MetadataMatchesSnapshot(user.ID, current)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata is unavailable")
				return
			}
			if !matches {
				writeError(w, http.StatusConflict, "metadata_drift", "HomeDrive files differ from committed metadata")
				return
			}
			trash, err := userFiles.ListTrash()
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "HomeDrive Trash is unavailable")
				return
			}
			matches, err = db.TrashMatchesSnapshot(user.ID, trash)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive Trash metadata is unavailable")
				return
			}
			if !matches {
				writeError(w, http.StatusConflict, "metadata_drift", "HomeDrive Trash differs from committed metadata")
				return
			}
		}
		if err := db.BeginFileMutation(user.ID); err != nil {
			log.Printf("HomeDrive mutation marker for user %d: %v", user.ID, err)
			writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata is unavailable")
			return
		}
		buffered := httptest.NewRecorder()
		driveMux.ServeHTTP(buffered, r)
		response := buffered.Result()
		defer response.Body.Close()
		if response.StatusCode >= http.StatusInternalServerError {
			writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive operation needs recovery")
			return
		}
		if response.StatusCode >= http.StatusMultipleChoices {
			if err := db.CancelFileMutation(user.ID); err != nil {
				log.Printf("HomeDrive mutation marker cancellation for user %d: %v", user.ID, err)
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata update is pending recovery")
				return
			}
		} else {
			err := reconcile()
			if err != nil {
				log.Printf("HomeDrive metadata reconciliation for user %d: %v", user.ID, err)
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata update is pending recovery")
				return
			}
		}
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}
	mux.HandleFunc("/api/v1/drive", driveHandler)
	mux.HandleFunc("/api/v1/drive/", driveHandler)
	mux.HandleFunc("GET /api/v1/health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /api/v1/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := db.Ready(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Service is not ready")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, ok := authenticate(w, r, db); !ok {
			return
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		now := time.Now().UTC()
		samples := collector.Collect(r.Context())
		samples = append(samples,
			metric{ID: "uptime", Label: "Uptime", Value: time.Since(startedAt).Seconds(), Unit: "seconds", Scope: "application", State: "available", SampledAt: now},
			metric{ID: "go_heap", Label: "Go heap", Value: float64(memory.HeapAlloc), Unit: "bytes", Scope: "application", State: "available", SampledAt: now},
		)
		writeJSON(w, http.StatusOK, statusResponse{
			Status: "healthy", Version: "0.1.0-dev", SampledAt: now,
			Metrics: samples,
		})
	})

	if webDir != "" {
		if index, err := os.ReadFile(filepath.Join(webDir, "index.html")); err == nil {
			files := http.FileServer(http.Dir(webDir))
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Header().Set("Cache-Control", "no-store")
					_, _ = w.Write(index)
					return
				}
				files.ServeHTTP(w, r)
			})
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
