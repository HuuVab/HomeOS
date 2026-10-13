package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/store"
	"github.com/fsnotify/fsnotify"
)

// MusicScanner runs one bounded scan at a time. Watch events are hints; the periodic
// reconciliation is authoritative and also recovers from missed notifications.
type MusicScanner struct {
	db   *store.Store
	wake chan struct{}
}

var activeMusicScanner *MusicScanner

func StartMusicScanner(ctx context.Context, db *store.Store) {
	if !db.IsPostgres() {
		return
	}
	s := &MusicScanner{db: db, wake: make(chan struct{}, 1)}
	activeMusicScanner = s
	go s.run(ctx)
	go runMusicLoudnessWorker(ctx, db)
}

func wakeMusicScanner() {
	if activeMusicScanner != nil {
		select {
		case activeMusicScanner.wake <- struct{}{}:
		default:
		}
	}
}

func (s *MusicScanner) run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("HomeMusic watcher unavailable: %v", err)
	} else {
		defer watcher.Close()
	}
	var watchedRoot string
	for {
		library, err := s.db.MusicLibrary(ctx)
		if err == nil && library.Root != "" {
			if watcher != nil && library.Root != watchedRoot {
				for _, path := range watcher.WatchList() {
					_ = watcher.Remove(path)
				}
				watchedRoot = library.Root
			}
			if err := s.scan(ctx, library.Root, watcher); err != nil {
				log.Printf("HomeMusic scan: %v", err)
			}
		}
		var events <-chan fsnotify.Event
		var errors <-chan error
		if watcher != nil {
			events = watcher.Events
			errors = watcher.Errors
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		case _, ok := <-events:
			if !ok {
				watcher = nil
				continue
			}
			// Files are often written in several events; let the writer settle.
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
		case err := <-errors:
			if err != nil {
				log.Printf("HomeMusic watcher: %v", err)
			}
		}
	}
}

func musicFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *MusicScanner) scan(ctx context.Context, root string, watcher *fsnotify.Watcher) error {
	if !musicRootAllowed(root) {
		_ = s.db.MusicDeactivateRoot(ctx, root)
		_ = s.db.MusicScanFinish(ctx, root, time.Now().Unix(), "Music folder is outside approved roots")
		return errors.New("music folder is outside approved roots")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		_ = s.db.MusicDeactivateRoot(ctx, root)
		_ = s.db.MusicScanFinish(ctx, root, time.Now().Unix(), "Music folder is unavailable")
		return errors.New("music folder is unavailable")
	}
	stamp := time.Now().UnixNano()
	if err := s.db.MusicScanStart(ctx, root, time.Now().Unix()); err != nil {
		return err
	}
	probe, _ := exec.LookPath("ffprobe")
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if watcher != nil {
				_ = watcher.Add(path)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || musicTypes[strings.ToLower(filepath.Ext(path))] == "" {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		existing, _, err := s.db.MusicTrackAtPath(ctx, path)
		if err == nil && existing.Size == info.Size() && existing.ModifiedAt == info.ModTime().UnixNano() {
			return s.db.MusicMarkSeen(ctx, existing.ID, stamp)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		hash, err := musicFileHash(path)
		if err != nil {
			return err
		}
		id := existing.ID
		if id == "" {
			match, matchErr := s.db.MusicMatchHash(ctx, hash, root, stamp)
			if matchErr == nil {
				id = match
			} else if !errors.Is(matchErr, sql.ErrNoRows) {
				return matchErr
			}
		}
		if id == "" {
			id = store.NewMusicID()
		}
		metadata := readMusicMetadata(ctx, probe, path, filepath.Base(path), info.Size(), info.ModTime())
		return s.db.UpsertMusicTrack(ctx, store.MusicTrack{ID: id, Path: path, Name: filepath.Base(path), Title: metadata.Title, Artist: metadata.Artist, Album: metadata.Album, Duration: metadata.Duration, Size: info.Size(), ModifiedAt: info.ModTime().UnixNano(), ContentHash: hash, HasCover: metadata.HasCover, CoverStream: metadata.CoverStream, Available: true}, stamp)
	})
	if walkErr != nil {
		_ = s.db.MusicScanFinish(ctx, root, time.Now().Unix(), walkErr.Error())
		return walkErr
	}
	library, err := s.db.MusicLibrary(ctx)
	if err != nil {
		return err
	}
	if library.Root != root {
		return s.db.MusicDeactivateRoot(ctx, root)
	}
	if err := s.db.MusicMarkMissing(ctx, root, stamp); err != nil {
		return err
	}
	return s.db.MusicScanFinish(ctx, root, time.Now().Unix(), "")
}
