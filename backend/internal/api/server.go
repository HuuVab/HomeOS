package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
		driveMux := http.NewServeMux()
		registerDriveRoutes(driveMux, db, userFiles)
		driveMux.ServeHTTP(w, r)
		metadataChanged := r.Method == http.MethodPut || r.Method == http.MethodDelete ||
			(r.Method == http.MethodPost && r.URL.Path != "/api/v1/drive/uploads")
		if metadataChanged {
			entries, err := userFiles.AllEntries()
			if err == nil {
				err = db.ReplaceFileEntries(user.ID, entries)
			}
			if err != nil {
				log.Printf("HomeDrive metadata sync for user %d: %v", user.ID, err)
			}
		}
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
