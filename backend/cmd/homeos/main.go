package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/api"
	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/drive"
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
	dbPath := filepath.Join(dataDir, "homeos.sqlite")
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if _, err := os.Stat(dbPath); err != nil {
			log.Fatal("live database does not exist: ", err)
		}
	}
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if len(os.Args) != 3 {
			log.Fatal("usage: homeos backup <new-backup-path>")
		}
		if err := db.Backup(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Database backup created and verified.")
		return
	}

	if len(os.Args) > 1 && (os.Args[1] == "create-admin" || os.Args[1] == "create-user" || os.Args[1] == "reset-admin-password") {
		if len(os.Args) != 3 || !validUsername.MatchString(os.Args[2]) {
			log.Fatal("usage: homeos <create-admin|create-user|reset-admin-password> <3-32 character username (letters, digits, underscore)>")
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
		} else if os.Args[1] == "create-user" {
			err = db.CreateUser(os.Args[2], hash)
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
		log.Fatal("usage: homeos [create-admin <username> | create-user <username> | reset-admin-password <username> | backup <new-backup-path>]")
	}

	bindAddress := os.Getenv("HOMEOS_BIND_ADDR")
	if bindAddress == "" {
		bindAddress = "127.0.0.1"
	}
	port := "8080"
	if configuredPort := os.Getenv("HOMEOS_PORT"); configuredPort != "" {
		port = configuredPort
	}
	addr := net.JoinHostPort(bindAddress, port)
	files, err := drive.New(filepath.Join(dataDir, "files"))
	if err != nil {
		log.Fatal("initialize HomeDrive: ", err)
	}
	defer files.Close()
	if entries, err := files.AllEntries(); err != nil {
		log.Fatal("scan HomeDrive: ", err)
	} else if err := db.ReplaceFileEntries(1, entries); err != nil {
		log.Fatal("index HomeDrive: ", err)
	}
	retentionDays := 30
	if configured := os.Getenv("HOMEOS_TRASH_RETENTION_DAYS"); configured != "" {
		retentionDays, err = strconv.Atoi(configured)
		if err != nil || retentionDays < 1 {
			log.Fatal("HOMEOS_TRASH_RETENTION_DAYS must be a positive integer")
		}
	}
	retention := time.Duration(retentionDays) * 24 * time.Hour
	cleanup := func() {
		if _, err := files.PurgeExpired(retention, time.Now()); err != nil {
			log.Printf("HomeDrive trash cleanup: %v", err)
		}
	}
	cleanup()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanup()
		}
	}()

	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(os.Getenv("HOMEOS_WEB_DIR"), db, monitor.NewCollector(dataDir), files),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HomeOS listening on http://%s", addr)
	log.Fatal(server.ListenAndServe())
}
