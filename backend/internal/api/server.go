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
	"sync"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/monitor"
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

func NewHandler(webDir string, db *store.Store, collector monitor.Collector, files *drive.Store) http.Handler {
	mux := http.NewServeMux()
	var mutationMu sync.Mutex
	registerAuthRoutes(mux, db)
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
			pending, err := db.HasPendingFileMutation(user.ID)
			if err == nil && pending {
				mutationMu.Lock()
				pending, err = db.HasPendingFileMutation(user.ID)
				if err == nil && pending {
					var entries []drive.Entry
					entries, err = userFiles.AllEntries()
					if err == nil {
						err = db.ReconcileFileMutation(user.ID, entries)
					}
				}
				mutationMu.Unlock()
			}
			if err != nil {
				log.Printf("HomeDrive pending metadata recovery for user %d: %v", user.ID, err)
				writeError(w, http.StatusServiceUnavailable, "metadata_unavailable", "HomeDrive metadata recovery is pending")
				return
			}
		}
		driveMux := http.NewServeMux()
		registerDriveRoutes(driveMux, db, userFiles, user.ID)
		metadataChanged := r.Method == http.MethodPut || r.Method == http.MethodDelete ||
			(r.Method == http.MethodPost && r.URL.Path != "/api/v1/drive/uploads")
		if !metadataChanged {
			driveMux.ServeHTTP(w, r)
			return
		}
		mutationMu.Lock()
		defer mutationMu.Unlock()
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
			entries, err := userFiles.AllEntries()
			if err == nil {
				err = db.ReconcileFileMutation(user.ID, entries)
			}
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
