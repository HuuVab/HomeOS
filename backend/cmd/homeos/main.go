package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/api"
	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/monitor"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"golang.org/x/term"
)

var validUsername = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

func main() {
	dataDir := os.Getenv("HOMEOS_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	db, err := store.Open(filepath.Join(dataDir, "homeos.sqlite"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if len(os.Args) > 1 && (os.Args[1] == "create-admin" || os.Args[1] == "reset-admin-password") {
		if len(os.Args) != 3 || !validUsername.MatchString(os.Args[2]) {
			log.Fatal("usage: homeos <create-admin|reset-admin-password> <3-32 character username (letters, digits, underscore)>")
		}
		fmt.Fprint(os.Stderr, "Admin password: ")
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Fatal("read password from terminal: ", err)
		}
		hash, err := auth.HashPassword(string(password))
		for i := range password {
			password[i] = 0
		}
		if err != nil {
			log.Fatal(err)
		}
		if os.Args[1] == "reset-admin-password" {
			err = db.ResetAdminPassword(os.Args[2], hash)
		} else {
			err = db.CreateAdmin(os.Args[2], hash)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Administrator credentials updated.")
		return
	}
	if len(os.Args) > 1 {
		log.Fatal("usage: homeos [create-admin|reset-admin-password <username>]")
	}

	addr := "127.0.0.1:8080"
	if port := os.Getenv("HOMEOS_PORT"); port != "" {
		addr = "127.0.0.1:" + port
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(os.Getenv("HOMEOS_WEB_DIR"), db, monitor.NewCollector(dataDir)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HomeOS listening on http://%s", addr)
	log.Fatal(server.ListenAndServe())
}
