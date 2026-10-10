package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
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

func NewHandler(webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /api/v1/health/ready", func(w http.ResponseWriter, _ *http.Request) {
		// Database readiness joins this check when SQLite is added.
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, _ *http.Request) {
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
					_, _ = w.Write(index)
					return
				}
				files.ServeHTTP(w, r)
			})
		}
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
