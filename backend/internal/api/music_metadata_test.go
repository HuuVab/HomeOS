package api

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReadMusicMetadata(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "track.m4a")
	output, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "anullsrc", "-t", "1", "-metadata", "title=Song title", "-metadata", "artist=Test artist", "-metadata", "album=Test album", "-c:a", "aac", "-y", path).CombinedOutput()
	if err != nil {
		t.Fatalf("create test audio: %v: %s", err, output)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	track := readMusicMetadata(ctx, probe, path, info.Name(), info.Size(), info.ModTime())
	if track.Title != "Song title" || track.Artist != "Test artist" || track.Album != "Test album" || track.Duration < 0.9 {
		t.Fatalf("metadata = %+v", track)
	}
	cover := filepath.Join(t.TempDir(), "cover.jpg")
	output, err = exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=32x32", "-frames:v", "1", "-y", cover).CombinedOutput()
	if err != nil {
		t.Fatalf("create cover: %v: %s", err, output)
	}
	withCover := filepath.Join(t.TempDir(), "with-cover.m4a")
	output, err = exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", path, "-i", cover, "-map", "0:a", "-map", "1:v", "-c:a", "copy", "-c:v", "mjpeg", "-disposition:v", "attached_pic", "-y", withCover).CombinedOutput()
	if err != nil {
		t.Fatalf("attach cover: %v: %s", err, output)
	}
	info, err = os.Stat(withCover)
	if err != nil {
		t.Fatal(err)
	}
	covered := readMusicMetadata(ctx, probe, withCover, info.Name(), info.Size(), info.ModTime())
	if !covered.HasCover {
		t.Fatalf("cover not detected: %+v", covered)
	}
	image, err := readMusicCover(ctx, ffmpeg, withCover, covered.CoverStream, info.Size(), info.ModTime())
	if err != nil || len(image) < 4 || image[0] != 0xff || image[1] != 0xd8 {
		t.Fatalf("cover extraction: %v, %d bytes", err, len(image))
	}
}
