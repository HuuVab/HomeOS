package drive

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const MaxChunkSize int64 = 8 << 20

var ErrOffset = errors.New("upload offset mismatch")
var ErrTooLarge = errors.New("upload chunk too large")

type UploadSession struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Fingerprint string `json:"fingerprint"`
	Offset      int64  `json:"offset"`
	Staged      bool   `json:"staged"`
}

func marker(id string) string    { return ".homeos-upload-" + id + ".json" }
func stagePath(id string) string { return ".homeos-staging/" + id }

func (s *Store) ensureStage() error {
	if err := s.root.Mkdir(".homeos-staging", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := s.root.Lstat(".homeos-staging")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	return nil
}

func (s *Store) loadUploads() error {
	root, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer root.Close()
	items, err := root.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, item := range items {
		name := item.Name()
		if !strings.HasPrefix(name, ".homeos-upload-") {
			continue
		}
		if item.Type()&os.ModeSymlink != 0 || item.IsDir() {
			return ErrInvalidPath
		}
		file, err := s.root.Open(name)
		if err != nil {
			return err
		}
		var session UploadSession
		decodeErr := json.NewDecoder(io.LimitReader(file, 4096)).Decode(&session)
		file.Close()
		if decodeErr != nil || name != marker(session.ID) || !validID(session.ID) || session.Size < 0 || !validFingerprint(session.Fingerprint) {
			return fmt.Errorf("invalid upload marker %q", name)
		}
		if _, err := s.resolve(session.Path, false); err != nil {
			return err
		}
		if session.Staged {
			if _, err := s.root.Lstat(stagePath(session.ID)); errors.Is(err, os.ErrNotExist) {
				info, targetErr := s.root.Lstat(session.Path)
				if targetErr == nil && info.Mode().IsRegular() && info.Size() == session.Size {
					if err := s.root.Remove(name); err != nil {
						return err
					}
					continue
				}
				if errors.Is(targetErr, os.ErrNotExist) {
					if err := s.root.Remove(name); err != nil {
						return err
					}
					continue
				}
				return err
			} else if err != nil {
				return err
			}
		}
		s.pending[session.ID] = session
	}
	return nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *Store) isPending(path string) bool {
	for _, session := range s.pending {
		if strings.EqualFold(session.Path, path) {
			return true
		}
	}
	return false
}

func (s *Store) PendingUploads() []UploadSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := make([]UploadSession, 0, len(s.pending))
	for _, session := range s.pending {
		sessions = append(sessions, session)
	}
	return sessions
}

func (s *Store) StartUpload(path string, size int64, fingerprint string) (UploadSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.resolve(path, false)
	if err != nil {
		return UploadSession{}, err
	}
	if size < 0 || !validFingerprint(fingerprint) {
		return UploadSession{}, ErrInvalidPath
	}
	if size > s.maxFileBytes {
		return UploadSession{}, ErrTooLarge
	}
	for _, pending := range s.pending {
		if !strings.EqualFold(pending.Path, path) {
			continue
		}
		if pending.Size != size || pending.Fingerprint != fingerprint {
			return UploadSession{}, os.ErrExist
		}
		name := target
		if pending.Staged {
			name = stagePath(pending.ID)
		}
		file, err := s.root.OpenFile(name, os.O_RDWR, 0600)
		if err != nil {
			return UploadSession{}, err
		}
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return UploadSession{}, err
		}
		if !info.Mode().IsRegular() || info.Size() > size {
			return UploadSession{}, ErrInvalidPath
		}
		pending.Offset = info.Size()
		s.pending[pending.ID] = pending
		return pending, nil
	}
	if _, err := s.root.Lstat(target); err == nil {
		return UploadSession{}, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return UploadSession{}, err
	}
	if err := s.checkAllocationLocked(size); err != nil {
		return UploadSession{}, err
	}
	if err := s.ensureStage(); err != nil {
		return UploadSession{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return UploadSession{}, err
	}
	session := UploadSession{ID: hex.EncodeToString(random[:]), Path: path, Size: size, Fingerprint: fingerprint, Staged: true}
	meta, err := s.root.OpenFile(marker(session.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return UploadSession{}, err
	}
	encoded, err := json.Marshal(session)
	if err == nil {
		_, err = meta.Write(encoded)
	}
	if err == nil {
		err = meta.Sync()
	}
	closeErr := meta.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		s.root.Remove(marker(session.ID))
		return UploadSession{}, err
	}
	file, err := s.root.OpenFile(stagePath(session.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		s.root.Remove(marker(session.ID))
		return UploadSession{}, err
	}
	file.Close()
	s.pending[session.ID] = session
	return session, nil
}

func (s *Store) AppendChunk(id string, offset int64, reader io.Reader) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.pending[id]
	if !ok {
		return 0, os.ErrNotExist
	}
	if _, err := s.resolve(session.Path, false); err != nil {
		return 0, err
	}
	name := session.Path
	if session.Staged {
		name = stagePath(id)
	}
	file, err := s.root.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, ErrInvalidPath
	}
	if offset != info.Size() {
		return info.Size(), ErrOffset
	}
	if offset >= session.Size {
		return offset, ErrTooLarge
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	limit := min(MaxChunkSize, session.Size-offset)
	written, err := io.Copy(file, io.LimitReader(reader, limit+1))
	if err == nil && written > limit {
		err = ErrTooLarge
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		if truncateErr := file.Truncate(offset); truncateErr != nil {
			return offset, truncateErr
		}
		file.Sync()
		return offset, err
	}
	session.Offset = offset + written
	s.pending[id] = session
	return session.Offset, nil
}

func (s *Store) FinishUpload(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.pending[id]
	if !ok {
		return os.ErrNotExist
	}
	if _, err := s.resolve(session.Path, false); err != nil {
		return err
	}
	name := session.Path
	if session.Staged {
		name = stagePath(id)
	}
	file, err := s.root.Open(name)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	file.Close()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != session.Size {
		return ErrOffset
	}
	if session.Staged {
		if _, err := s.root.Lstat(session.Path); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := s.root.Rename(name, session.Path); err != nil {
			return err
		}
	}
	if err := s.root.Remove(marker(id)); err != nil {
		return err
	}
	delete(s.pending, id)
	return nil
}
