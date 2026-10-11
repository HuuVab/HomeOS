package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	databaseURL := os.Getenv("HOMEOS_DATABASE_URL")
	if passwordFile := os.Getenv("HOMEOS_DATABASE_PASSWORD_FILE"); databaseURL != "" && passwordFile != "" {
		password, readErr := os.ReadFile(passwordFile)
		if readErr != nil {
			log.Fatal("read database password: ", readErr)
		}
		parsed, parseErr := url.Parse(databaseURL)
		if parseErr != nil || parsed.User == nil {
			log.Fatal("HOMEOS_DATABASE_URL must include a database username")
		}
		parsed.User = url.UserPassword(parsed.User.Username(), strings.TrimSpace(string(password)))
		databaseURL = parsed.String()
	}
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if databaseURL != "" {
			log.Fatal("the backup command is SQLite-only; use pg_dump for PostgreSQL")
		}
		if _, err := os.Stat(dbPath); err != nil {
			log.Fatal("live database does not exist: ", err)
		}
	}
	var db *store.Store
	var err error
	if databaseURL != "" {
		db, err = store.OpenPostgres(databaseURL)
	} else {
		log.Print("HOMEOS_DATABASE_URL is not set; using SQLite standalone mode")
		db, err = store.Open(dbPath)
	}
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
	maxFileBytes := int64(2 << 30)
	quotaBytes := int64(20 << 30)
	if configured := os.Getenv("HOMEOS_MAX_FILE_BYTES"); configured != "" {
		maxFileBytes, err = strconv.ParseInt(configured, 10, 64)
		if err != nil {
			log.Fatal("HOMEOS_MAX_FILE_BYTES must be a positive integer")
		}
	}
	if configured := os.Getenv("HOMEOS_USER_QUOTA_BYTES"); configured != "" {
		quotaBytes, err = strconv.ParseInt(configured, 10, 64)
		if err != nil {
			log.Fatal("HOMEOS_USER_QUOTA_BYTES must be a positive integer")
		}
	}
	if err := files.SetLimits(maxFileBytes, quotaBytes); err != nil {
		log.Fatal("HomeDrive limits require a positive file cap and a quota at least as large")
	}
	pendingOwners, err := db.PendingFileMutations()
	if err != nil {
		log.Fatal("load HomeDrive recovery markers: ", err)
	}
	for _, ownerID := range pendingOwners {
		userFiles, err := files.ForUser(ownerID)
		if err != nil {
			log.Fatalf("open HomeDrive for recovery of user %d: %v", ownerID, err)
		}
		entries, err := userFiles.AllEntries()
		if err == nil {
			if db.AuthoritativeMetadata() {
				var trash []drive.TrashEntry
				trash, err = userFiles.ListTrash()
				if err == nil {
					err = db.ReconcileDriveMutation(ownerID, entries, trash)
				}
			} else {
				err = db.ReconcileFileMutation(ownerID, entries)
			}
		}
		if err != nil {
			log.Fatalf("recover HomeDrive metadata for user %d: %v", ownerID, err)
		}
		log.Printf("Recovered HomeDrive metadata for user %d", ownerID)
	}
	if !db.AuthoritativeMetadata() {
		if entries, err := files.AllEntries(); err != nil {
			log.Fatal("scan HomeDrive: ", err)
		} else if err := db.ReplaceFileEntries(1, entries); err != nil {
			log.Fatal("index HomeDrive: ", err)
		}
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
