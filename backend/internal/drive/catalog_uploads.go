package drive

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// Catalog upload sessions are owned by PostgreSQL. These files contain only
// disposable staged bytes; no JSON marker or filesystem metadata is trusted.
func (s *Store) CreateCatalogStage(id string) error {
	if !validID(id) {
		return ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureStage(); err != nil {
		return err
	}
	file, err := s.root.OpenFile(stagePath(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		s.root.Remove(stagePath(id))
		return err
	}
	return file.Close()
}

func (s *Store) AppendCatalogStage(id string, offset, expectedSize int64, reader io.Reader, checksum string) (int64, error) {
	if !validID(id) || offset < 0 || expectedSize < 0 || !validFingerprint(checksum) {
		return 0, ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.root.OpenFile(stagePath(id), os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < offset {
		return 0, ErrOffset
	}
	if info.Size() > offset {
		// Bytes written before a failed database-offset commit are discarded.
		if err := file.Truncate(offset); err != nil {
			return 0, err
		}
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	remaining := expectedSize - offset
	if remaining < 0 {
		return 0, ErrOffset
	}
	limit := MaxChunkSize
	if remaining < limit {
		limit = remaining
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, limit+1))
	if err != nil {
		_ = file.Truncate(offset)
		return 0, err
	}
	if written > limit {
		_ = file.Truncate(offset)
		return 0, ErrTooLarge
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(checksum)) != 1 {
		_ = file.Truncate(offset)
		return 0, ErrChecksum
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	return offset + written, nil
}

func (s *Store) FinishCatalogStage(id string, expectedSize int64, fingerprint string) (string, error) {
	if !validID(id) || expectedSize < 0 || !validFingerprint(fingerprint) {
		return "", ErrInvalidPath
	}
	s.mu.Lock()
	file, err := s.root.Open(stagePath(id))
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedSize {
		return "", ErrOffset
	}
	checksum, size, err := s.PutBlob(file, expectedSize)
	if err != nil {
		return "", err
	}
	if size != expectedSize {
		return "", ErrChecksum
	}
	if subtle.ConstantTimeCompare([]byte(checksum), []byte(fingerprint)) != 1 {
		return "", ErrChecksum
	}
	return checksum, nil
}

func (s *Store) RemoveCatalogStage(id string) error {
	if !validID(id) {
		return ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.root.Remove(stagePath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
