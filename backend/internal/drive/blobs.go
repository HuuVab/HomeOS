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

var ErrBlobUnavailable = errors.New("committed blob unavailable")

func validChecksum(checksum string) bool {
	if len(checksum) != 64 {
		return false
	}
	for _, char := range checksum {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// PutBlob makes bytes durable before returning their content address. A
// PostgreSQL catalog transaction may safely refer to the returned digest.
// Abandoned, unreferenced blobs can be collected after a failed transaction.
func (s *Store) PutBlob(reader io.Reader, limit int64) (string, int64, error) {
	if limit < 0 {
		return "", 0, ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.root.Mkdir(versionsDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", 0, err
	}
	info, err := s.root.Lstat(versionsDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", 0, ErrInvalidPath
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", 0, err
	}
	temporary := path.Join(versionsDir, ".blob-"+hex.EncodeToString(random[:]))
	file, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", 0, err
	}
	defer s.root.Remove(temporary)
	hash := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, limit+1))
	if copyErr == nil && count > limit {
		copyErr = ErrTooLarge
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	target := path.Join(versionsDir, checksum)
	if existing, err := s.root.Lstat(target); err == nil {
		if !existing.Mode().IsRegular() || existing.Size() != count {
			return "", 0, fmt.Errorf("invalid existing blob %s", checksum)
		}
		return checksum, count, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	}
	if err := s.root.Rename(temporary, target); err != nil {
		return "", 0, err
	}
	return checksum, count, nil
}

// OpenBlob reads only an immutable, checksum-named blob. Its caller must
// authorize the catalog reference before invoking it.
func (s *Store) OpenBlob(checksum string, size int64) (*os.File, error) {
	if !validChecksum(checksum) || size < 0 {
		return nil, ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.root.Open(path.Join(versionsDir, checksum))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBlobUnavailable, err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		file.Close()
		return nil, fmt.Errorf("%w: committed blob %s is missing or invalid", ErrBlobUnavailable, checksum)
	}
	return file, nil
}
