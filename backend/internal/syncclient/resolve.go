package syncclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ResolveFileConflict makes an explicit file-only choice. Keeping remote
// archives any local bytes first; keeping local uses the current remote
// revision as an optimistic precondition.
func ResolveFileConflict(ctx context.Context, directory string, state *State, remote *Remote, name, choice string) error {
	if !validPath(name) || (choice != "keep-local" && choice != "keep-remote") {
		return errors.New("expected a safe file path and keep-local or keep-remote")
	}
	conflicts, err := state.Conflicts()
	if err != nil {
		return err
	}
	found := false
	for _, item := range conflicts {
		if item.Path == name {
			found = true
			break
		}
	}
	if !found {
		return errors.New("no recorded conflict exists for that path")
	}
	server, err := remote.ListAll(ctx)
	if err != nil {
		return err
	}
	entry, remoteExists := server[name]
	if remoteExists && entry.Kind != "file" {
		return errors.New("folder conflicts require manual resolution")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(name))
	localExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if localExists {
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("folder conflicts require manual resolution")
		}
	}
	if choice == "keep-remote" {
		if localExists {
			file.Close()
			if err := archiveRemoteDeletion(root, name); err != nil {
				return err
			}
		}
		if err := state.RemoveBaseline(name); err != nil {
			return err
		}
		if !remoteExists {
			return nil
		}
		hash, err := downloadFile(ctx, root, remote, name)
		if err != nil {
			return err
		}
		return state.SaveBaseline(Baseline{Path: name, Kind: "file", RemoteVersion: entry.Version, LocalHash: hash})
	}
	if !localExists {
		if remoteExists {
			if err := remote.Trash(ctx, name, entry.Version); err != nil {
				return err
			}
		}
		return state.RemoveBaseline(name)
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxPrototypeFileBytes {
		return errors.New("file exceeds the sync transfer range")
	}
	fullHash := sha256.New()
	if _, err := io.Copy(fullHash, file); err != nil {
		return err
	}
	checksum := hex.EncodeToString(fullHash.Sum(nil))
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	sentHash := sha256.New()
	reader := io.TeeReader(file, sentHash)
	if remoteExists {
		err = remote.UploadUpdated(ctx, name, entry.Version, reader)
	} else if info.Size() > resumableThreshold {
		err = remote.UploadResumable(ctx, name, file, info.Size(), checksum)
		if err == nil {
			if _, err = file.Seek(0, io.SeekStart); err == nil {
				verified := sha256.New()
				_, err = io.Copy(verified, file)
				if err == nil && hex.EncodeToString(verified.Sum(nil)) != checksum {
					err = fmt.Errorf("local file changed during conflict resolution; remote contents may differ")
				}
			}
		}
	} else {
		err = remote.UploadNew(ctx, name, reader)
	}
	if err != nil {
		return err
	}
	if remoteExists || info.Size() <= resumableThreshold {
		if hex.EncodeToString(sentHash.Sum(nil)) != checksum {
			return fmt.Errorf("local file changed during conflict resolution; remote contents may differ")
		}
	}
	version := int64(1)
	if remoteExists {
		version = entry.Version + 1
	}
	return state.SaveBaseline(Baseline{Path: name, Kind: "file", RemoteVersion: version, LocalHash: checksum})
}
