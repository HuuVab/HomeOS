package drive

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

func (s *Store) Copy(from, to string) error {
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
	copyBytes, err := s.treeBytes(source, false)
	if err != nil {
		return err
	}
	if err := s.checkAllocationLocked(copyBytes); err != nil {
		return err
	}
	if err := s.copyTree(source, target); err != nil {
		_ = s.removeTree(target)
		return err
	}
	return nil
}

func (s *Store) Search(query string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return nil, ErrInvalidPath
	}
	result := make([]Entry, 0)
	var visit func(string) error
	visit = func(folder string) error {
		dir, err := s.root.Open(folder)
		if err != nil {
			return err
		}
		items, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Type()&os.ModeSymlink != 0 || item.Name() == trashDir || item.Name() == ".homeos-users" || item.Name() == ".homeos-staging" || strings.HasPrefix(item.Name(), ".homeos-upload-") {
				continue
			}
			entryPath := item.Name()
			if folder != "." {
				entryPath = path.Join(folder, item.Name())
			}
			if s.isPending(entryPath) {
				continue
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			kind := "file"
			if info.IsDir() {
				kind = "folder"
			} else if !info.Mode().IsRegular() {
				continue
			}
			if strings.Contains(strings.ToLower(item.Name()), strings.ToLower(query)) {
				result = append(result, Entry{Name: item.Name(), Path: entryPath, Kind: kind, Size: info.Size(), ModifiedAt: info.ModTime().UTC()})
				if len(result) >= 500 {
					return nil
				}
			}
			if info.IsDir() {
				if err := visit(entryPath); err != nil {
					return err
				}
				if len(result) >= 500 {
					return nil
				}
			}
		}
		return nil
	}
	if err := visit("."); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) AllEntries() ([]Entry, error) {
	result := make([]Entry, 0)
	var visit func(string) error
	visit = func(folder string) error {
		entries, err := s.List(folder)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			result = append(result, entry)
			if entry.Kind == "folder" {
				if err := visit(entry.Path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(""); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) OpenPreview(name string) (*os.File, string, error) {
	extension := strings.ToLower(path.Ext(name))
	contentType := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".pdf": "application/pdf",
		".txt": "text/plain; charset=utf-8", ".md": "text/plain; charset=utf-8", ".csv": "text/plain; charset=utf-8", ".json": "text/plain; charset=utf-8", ".log": "text/plain; charset=utf-8",
		".go": "text/plain; charset=utf-8", ".js": "text/plain; charset=utf-8", ".ts": "text/plain; charset=utf-8", ".tsx": "text/plain; charset=utf-8", ".py": "text/plain; charset=utf-8", ".css": "text/plain; charset=utf-8",
	}[extension]
	if contentType == "" {
		return nil, "", ErrInvalidPath
	}
	file, err := s.Open(name)
	if err != nil {
		return nil, "", err
	}
	if strings.HasPrefix(contentType, "text/plain") {
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, "", err
		}
		if info.Size() > 1<<20 {
			file.Close()
			return nil, "", ErrTooLarge
		}
	}
	return file, contentType, nil
}

func (s *Store) copyTree(source, target string) error {
	info, err := s.root.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := s.root.Mkdir(target, 0700); err != nil {
			return err
		}
		dir, err := s.root.Open(source)
		if err != nil {
			return err
		}
		items, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Type()&os.ModeSymlink != 0 || strings.HasPrefix(item.Name(), ".homeos-upload-") {
				return ErrInvalidPath
			}
			if err := s.copyTree(path.Join(source, item.Name()), path.Join(target, item.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return ErrInvalidPath
	}
	in, err := s.root.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := s.root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func (s *Store) WriteZip(folder string, output io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.resolve(folder, false)
	if err != nil {
		return err
	}
	info, err := s.root.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrInvalidPath
	}
	archive := zip.NewWriter(output)
	if err := s.addZipDirectory(archive, target, path.Base(folder)); err != nil {
		return err
	}
	return archive.Close()
}

func (s *Store) ValidateFolder(folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.resolve(folder, false)
	if err != nil {
		return err
	}
	info, err := s.root.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrInvalidPath
	}
	return nil
}

func (s *Store) addZipDirectory(archive *zip.Writer, directory, archivePath string) error {
	if _, err := archive.Create(archivePath + "/"); err != nil {
		return err
	}
	dir, err := s.root.Open(directory)
	if err != nil {
		return err
	}
	items, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		name := path.Join(directory, item.Name())
		if item.Type()&os.ModeSymlink != 0 || strings.HasPrefix(item.Name(), ".homeos-upload-") || s.isPending(strings.ReplaceAll(name, "\\", "/")) {
			continue
		}
		zipName := path.Join(archivePath, item.Name())
		if item.IsDir() {
			if err := s.addZipDirectory(archive, name, zipName); err != nil {
				return err
			}
			continue
		}
		if !item.Type().IsRegular() {
			continue
		}
		input, err := s.root.Open(name)
		if err != nil {
			return err
		}
		writer, err := archive.Create(zipName)
		if err != nil {
			input.Close()
			return err
		}
		_, err = io.Copy(writer, input)
		input.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
