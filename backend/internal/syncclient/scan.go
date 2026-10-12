package syncclient

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type LocalEntry struct {
	Path string
	Kind string
	Hash string
	Size int64
}

// ScanLocal records offline changes without contacting or modifying the server.
func ScanLocal(root string, state *State) (map[string]LocalEntry, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	baselines, err := state.Baselines()
	if err != nil {
		return nil, err
	}
	cache, err := state.ScanCache()
	if err != nil {
		return nil, err
	}
	lastFull, err := state.Meta("last_full_hash")
	if err != nil {
		return nil, err
	}
	lastFullUnix, parseErr := strconv.ParseInt(lastFull, 10, 64)
	fullAudit := parseErr != nil || time.Since(time.Unix(lastFullUnix, 0)) >= 24*time.Hour
	nextCache := make(map[string]ScanCacheEntry)
	local := make(map[string]LocalEntry)
	err = filepath.WalkDir(absolute, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(absolute, filename)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if relative == stateDir {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("sync refuses symbolic link: %s", relative)
		}
		name := filepath.ToSlash(relative)
		if !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
			return fmt.Errorf("invalid local filename: %s", relative)
		}
		item := LocalEntry{Path: name}
		if entry.IsDir() {
			item.Kind = "folder"
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("sync refuses non-regular file: %s", relative)
			}
			cached, known := cache[name]
			if !fullAudit && known && cached.Size == info.Size() && cached.ModifiedNS == info.ModTime().UnixNano() {
				item.Hash = cached.Hash
			} else {
				file, err := os.Open(filename)
				if err != nil {
					return err
				}
				hash := sha256.New()
				_, copyErr := io.Copy(hash, file)
				closeErr := file.Close()
				if copyErr != nil {
					return copyErr
				}
				if closeErr != nil {
					return closeErr
				}
				item.Hash = hex.EncodeToString(hash.Sum(nil))
			}
			item.Kind = "file"
			item.Size = info.Size()
			nextCache[name] = ScanCacheEntry{Size: info.Size(), ModifiedNS: info.ModTime().UnixNano(), Hash: item.Hash}
		}
		local[name] = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := state.SaveScanCache(nextCache); err != nil {
		return nil, err
	}
	if fullAudit {
		if err := state.SetMeta("last_full_hash", strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
			return nil, err
		}
	}
	for name, item := range local {
		baseline, known := baselines[name]
		if !known {
			if err := state.Queue(Pending{Path: name, Operation: "create", LocalHash: item.Hash}); err != nil {
				return nil, err
			}
			continue
		}
		if baseline.Kind != item.Kind {
			if err := state.MarkConflict(name, "local file type changed"); err != nil {
				return nil, err
			}
			continue
		}
		if item.Kind == "file" && baseline.LocalHash != item.Hash {
			if err := state.Queue(Pending{Path: name, Operation: "update", BaseVersion: baseline.RemoteVersion, LocalHash: item.Hash}); err != nil {
				return nil, err
			}
		} else if err := state.ClearPending(name); err != nil {
			return nil, err
		}
	}
	for name, baseline := range baselines {
		if _, exists := local[name]; !exists {
			if err := state.Queue(Pending{Path: name, Operation: "delete", BaseVersion: baseline.RemoteVersion}); err != nil {
				return nil, err
			}
		}
	}
	return local, nil
}
