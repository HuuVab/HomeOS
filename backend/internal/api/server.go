package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

var startedAt = time.Now()

type metric struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Scope     string    `json:"scope"`
	State     string    `json:"state"`
	SampledAt time.Time `json:"sampledAt"`
}

type statusResponse struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	SampledAt time.Time `json:"sampledAt"`
	Metrics   []metric  `json:"metrics"`
}

func NewHandler(webDir string, db *store.Store) http.Handler {
	mux := http.NewServeMux()
	registerAuthRoutes(mux, db)
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
		writeJSON(w, http.StatusOK, statusResponse{
			Status: "healthy", Version: "0.1.0-dev", SampledAt: now,
			Metrics: []metric{
				{ID: "uptime", Label: "Uptime", Value: time.Since(startedAt).Seconds(), Unit: "seconds", Scope: "application", State: "available", SampledAt: now},
				{ID: "go_heap", Label: "Go heap", Value: float64(memory.HeapAlloc), Unit: "bytes", Scope: "application", State: "available", SampledAt: now},
			},
		})
	})

	if webDir != "" {
		if index, err := os.ReadFile(filepath.Join(webDir, "index.html")); err == nil {
			files := http.FileServer(http.Dir(webDir))
			mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
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
