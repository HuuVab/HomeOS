package drive

import (
	"errors"
	"fmt"
	"os"
	"path"
)

// SnapshotTrashItem copies a legacy Trash tree into immutable blobs. It is
// intended for an offline migration with the HomeOS server stopped.
func (s *Store) SnapshotTrashItem(item TrashEntry) ([]Entry, error) {
	if !validID(item.ID) || !ValidCatalogPath(item.Path) {
		return nil, ErrInvalidPath
	}
	var result []Entry
	var walk func(string, string) error
	walk = func(onDisk, logical string) error {
		info, err := s.root.Lstat(onDisk)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidPath
		}
		entry := Entry{Path: logical, Name: path.Base(logical), Size: info.Size(), ModifiedAt: info.ModTime().UTC(), Version: 1}
		if info.IsDir() {
			entry.Kind = "folder"
			entry.Size = 0
			result = append(result, entry)
			directory, err := s.root.Open(onDisk)
			if err != nil {
				return err
			}
			children, err := directory.ReadDir(-1)
			directory.Close()
			if err != nil {
				return err
			}
			for _, child := range children {
				if !validPart(child.Name()) {
					return ErrInvalidPath
				}
				if err := walk(path.Join(onDisk, child.Name()), logical+"/"+child.Name()); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return ErrInvalidPath
		}
		entry.Kind = "file"
		file, err := s.root.Open(onDisk)
		if err != nil {
			return err
		}
		checksum, size, copyErr := s.PutBlob(file, info.Size())
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		after, err := s.root.Lstat(onDisk)
		if err != nil || !after.Mode().IsRegular() || after.Size() != size || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("Trash bytes changed during migration: %s", logical)
		}
		entry.Checksum = checksum
		entry.Size = size
		result = append(result, entry)
		return nil
	}
	if err := walk(path.Join(trashDir, item.ID), item.Path); err != nil {
		return nil, err
	}
	if len(result) == 0 || result[0].Path != item.Path {
		return nil, errors.New("invalid Trash snapshot")
	}
	return result, nil
}
