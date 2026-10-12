package drive

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrInvalidPath = errors.New("invalid drive path")

const trashDir = ".homeos-trash"

type TrashEntry struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	DeletedAt time.Time `json:"deletedAt"`
}

type Store struct {
	root         *os.Root
	rootPath     string
	mu           sync.Mutex
	pending      map[string]UploadSession
	users        map[int64]*Store
	maxFileBytes int64
	quotaBytes   int64
}

type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Version    int64     `json:"version,omitempty"`
	Checksum   string    `json:"checksum,omitempty"`
}

func New(root string) (*Store, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidPath
	}
	opened, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	store := &Store{root: opened, rootPath: absolute, pending: make(map[string]UploadSession), users: make(map[int64]*Store), maxFileBytes: 2 << 30, quotaBytes: 20 << 30}
	if err := store.loadUploads(); err != nil {
		opened.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, child := range s.users {
		_ = child.Close()
	}
	return s.root.Close()
}

func (s *Store) ForUser(id int64) (*Store, error) {
	if id == 1 {
		return s, nil
	}
	if id < 1 {
		return nil, ErrInvalidPath
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if child := s.users[id]; child != nil {
		return child, nil
	}
	if err := s.root.Mkdir(".homeos-users", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := s.root.Lstat(".homeos-users")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidPath
	}
	child, err := New(filepath.Join(s.rootPath, ".homeos-users", strconv.FormatInt(id, 10)))
	if err != nil {
		return nil, err
	}
	s.users[id] = child
	child.maxFileBytes = s.maxFileBytes
	child.quotaBytes = s.quotaBytes
	return child, nil
}

func validPart(part string) bool {
	if part == "" || part == "." || part == ".." || part == trashDir || part == ".homeos-users" || part == ".homeos-staging" || part == ".homeos-versions" || strings.HasPrefix(part, ".homeos-upload-") || strings.HasPrefix(part, ".homeos-restore-") ||
		strings.ContainsAny(part, `\/:*?"<>|`) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
		return false
	}
	for _, r := range part {
		if r < 32 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
		(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		return false
	}
	return true
}

func (s *Store) resolve(path string, allowRoot bool) (string, error) {
	if path == "" && allowRoot {
		return ".", nil
	}
	if path == "" {
		return "", ErrInvalidPath
	}
	current := ""
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if !validPart(part) {
			return "", ErrInvalidPath
		}
		current = filepath.Join(current, part)
		info, err := s.root.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", ErrInvalidPath
			}
			if i < len(parts)-1 && !info.IsDir() {
				return "", ErrInvalidPath
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return current, nil
}

func (s *Store) List(path string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	directory, err := s.resolve(path, true)
	if err != nil {
		return nil, err
	}
	file, err := s.root.Open(directory)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrInvalidPath
	}
	items, err := file.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		if item.Name() == trashDir || item.Name() == ".homeos-users" || item.Name() == ".homeos-staging" || item.Name() == ".homeos-versions" || strings.HasPrefix(item.Name(), ".homeos-upload-") || strings.HasPrefix(item.Name(), ".homeos-restore-") || item.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := item.Info()
		if err != nil {
			return nil, err
		}
		kind := "file"
		if info.IsDir() {
			kind = "folder"
		} else if !info.Mode().IsRegular() {
			continue
		}
		entryPath := item.Name()
		if path != "" {
			entryPath = path + "/" + item.Name()
		}
		if s.isPending(entryPath) {
			continue
		}
		entries = append(entries, Entry{Name: item.Name(), Path: entryPath, Kind: kind, Size: info.Size(), ModifiedAt: info.ModTime().UTC()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "folder"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (s *Store) Move(from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if from == to || s.hasPendingBelow(from) || s.isPending(to) {
		return ErrInvalidPath
	}
	source, err := s.resolve(from, false)
	if err != nil {
		return err
	}
	target, err := s.resolve(to, false)
	if err != nil {
		return err
	}
	info, err := s.root.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() && strings.HasPrefix(to, from+"/") {
		return ErrInvalidPath
	}
	if _, err := s.root.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.root.Rename(source, target)
}

func (s *Store) Trash(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasPendingBelow(path) {
		return ErrInvalidPath
	}
	source, err := s.resolve(path, false)
	if err != nil {
		return err
	}
	if _, err := s.root.Lstat(source); err != nil {
		return err
	}
	if err := s.root.Mkdir(trashDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	key := hex.EncodeToString(id[:])
	meta := TrashEntry{ID: key, Path: path, DeletedAt: time.Now().UTC()}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	metadata := trashDir + "/" + key + ".json"
	f, err := s.root.OpenFile(metadata, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		s.root.Remove(metadata)
		return err
	}
	if err := s.root.Rename(source, trashDir+"/"+key); err != nil {
		s.root.Remove(metadata)
		return err
	}
	return nil
}

func (s *Store) removeTree(path string) error {
	info, err := s.root.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		dir, err := s.root.Open(path)
		if err != nil {
			return err
		}
		items, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := s.removeTree(path + "/" + item.Name()); err != nil {
				return err
			}
		}
	}
	return s.root.Remove(path)
}

func (s *Store) DeleteForever(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(id) != 32 {
		return ErrInvalidPath
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ErrInvalidPath
	}
	metadata := trashDir + "/" + id + ".json"
	if _, err := s.root.Lstat(metadata); err != nil {
		return err
	}
	if err := s.removeTree(trashDir + "/" + id); err != nil {
		return err
	}
	return s.root.Remove(metadata)
}

func (s *Store) PurgeExpired(retention time.Duration, now time.Time) (int, error) {
	if retention <= 0 {
		return 0, ErrInvalidPath
	}
	entries, err := s.ListTrash()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		deletedAt := entry.DeletedAt
		if deletedAt.IsZero() {
			s.mu.Lock()
			info, statErr := s.root.Stat(trashDir + "/" + entry.ID + ".json")
			s.mu.Unlock()
			if statErr != nil {
				return removed, statErr
			}
			deletedAt = info.ModTime()
		}
		if !deletedAt.Add(retention).After(now) {
			if err := s.DeleteForever(entry.ID); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

func (s *Store) hasPendingBelow(path string) bool {
	for _, upload := range s.pending {
		if strings.EqualFold(upload.Path, path) || strings.HasPrefix(strings.ToLower(upload.Path), strings.ToLower(path)+"/") {
			return true
		}
	}
	return false
}

func (s *Store) ListTrash() ([]TrashEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.root.Open(trashDir)
	if errors.Is(err, os.ErrNotExist) {
		return []TrashEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	items, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := make([]TrashEntry, 0)
	for _, item := range items {
		if !strings.HasSuffix(item.Name(), ".json") || item.Type()&os.ModeSymlink != 0 {
			continue
		}
		f, err := s.root.Open(trashDir + "/" + item.Name())
		if err != nil {
			return nil, err
		}
		var entry TrashEntry
		err = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&entry)
		f.Close()
		if err != nil || entry.ID+".json" != item.Name() {
			return nil, ErrInvalidPath
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s *Store) Restore(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(id) != 32 {
		return ErrInvalidPath
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ErrInvalidPath
	}
	f, err := s.root.Open(trashDir + "/" + id + ".json")
	if err != nil {
		return err
	}
	var entry TrashEntry
	err = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&entry)
	f.Close()
	if err != nil || entry.ID != id {
		return ErrInvalidPath
	}
	target, err := s.resolve(entry.Path, false)
	if err != nil {
		return err
	}
	if s.isPending(entry.Path) {
		return os.ErrExist
	}
	if _, err := s.root.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.root.Rename(trashDir+"/"+id, target); err != nil {
		return err
	}
	return s.root.Remove(trashDir + "/" + id + ".json")
}

func (s *Store) CreateFolder(path string) error {
	target, err := s.resolve(path, false)
	if err != nil {
		return err
	}
	return s.root.Mkdir(target, 0700)
}

func (s *Store) Upload(path string, reader io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.resolve(path, false)
	if err != nil {
		return err
	}
	if _, err := s.root.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	usage, err := s.usageLocked()
	if err != nil {
		return err
	}
	available := usage.QuotaBytes - usage.UsedBytes - usage.ReservedBytes
	if available < 0 {
		return ErrQuotaExceeded
	}
	limit := s.maxFileBytes
	if available < limit {
		limit = available
	}
	if err := s.ensureStage(); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	staging := stagePath(hex.EncodeToString(random[:]))
	file, err := s.root.OpenFile(staging, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			s.root.Remove(staging)
		}
	}()
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if written > limit {
		if available < s.maxFileBytes {
			return ErrQuotaExceeded
		}
		return ErrTooLarge
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := s.root.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.root.Rename(staging, target); err != nil {
		return err
	}
	complete = true
	return nil
}

// Replace atomically stages new bytes for an existing regular file. The
// caller must enforce the catalog revision precondition before invoking it.
func (s *Store) Replace(path string, reader io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isPending(path) {
		return ErrInvalidPath
	}
	target, err := s.resolve(path, false)
	if err != nil {
		return err
	}
	before, err := s.root.Lstat(target)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return ErrInvalidPath
	}
	usage, err := s.usageLocked()
	if err != nil {
		return err
	}
	available := usage.QuotaBytes - usage.UsedBytes - usage.ReservedBytes + before.Size()
	if available < 0 {
		return ErrQuotaExceeded
	}
	limit := s.maxFileBytes
	if available < limit {
		limit = available
	}
	if err := s.ensureStage(); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	staging := stagePath(hex.EncodeToString(random[:]))
	file, err := s.root.OpenFile(staging, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(staging)
	written, copyErr := io.Copy(file, io.LimitReader(reader, limit+1))
	if copyErr != nil {
		file.Close()
		return copyErr
	}
	if written > limit {
		file.Close()
		if available < s.maxFileBytes {
			return ErrQuotaExceeded
		}
		return ErrTooLarge
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	after, err := s.root.Lstat(target)
	if err != nil || !after.Mode().IsRegular() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return ErrInvalidPath
	}
	return s.root.Rename(staging, target)
}

func (s *Store) Open(path string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isPending(path) {
		return nil, os.ErrNotExist
	}
	target, err := s.resolve(path, false)
	if err != nil {
		return nil, err
	}
	file, err := s.root.Open(target)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, ErrInvalidPath
	}
	return file, nil
}
