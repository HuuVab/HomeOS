package api

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type cachedCover struct {
	Size       int64
	ModifiedAt time.Time
	Image      []byte
}

var musicCoverCache = struct {
	sync.Mutex
	entries map[string]cachedCover
}{entries: make(map[string]cachedCover)}

func readMusicCover(ctx context.Context, ffmpeg, path string, stream int, size int64, modifiedAt time.Time) ([]byte, error) {
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
