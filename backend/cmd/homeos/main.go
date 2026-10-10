package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/api"
)

func main() {
	// v0.1's first slice has no authentication yet. Keep it on loopback.
	addr := "127.0.0.1:8080"
	if port := os.Getenv("HOMEOS_PORT"); port != "" {
		addr = "127.0.0.1:" + port
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(os.Getenv("HOMEOS_WEB_DIR")),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HomeOS listening on http://%s", addr)
	log.Fatal(server.ListenAndServe())
}
