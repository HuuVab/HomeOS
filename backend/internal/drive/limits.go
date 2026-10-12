package drive

import (
	"errors"
	"os"
	"path"
	"strings"
)

var ErrQuotaExceeded = errors.New("storage quota exceeded")

type StorageUsage struct {
	UsedBytes     int64 `json:"usedBytes"`
	ReservedBytes int64 `json:"reservedBytes"`
	QuotaBytes    int64 `json:"quotaBytes"`
	MaxFileBytes  int64 `json:"maxFileBytes"`
}

func (s *Store) SetLimits(maxFileBytes, quotaBytes int64) error {
	if maxFileBytes < 1 || quotaBytes < maxFileBytes {
		return ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxFileBytes, s.quotaBytes = maxFileBytes, quotaBytes
	for _, child := range s.users {
		if err := child.SetLimits(maxFileBytes, quotaBytes); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Limits() (maxFileBytes, quotaBytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxFileBytes, s.quotaBytes
}

func (s *Store) Usage() (StorageUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usageLocked()
}

func (s *Store) usageLocked() (StorageUsage, error) {
	used, err := s.treeBytes(".", true)
	if err != nil {
		return StorageUsage{}, err
	}
	reserved := int64(0)
	for _, session := range s.pending {
		name := stagePath(session.ID)
		if !session.Staged {
			name = session.Path
		}
		info, err := s.root.Lstat(name)
		if err != nil {
			return StorageUsage{}, err
		}
		if remaining := session.Size - info.Size(); remaining > 0 {
			reserved += remaining
		}
	}
	return StorageUsage{UsedBytes: used, ReservedBytes: reserved, QuotaBytes: s.quotaBytes, MaxFileBytes: s.maxFileBytes}, nil
}

func (s *Store) checkAllocationLocked(add int64) error {
	if add < 0 {
		return ErrInvalidPath
	}
	usage, err := s.usageLocked()
	if err != nil {
		return err
	}
	if add > usage.QuotaBytes-usage.UsedBytes-usage.ReservedBytes {
		return ErrQuotaExceeded
	}
	return nil
}

func (s *Store) treeBytes(name string, skipUsers bool) (int64, error) {
	info, err := s.root.Lstat(name)
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, nil
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() {
			return info.Size(), nil
		}
		return 0, nil
	}
	dir, err := s.root.Open(name)
	if err != nil {
		return 0, err
	}
	items, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return 0, err
	}
	total := int64(0)
	for _, item := range items {
		if skipUsers && name == "." && item.Name() == ".homeos-users" {
			continue
		}
		if strings.HasPrefix(item.Name(), ".homeos-upload-") {
			continue
		}
		child := item.Name()
		if name != "." {
			child = path.Join(name, item.Name())
		}
		size, err := s.treeBytes(child, false)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}
