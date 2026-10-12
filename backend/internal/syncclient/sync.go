package syncclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const maxPrototypeFileBytes = 32 << 20

type Report struct {
	Downloaded int
	Uploaded   int
	Folders    int
	Conflicts  int
	Retried    int
	Cursor     int64
}

func Sync(ctx context.Context, directory string, state *State, remote *Remote) (Report, error) {
	var report Report
	if remote.OwnerID() < 1 {
		return report, errors.New("login required")
	}
	origin, err := state.Meta("origin")
	if err != nil {
		return report, err
	}
	owner, err := state.Meta("owner")
	if err != nil {
		return report, err
	}
	if origin != "" && origin != remote.Origin() || owner != "" && owner != strconv.FormatInt(remote.OwnerID(), 10) {
		return report, errors.New("this sync folder is pinned to another HomeOS server or account")
	}
	if origin == "" {
		if err := state.SetMeta("origin", remote.Origin()); err != nil {
			return report, err
		}
	}
	if owner == "" {
		if err := state.SetMeta("owner", strconv.FormatInt(remote.OwnerID(), 10)); err != nil {
			return report, err
		}
	}
	local, err := ScanLocal(directory, state)
	if err != nil {
		return report, err
	}
	baselines, err := state.Baselines()
	if err != nil {
		return report, err
	}
	pendingList, err := state.Pending()
	if err != nil {
		return report, err
	}
	pending := make(map[string]Pending, len(pendingList))
	for _, item := range pendingList {
		pending[item.Path] = item
	}
	server, err := remote.ListAll(ctx)
	if err != nil {
		return report, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return report, err
	}
	defer root.Close()
	names := make([]string, 0, len(server))
	for name := range server {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		depthI, depthJ := pathDepth(names[i]), pathDepth(names[j])
		if depthI != depthJ {
			return depthI < depthJ
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		entry := server[name]
		current, exists := local[name]
		baseline, known := baselines[name]
		if queued, hasPending := pending[name]; hasPending {
			reason := "local and remote changes need manual resolution"
			if queued.Operation == "update" && entry.Version == queued.BaseVersion {
				reason = "remote overwrite is not supported by this prototype"
			} else if queued.Operation == "delete" {
				reason = "local deletion is not automatically propagated"
			}
			if err := state.MarkConflict(name, reason); err != nil {
				return report, err
			}
			report.Conflicts++
			continue
		}
		if exists && (!known || current.Kind != entry.Kind) {
			if err := state.MarkConflict(name, "local item collides with a remote item"); err != nil {
				return report, err
			}
			report.Conflicts++
			continue
		}
		if known && baseline.RemoteVersion == entry.Version && exists {
			continue
		}
		if entry.Kind == "folder" {
			if err := root.MkdirAll(filepath.FromSlash(name), 0700); err != nil {
				return report, err
			}
			if err := state.SaveBaseline(Baseline{Path: name, Kind: "folder", RemoteVersion: entry.Version}); err != nil {
				return report, err
			}
			report.Folders++
			continue
		}
		if entry.Size > maxPrototypeFileBytes {
			if err := state.MarkConflict(name, "file exceeds the 32 MiB prototype limit"); err != nil {
				return report, err
			}
			report.Conflicts++
			continue
		}
		hash, err := downloadFile(ctx, root, remote, name)
		if err != nil {
			return report, err
		}
		if err := state.SaveBaseline(Baseline{Path: name, Kind: "file", RemoteVersion: entry.Version, LocalHash: hash}); err != nil {
			return report, err
		}
		report.Downloaded++
	}
	queuedNames := make([]string, 0, len(pending))
	for name := range pending {
		queuedNames = append(queuedNames, name)
	}
	sort.Slice(queuedNames, func(i, j int) bool {
		depthI, depthJ := pathDepth(queuedNames[i]), pathDepth(queuedNames[j])
		if depthI != depthJ {
			return depthI < depthJ
		}
		return queuedNames[i] < queuedNames[j]
	})
	for _, name := range queuedNames {
		item := pending[name]
		if _, exists := server[name]; exists {
			continue
		}
		if item.Operation != "create" {
			if err := state.MarkConflict(name, "remote item was removed or local deletion/update cannot be applied automatically"); err != nil {
				return report, err
			}
			report.Conflicts++
			continue
		}
		if item.NextAttempt > time.Now().Unix() {
			continue
		}
		entry, exists := local[name]
		if !exists {
			continue
		}
		if parent := path.Dir(name); parent != "." {
			if _, exists := server[parent]; !exists {
				continue // The parent is still queued or failed; retry on the next run.
			}
		}
		if entry.Kind == "folder" {
			err = remote.CreateFolder(ctx, name)
		} else if entry.Size > maxPrototypeFileBytes {
			err = errors.New("file exceeds the 32 MiB prototype limit")
		} else {
			var file *os.File
			file, err = root.Open(filepath.FromSlash(name))
			if err == nil {
				err = remote.UploadNew(ctx, name, file)
				closeErr := file.Close()
				if err == nil && !errors.Is(closeErr, os.ErrClosed) {
					err = closeErr
				}
			}
		}
		if err != nil {
			if retryErr := state.Retry(name, err); retryErr != nil {
				return report, retryErr
			}
			report.Retried++
			continue
		}
		if err := state.SaveBaseline(Baseline{Path: name, Kind: entry.Kind, RemoteVersion: 1, LocalHash: entry.Hash}); err != nil {
			return report, err
		}
		server[name] = RemoteEntry{Path: name, Kind: entry.Kind, Version: 1}
		if entry.Kind == "folder" {
			report.Folders++
		} else {
			report.Uploaded++
		}
	}
	cursorText, err := state.Meta("cursor")
	if err != nil {
		return report, err
	}
	if cursorText != "" {
		report.Cursor, err = strconv.ParseInt(cursorText, 10, 64)
		if err != nil {
			return report, err
		}
	}
	for page := 0; page < 100; page++ {
		changes, next, err := remote.Changes(ctx, report.Cursor)
		if err != nil {
			return report, err
		}
		if next == report.Cursor || len(changes) == 0 {
			break
		}
		report.Cursor = next
		if err := state.SetMeta("cursor", strconv.FormatInt(next, 10)); err != nil {
			return report, err
		}
		if len(changes) < 100 {
			break
		}
	}
	return report, nil
}

func pathDepth(name string) int {
	depth := 0
	for _, char := range name {
		if char == '/' {
			depth++
		}
	}
	return depth
}

func downloadFile(ctx context.Context, root *os.Root, remote *Remote, name string) (string, error) {
	response, err := remote.Download(ctx, name)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %q: HTTP %d", name, response.StatusCode)
	}
	if response.ContentLength > maxPrototypeFileBytes {
		return "", errors.New("download exceeds the 32 MiB prototype limit")
	}
	if err := root.MkdirAll(filepath.FromSlash(path.Dir(name)), 0700); err != nil {
		return "", err
	}
	if err := root.MkdirAll(filepath.FromSlash(stateDir+"/tmp"), 0700); err != nil {
		return "", err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	temporary := filepath.FromSlash(stateDir + "/tmp/" + hex.EncodeToString(token[:]))
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer root.Remove(temporary)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxPrototypeFileBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > maxPrototypeFileBytes {
		return "", errors.New("download exceeds the 32 MiB prototype limit")
	}
	if err := root.Rename(temporary, filepath.FromSlash(name)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
