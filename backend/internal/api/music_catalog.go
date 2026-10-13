package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func approvedMusicTrack(r *http.Request, db *store.Store, id string) (store.MusicTrack, error) {
	t, err := db.MusicTrackByID(r.Context(), id)
	if err != nil || !t.Available {
		return t, errors.New("unavailable")
	}
	lib, err := db.MusicLibrary(r.Context())
	if err != nil || lib.Root == "" {
		return t, errors.New("library unavailable")
	}
	if !musicRootAllowed(lib.Root) {
		return t, errors.New("library root is not approved")
	}
	rel, err := filepath.Rel(lib.Root, t.Path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return t, errors.New("outside library")
	}
	resolvedRoot, err := filepath.EvalSymlinks(lib.Root)
	if err != nil {
		return t, err
	}
	resolvedPath, err := filepath.EvalSymlinks(t.Path)
	if err != nil {
		return t, err
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return t, errors.New("file escapes library")
	}
	info, err := os.Lstat(resolvedPath)
	if err != nil || !info.Mode().IsRegular() || musicTypes[strings.ToLower(filepath.Ext(t.Path))] == "" {
		return t, errors.New("file unavailable")
	}
	t.Path = resolvedPath
	return t, nil
}

func musicRootAllowed(path string) bool {
	configured := os.Getenv("HOMEOS_MUSIC_ALLOWED_ROOTS")
	if configured == "" {
		return true
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	for _, root := range filepath.SplitList(configured) {
		if root == "" {
			continue
		}
		root, err = filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, resolvedPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
