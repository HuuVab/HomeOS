package drive

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
)

const versionsDir = ".homeos-versions"

// SnapshotFile copies a regular file into immutable, content-addressed storage.
// The returned checksum identifies the snapshot and can be verified on restore.
func (s *Store) SnapshotFile(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resolved, err := s.resolve(name, false)
	if err != nil {
		return "", err
	}
	if err := s.root.Mkdir(versionsDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	dirInfo, err := s.root.Lstat(versionsDir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidPath
	}
	source, err := s.root.Open(resolved)
	if err != nil {
		return "", err
	}
	defer source.Close()
	before, err := source.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return "", ErrInvalidPath
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	temp := path.Join(versionsDir, ".snapshot-"+hex.EncodeToString(random[:]))
	target, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer s.root.Remove(temp)
	hash := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(target, hash), source)
	closeErr := target.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	after, err := s.root.Lstat(resolved)
	if err != nil || !after.Mode().IsRegular() || before.Size() != count || after.Size() != count || !after.ModTime().Equal(before.ModTime()) {
		return "", fmt.Errorf("file changed while snapshotting: %s", name)
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	blob := path.Join(versionsDir, checksum)
	if info, err := s.root.Lstat(blob); err == nil {
		if !info.Mode().IsRegular() || info.Size() != count {
			return "", fmt.Errorf("invalid version blob: %s", checksum)
		}
		return checksum, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := s.root.Rename(temp, blob); err != nil {
		return "", err
	}
	return checksum, nil
}

// RestoreVersion verifies the archived bytes before replacing the live file.
func (s *Store) RestoreVersion(name, checksum string, size int64) error {
	if len(checksum) != 64 || size < 0 {
		return ErrInvalidPath
	}
	for _, char := range checksum {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ErrInvalidPath
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resolved, err := s.resolve(name, false)
	if err != nil {
		return err
	}
	info, err := s.root.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return ErrInvalidPath
	}
	archive, err := s.root.Open(path.Join(versionsDir, checksum))
	if err != nil {
		return err
	}
	defer archive.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temp := path.Join(path.Dir(resolved), ".homeos-restore-"+hex.EncodeToString(random[:]))
	output, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(temp)
	hash := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(output, hash), archive)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if count != size || hex.EncodeToString(hash.Sum(nil)) != checksum {
		return fmt.Errorf("file version checksum mismatch")
	}
	return s.root.Rename(temp, resolved)
}
