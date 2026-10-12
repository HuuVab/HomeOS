package syncclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolveFolderConflict applies one explicit choice to a conflicting subtree.
// Displaced local content is archived; displaced server content goes to Trash.
func ResolveFolderConflict(ctx context.Context, directory string, state *State, remote *Remote, name, choice string) error {
	if !validPath(name) || (choice != "keep-local" && choice != "keep-remote") {
		return errors.New("expected a safe folder path and keep-local or keep-remote")
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
		return errors.New("no recorded conflict exists for that folder")
	}
	server, err := remote.ListAll(ctx)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	localInfo, localErr := root.Lstat(filepath.FromSlash(name))
	localExists := localErr == nil
	if localErr != nil && !errors.Is(localErr, os.ErrNotExist) {
		return localErr
	}
	if localExists && localInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("sync refuses symbolic links")
	}
	remoteRoot, remoteExists := server[name]
	if !localExists && !remoteExists {
		return errors.New("neither side contains the conflicting folder")
	}
	if localExists && !localInfo.IsDir() && (!remoteExists || remoteRoot.Kind != "folder") {
		return errors.New("this is a file conflict; use file resolution")
	}
	if choice == "keep-remote" {
		if localExists {
			if err := archiveRemoteDeletion(root, name); err != nil {
				return err
			}
		}
		if err := state.RemoveTree(name); err != nil {
			return err
		}
		if !remoteExists {
			return nil
		}
		names := subtreeNames(server, name)
		for _, itemName := range names {
			entry := server[itemName]
			if entry.Kind == "folder" {
				if err := root.MkdirAll(filepath.FromSlash(itemName), 0700); err != nil {
					return err
				}
				if err := state.SaveBaseline(Baseline{Path: itemName, Kind: "folder", RemoteVersion: entry.Version}); err != nil {
					return err
				}
				continue
			}
			if entry.Size > maxPrototypeFileBytes {
				return fmt.Errorf("remote file %s exceeds sync limit", itemName)
			}
			hash, err := downloadFile(ctx, root, remote, itemName)
			if err != nil {
				return err
			}
			if err := state.SaveBaseline(Baseline{Path: itemName, Kind: "file", RemoteVersion: entry.Version, LocalHash: hash}); err != nil {
				return err
			}
		}
		return nil
	}
	if !localExists || !localInfo.IsDir() {
		return errors.New("keep-local requires a local folder")
	}
	local, err := collectLocalFolder(directory, name)
	if err != nil {
		return err
	}
	if remoteExists {
		if err := remote.Trash(ctx, name, remoteRoot.Version); err != nil {
			return err
		}
	}
	if err := state.RemoveTree(name); err != nil {
		return err
	}
	for _, item := range local {
		if item.Kind == "folder" {
			if err := remote.CreateFolder(ctx, item.Path); err != nil {
				return err
			}
			if err := state.SaveBaseline(Baseline{Path: item.Path, Kind: "folder", RemoteVersion: 1}); err != nil {
				return err
			}
			continue
		}
		file, err := root.Open(filepath.FromSlash(item.Path))
		if err != nil {
			return err
		}
		hash := sha256.New()
		uploadErr := remote.UploadNew(ctx, item.Path, io.TeeReader(file, hash))
		closeErr := file.Close()
		if uploadErr != nil {
			return uploadErr
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != item.Hash {
			return fmt.Errorf("local file changed during upload: %s", item.Path)
		}
		if err := state.SaveBaseline(Baseline{Path: item.Path, Kind: "file", RemoteVersion: 1, LocalHash: item.Hash}); err != nil {
			return err
		}
	}
	return nil
}

func subtreeNames(entries map[string]RemoteEntry, name string) []string {
	names := make([]string, 0)
	for item := range entries {
		if item == name || strings.HasPrefix(item, name+"/") {
			names = append(names, item)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if pathDepth(names[i]) != pathDepth(names[j]) {
			return pathDepth(names[i]) < pathDepth(names[j])
		}
		return names[i] < names[j]
	})
	return names
}

func collectLocalFolder(directory, name string) ([]LocalEntry, error) {
	base := filepath.Join(directory, filepath.FromSlash(name))
	var result []LocalEntry
	err := filepath.WalkDir(base, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("sync refuses symbolic link: %s", filename)
		}
		relative, err := filepath.Rel(directory, filename)
		if err != nil {
			return err
		}
		item := LocalEntry{Path: filepath.ToSlash(relative)}
		if !validPath(item.Path) {
			return errors.New("unsafe local path")
		}
		if entry.IsDir() {
			item.Kind = "folder"
			result = append(result, item)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxPrototypeFileBytes {
			return fmt.Errorf("unsupported local file: %s", item.Path)
		}
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
		item.Kind, item.Size, item.Hash = "file", info.Size(), hex.EncodeToString(hash.Sum(nil))
		result = append(result, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if pathDepth(result[i].Path) != pathDepth(result[j].Path) {
			return pathDepth(result[i].Path) < pathDepth(result[j].Path)
		}
		return result[i].Path < result[j].Path
	})
	return result, nil
}
