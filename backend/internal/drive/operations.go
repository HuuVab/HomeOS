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
	if err := s.copyTree(source, target); err != nil {
		_ = s.removeTree(target)
		return err
	}
	return nil
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
