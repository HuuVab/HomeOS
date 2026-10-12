package main

import (
	"context"
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
	"github.com/HuuVab/HomeOS/backend/internal/metadata"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/monitor"
	"github.com/HuuVab/HomeOS/backend/internal/redisaccel"
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
	if len(os.Args) > 1 && os.Args[1] == "migrate-postgres" {
		if databaseURL == "" || len(os.Args) > 3 || (len(os.Args) == 3 && os.Args[2] != "--apply") {
			log.Fatal("usage: set HOMEOS_DATABASE_URL, then homeos migrate-postgres [--apply]")
		}
		if _, err := os.Stat(dbPath); err != nil {
			log.Fatal("SQLite source database does not exist: ", err)
		}
		source, err := store.Open(dbPath)
		if err != nil {
			log.Fatal("open SQLite source: ", err)
		}
		defer source.Close()
		files, err := drive.New(filepath.Join(dataDir, "files"))
		if err != nil {
			log.Fatal("open HomeDrive files: ", err)
		}
		defer files.Close()
		owners, err := source.UserIDs()
		if err != nil {
			log.Fatal("list SQLite users: ", err)
		}
		fmt.Printf("SQLite source: %d users\n", len(owners))
		for _, ownerID := range owners {
			userFiles, err := files.ForUser(ownerID)
			if err != nil {
				log.Fatal("open owner files: ", err)
			}
			entries, err := userFiles.AllEntries()
			if err != nil {
				log.Fatal("scan owner files: ", err)
			}
			trash, err := userFiles.ListTrash()
			if err != nil {
				log.Fatal("scan owner Trash: ", err)
			}
			fmt.Printf("Owner %d: %d files/folders, %d Trash entries, %d upload sessions\n", ownerID, len(entries), len(trash), len(userFiles.PendingUploads()))
		}
		if len(os.Args) == 2 {
			fmt.Println("Dry run only. Stop the server and back up SQLite and the file directory, then re-run with --apply.")
			return
		}
		if err := store.MigrateSQLiteToPostgres(source, db, files); err != nil {
			log.Fatal("migrate HomeDrive: ", err)
		}
		fmt.Println("PostgreSQL migration complete. Existing sessions were not copied; sign in again.")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "drive-import" {
		if len(os.Args) < 3 || len(os.Args) > 4 || (len(os.Args) == 4 && os.Args[3] != "--apply") {
			log.Fatal("usage: homeos drive-import <owner-id> [--apply]")
		}
		ownerID, parseErr := strconv.ParseInt(os.Args[2], 10, 64)
		if parseErr != nil || ownerID < 1 {
			log.Fatal("owner-id must be a positive integer")
		}
		if err := db.CheckDriveImport(ownerID); err != nil {
			log.Fatal("drive import is unavailable: ", err)
		}
		files, err := drive.New(filepath.Join(dataDir, "files"))
		if err != nil {
			log.Fatal("open HomeDrive: ", err)
		}
		defer files.Close()
		userFiles, err := files.ForUser(ownerID)
		if err != nil {
			log.Fatal("open owner files: ", err)
		}
		entries, err := userFiles.AllEntries()
		if err != nil {
			log.Fatal("scan owner files: ", err)
		}
		trash, err := userFiles.ListTrash()
		if err != nil {
			log.Fatal("scan owner Trash: ", err)
		}
		uploads := userFiles.PendingUploads()
		fmt.Printf("Owner %d: %d files/folders, %d Trash entries, %d upload sessions\n", ownerID, len(entries), len(trash), len(uploads))
		if len(os.Args) == 3 {
			fmt.Println("Dry run only. Re-run with --apply to import into the empty PostgreSQL catalog.")
			return
		}
		if err := db.ImportDriveSnapshot(ownerID, entries, trash, uploads); err != nil {
			log.Fatal("import HomeDrive snapshot: ", err)
		}
		fmt.Println("HomeDrive snapshot imported.")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "drive-repair" {
		if databaseURL == "" || len(os.Args) < 3 || len(os.Args) > 4 || (len(os.Args) == 4 && os.Args[3] != "--apply") {
			log.Fatal("usage: set HOMEOS_DATABASE_URL, then homeos drive-repair <owner-id> [--apply]")
		}
		ownerID, parseErr := strconv.ParseInt(os.Args[2], 10, 64)
		if parseErr != nil || ownerID < 1 {
			log.Fatal("owner-id must be a positive integer")
		}
		files, err := drive.New(filepath.Join(dataDir, "files"))
		if err != nil {
			log.Fatal("open HomeDrive: ", err)
		}
		defer files.Close()
		userFiles, err := files.ForUser(ownerID)
		if err != nil {
			log.Fatal("open owner files: ", err)
		}
		entries, err := userFiles.AllEntries()
		if err != nil {
			log.Fatal("scan owner files: ", err)
		}
		apply := len(os.Args) == 4
		report, err := db.RepairDriveCatalog(ownerID, entries, apply)
		fmt.Printf("Owner %d: %d entries, %d missing on disk, %d missing in catalog, %d mismatched (%d unverified), %d missing checksums\n",
			ownerID, report.Entries, report.MissingOnDisk, report.MissingInCatalog, report.Mismatched, report.Unverified, report.MissingChecksums)
		if err != nil {
			log.Fatal("repair refused: ", err)
		}
		if apply {
			fmt.Println("Safe catalog repairs, checksums, and version snapshots applied. Restart the server before editing files.")
		} else {
			fmt.Println("Dry run only. Stop the server, back up PostgreSQL and HomeDrive files, then re-run with --apply.")
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate-blobs" {
		if databaseURL == "" || len(os.Args) < 3 || len(os.Args) > 4 || (len(os.Args) == 4 && os.Args[3] != "--apply") {
			log.Fatal("usage: set HOMEOS_DATABASE_URL, then homeos migrate-blobs <owner-id> [--apply]")
		}
		ownerID, parseErr := strconv.ParseInt(os.Args[2], 10, 64)
		if parseErr != nil || ownerID < 1 {
			log.Fatal("owner-id must be a positive integer")
		}
		bindAddress := os.Getenv("HOMEOS_BIND_ADDR")
		if bindAddress == "" {
			bindAddress = "127.0.0.1"
		}
		port := os.Getenv("HOMEOS_PORT")
		if port == "" {
			port = "8080"
		}
		connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort(bindAddress, port), time.Second)
		if dialErr == nil {
			connection.Close()
			log.Fatal("stop the HomeOS server before migrating blob storage")
		}
		files, err := drive.New(filepath.Join(dataDir, "files"))
		if err != nil {
			log.Fatal("open HomeDrive: ", err)
		}
		defer files.Close()
		userFiles, err := files.ForUser(ownerID)
		if err != nil {
			log.Fatal("open owner files: ", err)
		}
		apply := len(os.Args) == 4
		report, err := db.MigrateOwnerToBlobs(ownerID, userFiles, apply)
		if err != nil {
			log.Fatal("blob migration refused: ", err)
		}
		fmt.Printf("Owner %d: %d active entries, %d Trash entries, %d Trash nodes, %d verified blobs.\n", ownerID, report.ActiveEntries, report.TrashEntries, report.TrashNodes, report.VerifiedBlobs)
		if apply {
			fmt.Println("Blob catalog activated. Restart HomeOS and verify Drive operations before removing the legacy tree.")
		} else {
			fmt.Println("Dry run only. Back up PostgreSQL and HomeDrive files, then re-run with --apply.")
		}
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
		log.Fatal("usage: homeos [create-admin <username> | create-user <username> | reset-admin-password <username> | backup <new-backup-path> | drive-import <owner-id> [--apply] | migrate-postgres [--apply] | migrate-blobs <owner-id> [--apply]]")
	}
	var accelerator *redisaccel.Layer
	if redisURL := os.Getenv("HOMEOS_REDIS_URL"); redisURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		accelerator, err = redisaccel.Open(ctx, redisURL)
		cancel()
		if err != nil {
			log.Fatal("configure Redis acceleration: ", err)
		}
		defer accelerator.Close()
		log.Print("Redis acceleration enabled")
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
		if db.AuthoritativeMetadata() {
			format, formatErr := db.StorageFormat(ownerID)
			if formatErr != nil {
				log.Fatalf("read storage format for user %d: %v", ownerID, formatErr)
			}
			if format == "blobs" {
				log.Fatalf("unexpected legacy mutation marker for blob owner %d; refusing filesystem reconciliation", ownerID)
			}
		}
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
					err = db.ReconcileDriveMutation(ownerID, entries, trash, userFiles.PendingUploads())
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
	if db.AuthoritativeMetadata() {
		ownerIDs, err := db.UserIDs()
		if err != nil {
			log.Fatal("list HomeDrive owners: ", err)
		}
		for _, ownerID := range ownerIDs {
			format, formatErr := db.StorageFormat(ownerID)
			if formatErr != nil {
				log.Fatalf("read storage format for user %d: %v", ownerID, formatErr)
			}
			if format == "blobs" {
				continue
			}
			userFiles, err := files.ForUser(ownerID)
			if err == nil {
				err = db.RecoverUploadSessions(ownerID, userFiles.PendingUploads())
			}
			if err != nil {
				log.Fatalf("recover upload sessions for user %d: %v", ownerID, err)
			}
		}
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
	versionDays := 90
	if configured := os.Getenv("HOMEOS_VERSION_RETENTION_DAYS"); configured != "" {
		versionDays, err = strconv.Atoi(configured)
		if err != nil || versionDays < 1 {
			log.Fatal("HOMEOS_VERSION_RETENTION_DAYS must be a positive integer")
		}
	}
	versionKeep := 10
	if configured := os.Getenv("HOMEOS_VERSION_MIN_KEEP"); configured != "" {
		versionKeep, err = strconv.Atoi(configured)
		if err != nil || versionKeep < 1 {
			log.Fatal("HOMEOS_VERSION_MIN_KEEP must be a positive integer")
		}
	}
	cleanup := func() {
		if !db.AuthoritativeMetadata() {
			if _, err := files.PurgeExpired(retention, time.Now()); err != nil {
				log.Printf("HomeDrive trash cleanup: %v", err)
			}
			return
		}
		db.LockDriveMutations()
		defer db.UnlockDriveMutations()
		owners, err := db.TrashOwners()
		if err != nil {
			log.Printf("list HomeDrive Trash owners: %v", err)
			return
		}
		for _, ownerID := range owners {
			format, formatErr := db.StorageFormat(ownerID)
			if formatErr != nil {
				log.Printf("HomeDrive storage format for user %d: %v", ownerID, formatErr)
				continue
			}
			if format == "blobs" {
				if _, err := db.CatalogPurgeExpiredTrash(ownerID, time.Now().Add(-retention).Unix()); err != nil {
					log.Printf("HomeDrive catalog Trash cleanup for user %d: %v", ownerID, err)
				}
				continue
			}
			userFiles, err := files.ForUser(ownerID)
			if err == nil {
				err = db.BeginFileMutation(ownerID)
			}
			if err == nil {
				_, err = userFiles.PurgeExpired(retention, time.Now())
			}
			var entries []drive.Entry
			if err == nil {
				entries, err = userFiles.AllEntries()
			}
			var trash []drive.TrashEntry
			if err == nil {
				trash, err = userFiles.ListTrash()
			}
			if err == nil {
				err = db.ReconcileDriveMutation(ownerID, entries, trash, userFiles.PendingUploads())
			}
			if err == nil && accelerator != nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				if cacheErr := accelerator.InvalidateOwner(ctx, ownerID); cacheErr != nil {
					log.Printf("HomeDrive Redis listing invalidation failed: %v", cacheErr)
				}
				cancel()
			}
			if err != nil {
				log.Printf("HomeDrive Trash cleanup for user %d: %v", ownerID, err)
			}
		}
		allOwners, err := db.UserIDs()
		if err != nil {
			log.Printf("list HomeDrive version owners: %v", err)
			return
		}
		cutoff := time.Now().Add(-time.Duration(versionDays) * 24 * time.Hour).Unix()
		for _, ownerID := range allOwners {
			userFiles, err := files.ForUser(ownerID)
			if err == nil {
				var pruned int
				pruned, err = db.PruneFileVersions(ownerID, cutoff, versionKeep)
				if pruned > 0 {
					log.Printf("HomeDrive pruned %d old versions for user %d", pruned, ownerID)
				}
			}
			var referenced map[string]bool
			if err == nil {
				referenced, err = db.ReferencedVersionChecksums(ownerID)
			}
			if err == nil {
				var removed int
				removed, err = userFiles.PruneVersionBlobs(referenced)
				if removed > 0 {
					log.Printf("HomeDrive removed %d unreferenced version blobs for user %d", removed, ownerID)
				}
			}
			if err != nil {
				log.Printf("HomeDrive version cleanup for user %d: %v", ownerID, err)
			}
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

	var metadataClient metadatapb.MetadataServiceClient
	if db.AuthoritativeMetadata() {
		var closeMetadata func()
		metadataClient, closeMetadata, err = metadata.NewInProcessClient(db, files, accelerator)
		if err != nil {
			log.Fatal("start metadata gRPC service: ", err)
		}
		defer closeMetadata()
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandlerWithRedis(os.Getenv("HOMEOS_WEB_DIR"), db, monitor.NewCollector(dataDir), files, accelerator, metadataClient),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HomeOS listening on http://%s", addr)
	log.Fatal(server.ListenAndServe())
}
