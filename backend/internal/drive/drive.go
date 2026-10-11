package drive

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const MaxUploadBytes = 100 << 20

var ErrInvalidPath = errors.New("invalid drive path")
var ErrTooLarge = errors.New("file exceeds 100 MiB limit")

type Store struct{ root *os.Root }

type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
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
	return &Store{root: opened}, nil
}

func (s *Store) Close() error { return s.root.Close() }

func validPart(part string) bool {
	if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".homeos-upload-") ||
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
		if strings.HasPrefix(item.Name(), ".homeos-upload-") || item.Type()&os.ModeSymlink != 0 {
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

func (s *Store) CreateFolder(path string) error {
	target, err := s.resolve(path, false)
	if err != nil {
		return err
	}
	return s.root.Mkdir(target, 0700)
}

func (s *Store) Upload(path string, reader io.Reader) error {
	target, err := s.resolve(path, false)
	if err != nil {
		return err
	}
	if _, err := s.root.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := s.root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			s.root.Remove(target)
		}
	}()
	defer file.Close()
	n, err := io.Copy(file, io.LimitReader(reader, MaxUploadBytes+1))
	if err != nil {
		return err
	}
	if n > MaxUploadBytes {
		return ErrTooLarge
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func (s *Store) Open(path string) (*os.File, error) {
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
