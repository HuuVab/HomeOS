package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

type cachedCover struct {
	Size       int64
	ModifiedAt time.Time
	Image      []byte
}

func serveMusicCover(w http.ResponseWriter, r *http.Request, track store.MusicTrack) {
	if !track.HasCover {
		serveFallbackMusicCover(w)
		return
	}
	dataDir := os.Getenv("HOMEOS_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	cacheDir := filepath.Join(dataDir, "music-covers")
	idKey := sha256.Sum256([]byte(track.ID))
	prefix := hex.EncodeToString(idKey[:])
	key := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", track.ID, track.Size, track.ModifiedAt)))
	cachePath := filepath.Join(cacheDir, prefix+"-"+hex.EncodeToString(key[:])+".jpg")
	if image, err := os.ReadFile(cachePath); err == nil {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(image)
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		serveFallbackMusicCover(w)
		return
	}
	image, err := readMusicCover(r.Context(), ffmpeg, track.Path, track.CoverStream, track.Size, time.Unix(0, track.ModifiedAt))
	if err != nil {
		serveFallbackMusicCover(w)
		return
	}
	if os.MkdirAll(cacheDir, 0700) == nil {
		tmp, err := os.CreateTemp(cacheDir, "cover-*.jpg")
		if err == nil {
			name := tmp.Name()
			_, writeErr := tmp.Write(image)
			closeErr := tmp.Close()
			if writeErr == nil && closeErr == nil {
				if os.Rename(name, cachePath) == nil {
					old, _ := filepath.Glob(filepath.Join(cacheDir, prefix+"-*.jpg"))
					for _, candidate := range old {
						if candidate != cachePath {
							_ = os.Remove(candidate)
						}
					}
				}
			} else {
				_ = os.Remove(name)
			}
		}
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(image)
}

func serveFallbackMusicCover(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><rect width="128" height="128" rx="10" fill="#173347"/><path fill="#67c7fa" d="M79 24v55a18 18 0 1 1-8-15V42l37-9v38a18 18 0 1 1-8-15V23z"/></svg>`))
}

var musicCoverCache = struct {
	sync.Mutex
	entries map[string]cachedCover
}{entries: make(map[string]cachedCover)}
var musicCoverSlots = make(chan struct{}, 2)

func readMusicCover(ctx context.Context, ffmpeg, path string, stream int, size int64, modifiedAt time.Time) ([]byte, error) {
	select {
	case musicCoverSlots <- struct{}{}:
		defer func() { <-musicCoverSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	musicCoverCache.Lock()
	if cached, ok := musicCoverCache.entries[path]; ok && cached.Size == size && cached.ModifiedAt.Equal(modifiedAt) {
		musicCoverCache.Unlock()
		return cached.Image, nil
	}
	musicCoverCache.Unlock()
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	image, err := exec.CommandContext(probeContext, ffmpeg,
		"-nostdin", "-v", "error", "-i", path, "-map", fmt.Sprintf("0:%d", stream),
		"-frames:v", "1", "-vf", "scale=128:128:force_original_aspect_ratio=decrease",
		"-q:v", "3", "-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1",
	).Output()
	if err != nil || len(image) == 0 || len(image) > 2<<20 {
		return nil, fmt.Errorf("cover unavailable")
	}
	musicCoverCache.Lock()
	if len(musicCoverCache.entries) > 300 {
		clear(musicCoverCache.entries)
	}
	musicCoverCache.entries[path] = cachedCover{Size: size, ModifiedAt: modifiedAt, Image: image}
	musicCoverCache.Unlock()
	return image, nil
}
